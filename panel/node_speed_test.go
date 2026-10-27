package panel

import (
	"testing"
	"time"
)

func TestNodeSpeedTracker(t *testing.T) {
	tracker := nodeSpeedTracker{samples: make(map[string]nodeSpeedSample)}
	start := time.Unix(1000, 0)
	tracker.record("a", 1000, 2000, start)
	if _, _, ok := tracker.get("a", start); ok {
		t.Fatal("first heartbeat must not report a speed")
	}
	tracker.record("a", 3000, 6000, start.Add(10*time.Second))
	up, down, ok := tracker.get("a", start.Add(10*time.Second))
	if !ok || up != 200 || down != 400 {
		t.Fatalf("unexpected rate: %d %d %v", up, down, ok)
	}
	if _, _, ok := tracker.get("a", start.Add(46*time.Second)); ok {
		t.Fatal("stale speed must be unavailable")
	}
	tracker.record("a", 1, 2, start.Add(20*time.Second))
	if _, _, ok := tracker.get("a", start.Add(20*time.Second)); ok {
		t.Fatal("counter reset needs a new baseline")
	}
	tracker.record("a", 1001, 2002, start.Add(30*time.Second))
	up, down, ok = tracker.get("a", start.Add(30*time.Second))
	if !ok || up != 100 || down != 200 {
		t.Fatalf("bad rate after reset: %d %d %v", up, down, ok)
	}
}
