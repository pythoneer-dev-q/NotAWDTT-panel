package paneldb

import "testing"

func TestConfiguredMaxDevicesLimit(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{"", MaxDevicesLimit},
		{"3", 3},
		{"0", MaxDevicesLimit},
		{"21", MaxDevicesLimit},
		{"bad", MaxDevicesLimit},
	} {
		t.Setenv("WDTT_MAX_DEVICES_PER_USER", tc.raw)
		if got := ConfiguredMaxDevicesLimit(); got != tc.want {
			t.Errorf("value %q: got %d, want %d", tc.raw, got, tc.want)
		}
	}
}
