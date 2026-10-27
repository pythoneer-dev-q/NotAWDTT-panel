package panel

import "testing"

func TestDashboardDeviceCountIncludesUserBindingsAndDeduplicates(t *testing.T) {
	db := &PasswordsDB{
		Devices: map[string]*DeviceEntry{"legacy": {}, "shared": {}},
		Passwords: map[string]*PasswordEntry{
			"one":   {DeviceID: "shared", DeviceIDs: []string{" shared ", "mobile"}},
			"two":   {DeviceIDs: []string{"mobile", "laptop"}},
			"empty": nil,
		},
	}
	if got := dashboardDeviceCount(db); got != 4 {
		t.Fatalf("got %d unique devices, want 4", got)
	}
	if got := dashboardDeviceCount(nil); got != 0 {
		t.Fatalf("nil database: got %d", got)
	}
}
