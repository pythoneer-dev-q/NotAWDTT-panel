package panel

import (
	"sync"
	"time"
)

// Node traffic totals are cumulative counters reported by the node itself.
// Rates are valid only after two successful heartbeats and expire with them.
type nodeSpeedSample struct {
	up, down         int64
	at               time.Time
	upRate, downRate uint64
	valid            bool
}

type nodeSpeedTracker struct {
	mu      sync.Mutex
	samples map[string]nodeSpeedSample
}

var clusterSpeeds = nodeSpeedTracker{samples: make(map[string]nodeSpeedSample)}

func (t *nodeSpeedTracker) record(id string, up, down int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	previous, exists := t.samples[id]
	next := nodeSpeedSample{up: up, down: down, at: now}
	if exists && now.After(previous.at) && now.Sub(previous.at) <= 60*time.Second && up >= previous.up && down >= previous.down {
		seconds := now.Sub(previous.at).Seconds()
		next.upRate = uint64(float64(up-previous.up) / seconds)
		next.downRate = uint64(float64(down-previous.down) / seconds)
		next.valid = true
	}
	t.samples[id] = next
	for key, sample := range t.samples {
		if now.Sub(sample.at) > 10*time.Minute {
			delete(t.samples, key)
		}
	}
}

func (t *nodeSpeedTracker) get(id string, now time.Time) (uint64, uint64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sample, ok := t.samples[id]
	if !ok || !sample.valid || now.Before(sample.at) || now.Sub(sample.at) > 35*time.Second {
		return 0, 0, false
	}
	return sample.upRate, sample.downRate, true
}
