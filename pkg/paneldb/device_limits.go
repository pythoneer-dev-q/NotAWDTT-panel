package paneldb

import (
	"os"
	"strconv"
	"strings"
)

// ConfiguredMaxDevicesLimit is the global cap for non-admin users. An invalid
// value falls back to the historical limit so a typo cannot disable binding.
func ConfiguredMaxDevicesLimit() int {
	raw := strings.TrimSpace(os.Getenv("WDTT_MAX_DEVICES_PER_USER"))
	if raw == "" {
		return MaxDevicesLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > MaxDevicesLimit {
		return MaxDevicesLimit
	}
	return limit
}
