// Package lamp is the single owner of talking to the K7 over TCP: every write
// goes through one mutex so the always-on engine, the HTTP API and the raw
// proxy never step on each other (the lamp has no locking and accepts one
// connection at a time).
//
// It wraps internal/k7tcp with pi-bridge-appropriate (generous, retrying)
// timeouts — the ESP32 firmware assumes it is inches from the lamp; a Pi may be
// across the tank room.
package lamp

import (
	"log/slog"
	"sync"
	"time"

	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/k7tcp"
)

type Lamp struct {
	host string
	port int

	mu   sync.Mutex // the lamp gate
	last struct {
		okAt   time.Time
		failAt time.Time
		err    string
	}
}

func New(host string, port int) *Lamp {
	if port == 0 {
		port = k7tcp.DefaultPort
	}
	return &Lamp{host: host, port: port}
}

// Gate exposes the mutex so the proxy can hold it for a whole client session.
func (l *Lamp) Gate() *sync.Mutex { return &l.mu }

func (l *Lamp) client(timeout time.Duration) k7tcp.Client {
	return k7tcp.New(l.host, l.port, timeout)
}

// Health reports the last-known link state for /api/wifi/signal etc.
type Health struct {
	OK        bool      `json:"ok"`
	LastOKAt  time.Time `json:"last_ok_at,omitempty"`
	LastErrAt time.Time `json:"last_err_at,omitempty"`
	LastErr   string    `json:"last_err,omitempty"`
}

func (l *Lamp) Health() Health {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Health{
		OK:        !l.last.okAt.IsZero() && l.last.okAt.After(l.last.failAt),
		LastOKAt:  l.last.okAt,
		LastErrAt: l.last.failAt,
		LastErr:   l.last.err,
	}
}

func (l *Lamp) do(name string, timeout time.Duration, fn func(k7tcp.Client) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	err := fn(l.client(timeout))
	if err != nil {
		l.last.failAt = time.Now()
		l.last.err = err.Error()
		slog.Warn("lamp op failed", "op", name, "err", err)
	} else {
		l.last.okAt = time.Now()
	}
	return err
}

// Hand pushes live per-channel luminance (0..255) with an ack wait (0x1005).
func (l *Lamp) Hand(ch [k7tcp.Channels]uint8) error {
	return l.do("hand", 3*time.Second, func(c k7tcp.Client) error { return c.HandLuminance(ch) })
}

// Preview is a non-persisting preview (0x1006).
func (l *Lamp) Preview(ch [k7tcp.Channels]uint8) error {
	return l.do("preview", 2*time.Second, func(c k7tcp.Client) error { return c.PreviewBrightness(ch) })
}

func (l *Lamp) ReadAll() (k7tcp.LampState, error) {
	var st k7tcp.LampState
	err := l.do("readAll", 8*time.Second, func(c k7tcp.Client) error {
		var e error
		st, e = c.ReadAll()
		return e
	})
	return st, err
}

func (l *Lamp) SyncTime() error {
	return l.do("syncTime", 3*time.Second, func(c k7tcp.Client) error { return c.SyncTimeLocal() })
}

func (l *Lamp) SetModeManual() error {
	return l.do("modeManual", 3*time.Second, func(c k7tcp.Client) error { return c.SetModeManual() })
}

func (l *Lamp) SetModeAuto() error {
	return l.do("modeAuto", 3*time.Second, func(c k7tcp.Client) error { return c.SetModeAuto() })
}

func (l *Lamp) PushSchedule(manual [k7tcp.Channels]uint8, sched [k7tcp.Slots][8]uint8, auto bool) error {
	return l.do("pushSchedule", 6*time.Second, func(c k7tcp.Client) error {
		return c.PushSchedule(manual, sched, auto)
	})
}
