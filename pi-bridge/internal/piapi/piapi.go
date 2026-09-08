// Package piapi hosts pi-bridge's always-on engine and the HTTP endpoints the
// upstream UI needs but pc-bridge never implemented (the "always-on" half of
// the capability ledger). It is mounted as a middleware in front of the
// httpapi handler, claiming its own routes and passing everything else through.
//
// pi-v0.5.0 scope: the engine tick loop driving the lamp from httpapi's stored
// schedule, plus /api/time, /api/output/status, /api/wifi/signal, /api/logs.
// Later tags add ramp / feed / maintenance / acclimation / seasonal.
package piapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/engine"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/httpapi"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/lamp"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/ringlog"
)

type Deps struct {
	Next    http.Handler
	API     *httpapi.Server
	Engine  *engine.Engine
	Lamp    *lamp.Lamp
	Log     *ringlog.Ring
	TZ      *time.Location
	WlanIf  string        // e.g. "wlan0"
	DataDir string        // fallback if FX is nil
	FX      *EffectsStore // share the same store the Provider uses
}

type handler struct {
	Deps
	fx *EffectsStore
}

func Wrap(d Deps) http.Handler {
	if d.WlanIf == "" {
		d.WlanIf = "wlan0"
	}
	fx := d.FX
	if fx == nil {
		fx = NewEffectsStore(d.DataDir)
	}
	h := &handler{Deps: d, fx: fx}
	h.applyRampCadence() // restore the tick cadence for a persisted ramp state
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/time":
		h.time(w, r)
	case "/api/output/status":
		h.outputStatus(w, r)
	case "/api/wifi/signal":
		h.wifiSignal(w, r)
	case "/api/logs":
		h.logs(w, r)

	case "/api/ramp/status":
		h.rampStatus(w, r)
	case "/api/ramp/start":
		h.rampStart(w, r)
	case "/api/ramp/stop":
		h.rampStop(w, r)
	case "/api/ramp/tick":
		h.rampTick(w, r)

	case "/api/feed/status":
		h.feedStatus(w, r)
	case "/api/feed/start":
		h.feedStart(w, r)
	case "/api/feed/stop":
		h.feedStop(w, r)

	case "/api/maintenance/status":
		h.maintStatus(w, r)
	case "/api/maintenance/start":
		h.maintStart(w, r)
	case "/api/maintenance/stop":
		h.maintStop(w, r)

	case "/api/acclimation/config":
		h.acclimationConfig(w, r)
	case "/api/acclimation/status":
		h.acclimationStatus(w, r)
	case "/api/seasonal/config":
		h.seasonalConfig(w, r)
	case "/api/seasonal/status":
		h.seasonalStatus(w, r)

	case "/api/push", "/api/master":
		// let the vendored handler persist it, then recompute immediately
		h.Next.ServeHTTP(w, r)
		if r.Method == http.MethodPost && h.Engine != nil {
			h.Engine.Kick()
		}
	default:
		h.Next.ServeHTTP(w, r)
	}
}

// FX exposes the effects store so the Provider can read acclimation/seasonal.
func (h *handler) FX() *EffectsStore { return h.fx }

// Provider implements engine.Provider by combining httpapi's stored working
// state with pi-bridge's own effect configs (acclimation, seasonal). Standalone
// so the engine can be constructed before the HTTP middleware.
type Provider struct {
	API *httpapi.Server
	FX  *EffectsStore
	TZ  *time.Location
}

func NewProvider(api *httpapi.Server, fx *EffectsStore, tz *time.Location) *Provider {
	if tz == nil {
		tz = time.Local
	}
	return &Provider{API: api, FX: fx, TZ: tz}
}

func (p *Provider) EngineSnapshot() engine.Snapshot {
	st := p.API.StateSnapshot()

	var base engine.Schedule
	for i := 0; i < engine.Slots && i < len(st.Schedule); i++ {
		for j := 0; j < 8 && j < len(st.Schedule[i]); j++ {
			base[i][j] = st.Schedule[i][j]
		}
	}
	var manual [engine.Channels]int
	for i := 0; i < engine.Channels && i < len(st.Manual); i++ {
		manual[i] = st.Manual[i]
	}

	cfg := engine.Config{
		Device:               p.API.Device(),
		MasterBrightness:     nz(st.MasterBrightness, 100),
		ScheduleShiftMinutes: st.ScheduleShiftMinutes,
	}
	cfg.Siesta.Enabled = st.Siesta.Enabled
	cfg.Siesta.Start = orDef(st.Siesta.Start, "13:00")
	cfg.Siesta.DurationMins = nz(st.Siesta.DurationMins, st.Siesta.Duration)
	cfg.Siesta.Intensity = st.Siesta.Intensity
	cfg.Lunar.Enabled = st.Lunar.Enabled
	cfg.Lunar.Start = orDef(st.Lunar.Start, "18:30")
	cfg.Lunar.End = orDef(st.Lunar.End, "06:30")
	cfg.Lunar.ClampStart = orDef(st.Lunar.ClampStart, "18:00")
	cfg.Lunar.ClampEnd = orDef(st.Lunar.ClampEnd, "08:00")
	cfg.Lunar.MaxIntensity = nz(st.Lunar.MaxIntensity, 15)
	cfg.Lunar.DayThreshold = st.Lunar.DayThreshold
	cfg.Lunar.TrackMoonrise = st.Lunar.TrackMoonrise

	if p.FX != nil {
		p.FX.mu.Lock()
		a, se := p.FX.Acclimation, p.FX.Seasonal
		p.FX.mu.Unlock()
		cfg.Acclimation.Enabled = a.Enabled
		cfg.Acclimation.StartPercent = a.StartPercent
		cfg.Acclimation.DurationDays = a.DurationDays
		if t, err := time.Parse(time.RFC3339, a.StartISO); err == nil {
			cfg.Acclimation.StartEpoch = t.Unix()
		}
		cfg.Seasonal.Enabled = se.Enabled
		cfg.Seasonal.MaxShiftMinutes = se.MaxShiftMinutes
	}

	return engine.Snapshot{
		Base:     base,
		Manual:   manual,
		AutoMode: st.Mode != "manual",
		Config:   cfg,
	}
}

// ---- endpoints ----------------------------------------------------------

func (h *handler) time(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// The Pi's clock is owned by NTP; accept and ignore the UI's set.
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "clock_set": engine.ClockSane()})
		return
	}
	now := time.Now().In(h.TZ)
	writeJSON(w, http.StatusOK, map[string]any{
		"clock_set": engine.ClockSane(),
		"now":       now.Format(time.RFC3339),
		"epoch":     now.Unix(),
		"tz":        h.TZ.String(),
	})
}

func (h *handler) outputStatus(w http.ResponseWriter, r *http.Request) {
	st := h.Engine.Status()
	writeJSON(w, http.StatusOK, st)
}

func (h *handler) logs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": h.Log.Entries(limit)})
}

var reSignal = regexp.MustCompile(`signal:\s*(-?\d+)\s*dBm`)
var reBitrate = regexp.MustCompile(`tx bitrate:\s*([\d.]+)`)

func (h *handler) wifiSignal(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"interface": h.WlanIf, "lamp": h.Lamp.Health()}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if b, err := exec.CommandContext(ctx, "iw", "dev", h.WlanIf, "link").CombinedOutput(); err == nil {
		s := string(b)
		out["associated"] = !strings.Contains(s, "Not connected")
		if m := reSignal.FindStringSubmatch(s); m != nil {
			if n, e := strconv.Atoi(m[1]); e == nil {
				out["rssi_dbm"] = n
				out["quality"] = rssiToQuality(n)
			}
		}
		if m := reBitrate.FindStringSubmatch(s); m != nil {
			out["tx_bitrate_mbps"], _ = strconv.ParseFloat(m[1], 64)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func rssiToQuality(dbm int) int {
	// -50 or better = 100%, -100 = 0%
	q := 2 * (dbm + 100)
	if q < 0 {
		q = 0
	}
	if q > 100 {
		q = 100
	}
	return q
}

// ---- small helpers ----------------------------------------------------------

func nz(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
func orDef(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errObj(msg string) map[string]any { return map[string]any{"ok": false, "error": msg} }
