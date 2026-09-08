// Command k7-pi-bridge is the 24/7 K7 Pro controller daemon for a Raspberry Pi.
//
// It bridges the lamp's Wi-Fi AP (reached on wlan0) to the home LAN (eth0):
// serves the shared web UI + REST API, proxies the raw 8266 protocol, runs the
// always-on lighting engine, and updates itself over the air from GitHub
// releases.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/config"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/engine"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/httpapi"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/lamp"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/piapi"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/piweb"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/profiles"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/proxy"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/ringlog"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/updater"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "k7-pi-bridge:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(args)
	if errors.Is(err, config.ErrVersionRequested) {
		fmt.Println(version.String())
		return nil
	}
	if err != nil {
		return err
	}

	rlog := ringlog.New(500)
	logger := slog.New(ringlog.NewHandler(rlog, newLogger(cfg.LogLevel).Handler()))
	slog.SetDefault(logger)
	logger.Info("starting", "version", version.String(), "listen", cfg.Listen,
		"lamp", fmt.Sprintf("%s:%d", cfg.LampHost, cfg.LampPort),
		"install_root", cfg.InstallRoot, "data_dir", cfg.DataDir)

	tz := time.Local
	if cfg.Timezone != "" {
		if loc, e := time.LoadLocation(cfg.Timezone); e == nil {
			tz = loc
		} else {
			logger.Warn("bad timezone, using system", "tz", cfg.Timezone, "err", e)
		}
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var healthy atomic.Bool
	healthy.Store(true)

	up := updater.New(updater.Options{
		Repo:        cfg.UpdateRepo,
		Channel:     cfg.UpdateChannel,
		InstallRoot: cfg.InstallRoot,
		CurrentTag:  version.Version,
	})
	up.ConfirmAfterStart(ctx, version.Version, 45*time.Second, func() bool { return healthy.Load() })
	cfgPath := os.Getenv("K7_CONFIG")
	if cfgPath == "" {
		cfgPath = filepath.Join(cfg.DataDir, "config.json")
	}
	var autoUpdate atomic.Bool
	autoUpdate.Store(cfg.AutoUpdate)
	if d := parseInterval(cfg.UpdateInterval); d > 0 {
		go updateLoop(ctx, up, d, &autoUpdate)
	}

	// Phase 3: the always-on engine drives the lamp; every feature is live.
	caps := httpapi.DefaultCapabilities()
	for k := range caps {
		caps[k] = true
	}
	caps["setup_portal"] = false // Phase 4

	lampConn := lamp.New(cfg.LampHost, cfg.LampPort)
	fx := piapi.NewEffectsStore(cfg.DataDir)

	api, err := httpapi.New(httpapi.Options{
		ConfigPath:   filepath.Join(cfg.DataDir, "store.json"),
		Version:      version.Version,
		Timeout:      5 * time.Second,
		Device:       "k7pro",
		LampHost:     cfg.LampHost,
		LampPort:     cfg.LampPort,
		LampGate:     lampConn.Gate(),
		Capabilities: caps,
	})
	if err != nil {
		return fmt.Errorf("http api: %w", err)
	}

	eng := engine.New(piapi.NewProvider(api, fx, tz), lampConn, tz, 5*time.Minute)
	go eng.Run(ctx)

	// Per-lamp profile store (isolated from OTA; keyed by lamp MAC/name).
	profStore := profiles.New(cfg.DataDir, cfg.LampHost)
	profStore.NameHint = api.LampName
	profStore.Migrate(profStore.LampID(ctx), api.LegacyProfiles())

	// pi-bridge UX layer over the unmodified upstream UI.
	uiHandler := piweb.Wrap(piweb.Deps{
		Next: piapi.Wrap(piapi.Deps{
			Next:    api.Routes(),
			API:     api,
			Engine:  eng,
			Lamp:    lampConn,
			Log:     rlog,
			TZ:      tz,
			DataDir: cfg.DataDir,
			FX:      fx,
		}),
		Profiles:   profStore,
		Version:    version.Version,
		UpdateRepo: cfg.UpdateRepo,
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           routes(cfg, cfgPath, up, &autoUpdate, uiHandler),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if cfg.Proxy != "" {
		px := &proxy.Proxy{
			Listen:   cfg.Proxy,
			LampAddr: fmt.Sprintf("%s:%d", cfg.LampHost, cfg.LampPort),
			Gate:     lampConn.Gate(),
		}
		go func() {
			if err := px.Run(ctx); err != nil {
				slog.Error("proxy stopped", "err", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

func routes(cfg config.Config, cfgPath string, up *updater.Updater, autoUpdate *atomic.Bool, ui http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("GET /api/update/status", func(w http.ResponseWriter, r *http.Request) {
		rel, err := up.Check(r.Context())
		resp := map[string]any{
			"current": version.Version, "channel": cfg.UpdateChannel, "repo": cfg.UpdateRepo,
			"auto_update": autoUpdate.Load(),
		}
		if err != nil {
			resp["error"] = err.Error()
			writeJSON(w, http.StatusBadGateway, resp)
			return
		}
		if rel == nil {
			resp["up_to_date"] = true
		} else {
			resp["up_to_date"] = false
			resp["available"] = rel.Tag
			resp["notes"] = rel.Notes
		}
		writeJSON(w, http.StatusOK, resp)
	})

	// Toggle automatic OTA. Persists to config.json so it survives restarts.
	mux.HandleFunc("POST /api/update/config", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AutoUpdate *bool `json:"auto_update"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AutoUpdate == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "auto_update (bool) required"})
			return
		}
		autoUpdate.Store(*in.AutoUpdate)
		c := cfg
		c.AutoUpdate = *in.AutoUpdate
		if err := c.Save(cfgPath); err != nil {
			slog.Warn("save config", "err", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"auto_update": *in.AutoUpdate})
	})

	// Release history for the version-chip changelog popup. Fetched server-side
	// (no browser CORS / rate-limit worries), cached ~10 min.
	var histMu sync.Mutex
	var histAt time.Time
	var histCache []byte
	mux.HandleFunc("GET /api/update/history", func(w http.ResponseWriter, r *http.Request) {
		histMu.Lock()
		fresh := time.Since(histAt) < 10*time.Minute && histCache != nil
		body := histCache
		histMu.Unlock()
		if !fresh {
			req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet,
				"https://api.github.com/repos/"+cfg.UpdateRepo+"/releases?per_page=40", nil)
			req.Header.Set("Accept", "application/vnd.github+json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				var raw []struct {
					TagName     string `json:"tag_name"`
					Name        string `json:"name"`
					Body        string `json:"body"`
					PublishedAt string `json:"published_at"`
					Prerelease  bool   `json:"prerelease"`
					HTMLURL     string `json:"html_url"`
				}
				if json.NewDecoder(resp.Body).Decode(&raw) == nil {
					out := make([]map[string]any, 0, len(raw))
					for _, x := range raw {
						if !strings.HasPrefix(x.TagName, "pi-v") {
							continue
						}
						out = append(out, map[string]any{
							"tag": x.TagName, "name": x.Name, "notes": x.Body,
							"published_at": x.PublishedAt, "prerelease": x.Prerelease, "url": x.HTMLURL,
						})
					}
					body, _ = json.Marshal(map[string]any{"current": version.Version, "releases": out})
					histMu.Lock()
					histCache, histAt = body, time.Now()
					histMu.Unlock()
				}
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
		}
		if body == nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "history unavailable"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	mux.HandleFunc("POST /api/update/apply", func(w http.ResponseWriter, r *http.Request) {
		rel, err := up.Check(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		if rel == nil {
			writeJSON(w, http.StatusOK, map[string]any{"applied": false, "reason": "up to date"})
			return
		}
		// Respond before the restart cuts the connection.
		writeJSON(w, http.StatusAccepted, map[string]any{"applying": rel.Tag})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			if err := up.Apply(context.Background(), rel); err != nil {
				slog.Error("update apply failed", "err", err)
			}
		}()
	})

	// Everything else — the shared UI (with the pi-bridge overlay), /api/version,
	// /api/capabilities, the pc-bridge endpoint set, /pi/*, and the per-lamp
	// profile store — is the piweb-wrapped httpapi handler. Go 1.22 ServeMux
	// gives the specific patterns above precedence over this "/".
	mux.Handle("/", ui)

	return logRequests(mux)
}

func updateLoop(ctx context.Context, up *updater.Updater, every time.Duration, auto *atomic.Bool) {
	// small initial delay so a broken release doesn't insta-loop on boot
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if rel, err := up.Check(ctx); err != nil {
			slog.Warn("update check failed", "err", err)
		} else if rel != nil {
			if auto.Load() {
				slog.Info("update available, auto-applying", "tag", rel.Tag)
				if err := up.Apply(ctx, rel); err != nil {
					slog.Error("update apply failed", "err", err)
				}
			} else {
				slog.Info("update available (auto_update off — apply from the UI)", "tag", rel.Tag)
			}
		}
		timer.Reset(every)
	}
}

func parseInterval(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute {
		return time.Hour
	}
	return d
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("http", "method", r.Method, "path", r.URL.Path,
			"remote", r.RemoteAddr, "dur", time.Since(start).String())
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode json", "err", err)
	}
}
