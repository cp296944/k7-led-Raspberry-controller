package engine

import (
	"context"
	"testing"
	"time"

	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/lamp"
)

type fakeProv struct{ s Snapshot }

func (f fakeProv) EngineSnapshot() Snapshot { return f.s }

func TestSetInterval(t *testing.T) {
	e := New(fakeProv{}, lamp.New("127.0.0.1", 1), time.UTC, time.Minute)
	if e.Interval() != time.Minute {
		t.Fatalf("initial interval = %v", e.Interval())
	}
	e.SetInterval(5 * time.Second) // clamped to 15s
	if e.Interval() != 15*time.Second {
		t.Errorf("clamp: got %v want 15s", e.Interval())
	}
	e.SetInterval(3 * time.Minute)
	if e.Interval() != 3*time.Minute {
		t.Errorf("set: got %v", e.Interval())
	}
}

func TestOverride(t *testing.T) {
	e := New(fakeProv{}, lamp.New("127.0.0.1", 1), time.UTC, time.Minute)
	if a, _, _ := e.OverrideActive(); a {
		t.Fatal("no override expected initially")
	}
	e.SetOverride(&Override{Channels: [Channels]int{9, 9, 9, 9, 9, 9}, Source: "feed", Until: time.Now().Add(time.Hour)})
	a, src, _ := e.OverrideActive()
	if !a || src != "feed" {
		t.Errorf("override = %v %q", a, src)
	}
	// expired override reads inactive
	e.SetOverride(&Override{Source: "feed", Until: time.Now().Add(-time.Minute)})
	if a, _, _ := e.OverrideActive(); a {
		t.Error("expired override should read inactive")
	}
	e.SetOverride(nil)
	if a, _, _ := e.OverrideActive(); a {
		t.Error("nil override")
	}
}

func TestStepAppliesOverride(t *testing.T) {
	// engine.step with an active override should target the override channels
	// even when the schedule says otherwise. We can't reach a real lamp, so
	// just check status tracking (lamp.Hand will fail fast to 127.0.0.1:1).
	var s Snapshot
	s.AutoMode = true
	e := New(fakeProv{s: s}, lamp.New("127.0.0.1", 1), time.UTC, time.Minute)
	e.SetOverride(&Override{Channels: [Channels]int{5, 4, 3, 2, 1, 0}, Source: "maintenance", Until: time.Now().Add(time.Hour)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = ctx
	e.step()
	st := e.Status()
	if st.Source != "maintenance" || st.Target != [Channels]int{5, 4, 3, 2, 1, 0} {
		t.Errorf("status after override step = %+v", st)
	}
}
