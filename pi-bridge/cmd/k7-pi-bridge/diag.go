package main

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/engine"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/lamp"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/tally"
)

// diagAPI powers the 7-day soak check: an hourly line in data/soak.log
// (RSS, heap, goroutines, GC count, lamp-op health, today's lamp writes) plus
// GET /api/diag = the current snapshot + the tail of that log. Reviewing a week
// of lines shows at a glance whether memory creeps, the engine hammers the lamp,
// or reconnects are clean.
type diagAPI struct {
	dataDir string
	started time.Time
	eng     *engine.Engine
	lamp    *lamp.Lamp
	tally   *tally.Counter
	version string
}

func (d *diagAPI) logPath() string { return filepath.Join(d.dataDir, "soak.log") }

func (d *diagAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/diag", d.handleGet)
}

func (d *diagAPI) snapshot() map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	auto, manual, day := d.tally.Today()
	ls := d.lamp.Stats()
	st := d.eng.Status()
	return map[string]any{
		"ts":            time.Now().Format(time.RFC3339),
		"version":       d.version,
		"uptime_s":      int(time.Since(d.started).Seconds()),
		"rss_kb":        rssKB(),
		"heap_kb":       int(ms.HeapAlloc / 1024),
		"heap_sys_kb":   int(ms.HeapSys / 1024),
		"goroutines":    runtime.NumGoroutine(),
		"gc_count":      int(ms.NumGC),
		"engine_live":   d.eng.Live(),
		"last_write_ok": st.LastWriteOK,
		"lamp_ops":      ls.Ops,
		"lamp_fails":    ls.Fails,
		"lamp_consec_fail": ls.ConsecFail,
		"writes_today":  map[string]any{"auto": auto, "manual": manual, "date": day},
	}
}

func (d *diagAPI) logLine() string {
	s := d.snapshot()
	wt := s["writes_today"].(map[string]any)
	return strings.Join([]string{
		s["ts"].(string),
		"ver=" + d.version,
		"up=" + itoa(s["uptime_s"]),
		"rss_kb=" + itoa(s["rss_kb"]),
		"heap_kb=" + itoa(s["heap_kb"]),
		"goroutines=" + itoa(s["goroutines"]),
		"gc=" + itoa(s["gc_count"]),
		"live=" + btoa(s["engine_live"]),
		"lamp_ops=" + itoa(s["lamp_ops"]),
		"lamp_fails=" + itoa(s["lamp_fails"]),
		"lamp_consec_fail=" + itoa(s["lamp_consec_fail"]),
		"w_auto=" + itoa(wt["auto"]),
		"w_manual=" + itoa(wt["manual"]),
	}, "  ")
}

func (d *diagAPI) append() {
	f, err := os.OpenFile(d.logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(d.logLine() + "\n")
}

func (d *diagAPI) run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Hour
	}
	// one line at startup so a restart is visible in the trail
	d.append()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.append()
		}
	}
}

func (d *diagAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	n := 300
	if v := r.URL.Query().Get("lines"); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k > 0 && k <= 5000 {
			n = k
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"now": d.snapshot(),
		"log": tailLines(d.logPath(), n),
	})
}

// ---- helpers ----

func rssKB() int {
	f, err := os.Open("/proc/self/status") // Linux only; 0 elsewhere (dev on Windows/mac)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "VmRSS:") {
			fs := strings.Fields(sc.Text())
			if len(fs) >= 2 {
				if kb, e := strconv.Atoi(fs[1]); e == nil {
					return kb
				}
			}
		}
	}
	return 0
}

func tailLines(path string, n int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func itoa(v any) string {
	switch x := v.(type) {
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return "0"
	}
}
func btoa(v any) string {
	if b, ok := v.(bool); ok && b {
		return "1"
	}
	return "0"
}
