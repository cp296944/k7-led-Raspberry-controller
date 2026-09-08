package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/k7tcp"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/lamp"
)

// Snapshot is the engine's authoritative input for one tick — the base
// schedule the user pushed plus all effect settings.
type Snapshot struct {
	Base     Schedule
	Manual   [Channels]int
	AutoMode bool
	Config   Config
}

// Provider hands the engine a fresh Snapshot each tick (implemented by the
// httpapi store).
type Provider interface {
	EngineSnapshot() Snapshot
}

// Override is a timed full-output replacement (Feed / Maintenance). Nil = none.
type Override struct {
	Channels [Channels]int
	Source   string
	Until    time.Time
}

// OutputStatus is what /api/output/status returns (mirrors Effects.h).
type OutputStatus struct {
	Target      [Channels]int `json:"target"`
	Sent        [Channels]int `json:"sent"`
	TargetMs    int64         `json:"target_ms"`
	SentMs      int64         `json:"sent_ms"`
	LastWriteOK bool          `json:"last_write_ok"`
	Source      string        `json:"source"`
}

type Engine struct {
	prov Provider
	lamp *lamp.Lamp
	tz   *time.Location

	mu       sync.Mutex
	status   OutputStatus
	lastSent [Channels]int
	haveSent bool
	override *Override
	interval time.Duration // live-tunable (smooth ramp)
	lastPush time.Time

	tickNow  chan struct{}
	reticker chan struct{}
}

func New(prov Provider, l *lamp.Lamp, tz *time.Location, interval time.Duration) *Engine {
	if tz == nil {
		tz = time.Local
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Engine{
		prov: prov, lamp: l, tz: tz, interval: interval,
		tickNow:  make(chan struct{}, 1),
		reticker: make(chan struct{}, 1),
	}
}

// Kick forces an immediate recompute+push (call after a user push / mode change).
func (e *Engine) Kick() {
	select {
	case e.tickNow <- struct{}{}:
	default:
	}
}

// SetInterval retunes the tick cadence at runtime (smooth ramp on/off).
func (e *Engine) SetInterval(d time.Duration) {
	if d < 15*time.Second {
		d = 15 * time.Second
	}
	e.mu.Lock()
	changed := e.interval != d
	e.interval = d
	e.mu.Unlock()
	if changed {
		slog.Info("engine interval changed", "interval", d)
		select {
		case e.reticker <- struct{}{}:
		default:
		}
	}
}

// Interval returns the current tick cadence.
func (e *Engine) Interval() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.interval
}

// LastPush is when the engine last successfully wrote to the lamp.
func (e *Engine) LastPush() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastPush
}

// SetOverride installs or clears a timed full-output override.
func (e *Engine) SetOverride(o *Override) {
	e.mu.Lock()
	e.override = o
	e.mu.Unlock()
	e.Kick()
}

func (e *Engine) OverrideActive() (bool, string, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.override == nil || time.Now().After(e.override.Until) {
		return false, "", time.Time{}
	}
	return true, e.override.Source, e.override.Until
}

func (e *Engine) Status() OutputStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// ClockSane reports whether the host clock looks set (post-2023).
func ClockSane() bool { return time.Now().Unix() > 1700000000 }

func (e *Engine) Run(ctx context.Context) {
	slog.Info("engine started", "interval", e.interval, "tz", e.tz.String())
	// prime the lamp clock, then tick
	_ = e.lamp.SyncTime()
	e.step()

	t := time.NewTicker(e.Interval())
	defer t.Stop()
	daily := time.NewTicker(6 * time.Hour)
	defer daily.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.step()
		case <-e.tickNow:
			e.step()
		case <-e.reticker:
			t.Reset(e.Interval())
		case <-daily.C:
			_ = e.lamp.SyncTime()
		}
	}
}

func (e *Engine) step() {
	now := time.Now().In(e.tz)
	if !ClockSane() {
		slog.Warn("engine: host clock not set, skipping tick")
		return
	}

	var out Output
	e.mu.Lock()
	ov := e.override
	e.mu.Unlock()
	if ov != nil && now.Before(ov.Until) {
		out = Output{Channels: ov.Channels, Source: ov.Source}
	} else {
		snap := e.prov.EngineSnapshot()
		out = snap.Config.Compute(snap.Base, snap.Manual, snap.AutoMode, now)
	}

	e.mu.Lock()
	e.status.Target = out.Channels
	e.status.TargetMs = now.UnixMilli()
	e.status.Source = out.Source
	changed := !e.haveSent || out.Channels != e.lastSent
	e.mu.Unlock()

	if !changed {
		return
	}

	err := e.lamp.Hand(toU8(out.Channels))
	e.mu.Lock()
	e.status.LastWriteOK = err == nil
	if err == nil {
		e.status.Sent = out.Channels
		e.status.SentMs = time.Now().UnixMilli()
		e.lastSent = out.Channels
		e.haveSent = true
		e.lastPush = time.Now()
	}
	e.mu.Unlock()
	if err != nil {
		slog.Warn("engine: lamp write failed", "err", err)
	} else {
		slog.Debug("engine: pushed", "src", out.Source, "ch", out.Channels)
	}
}

func toU8(in [Channels]int) [k7tcp.Channels]uint8 {
	var out [k7tcp.Channels]uint8
	for i := 0; i < Channels && i < k7tcp.Channels; i++ {
		v := in[i]
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		out[i] = uint8(v)
	}
	return out
}
