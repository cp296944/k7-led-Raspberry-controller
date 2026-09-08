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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/config"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/httpapi"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/piweb"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/profiles"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/proxy"
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

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("starting", "version", version.String(), "listen", cfg.Listen,
		"lamp", fmt.Sprintf("%s:%d", cfg.LampHost, cfg.LampPort),
		"install_root", cfg.InstallRoot, "data_dir", cfg.DataDir)

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
	if d := parseInterval(cfg.UpdateInterval); d > 0 {
		go updateLoop(ctx, up, d)
	}

	api, err := httpapi.New(httpapi.Options{
		ConfigPath: filepath.Join(cfg.DataDir, "store.json"),
		Version:    version.Version,
		Timeout:    5 * time.Second,
		Device:     "k7pro",
		LampHost:   cfg.LampHost,
		LampPort:   cfg.LampPort,
	})
	if err != nil {
		return fmt.Errorf("http api: %w", err)
	}

	// Per-lamp profile store (isolated from OTA; keyed by lamp MAC/name).
	profStore := profiles.New(cfg.DataDir, cfg.LampHost)
	profStore.NameHint = api.LampName
	profStore.Migrate(profStore.LampID(ctx), api.LegacyProfiles())

	// pi-bridge UX layer over the unmodified upstream UI.
	uiHandler := piweb.Wrap(piweb.Deps{
		Next:       api.Routes(),
		Profiles:   profStore,
		Version:    version.Version,
		UpdateRepo: cfg.UpdateRepo,
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           routes(cfg, up, uiHandler),
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

func routes(cfg config.Config, up *updater.Updater, ui http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("GET /api/update/status", func(w http.ResponseWriter, r *http.Request) {
		rel, err := up.Check(r.Context())
		resp := map[string]any{"current": version.Version, "channel": cfg.UpdateChannel, "repo": cfg.UpdateRepo}
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

func updateLoop(ctx context.Context, up *updater.Updater, every time.Duration) {
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
			slog.Info("update available, applying", "tag", rel.Tag)
			if err := up.Apply(ctx, rel); err != nil {
				slog.Error("update apply failed", "err", err)
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
