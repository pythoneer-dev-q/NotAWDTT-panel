package panel

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

func deviceNamesForPage(entries map[string]*PasswordEntry) map[string]string {
	ids := make([]string, 0)
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		for _, id := range allEntryDeviceIDsPanel(entry) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if !panelDBEnabled() {
		return map[string]string{}
	}
	names, err := paneldb.DeviceNames(panelDB, ids)
	if err != nil {
		return map[string]string{}
	}
	return names
}

func deviceNamesForEntry(entry *PasswordEntry, all map[string]string) map[string]string {
	names := map[string]string{}
	for _, id := range allEntryDeviceIDsPanel(entry) {
		if name := all[id]; name != "" {
			names[id] = name
		}
	}
	return names
}

func (a *App) handleDeviceName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !panelDBEnabled() {
		jsonError(w, "method or database unavailable", http.StatusBadRequest)
		return
	}
	var req struct {
		DeviceID string `json:"device_id"`
		Name     string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.Name = strings.TrimSpace(req.Name)
	if req.DeviceID == "" || len(req.DeviceID) > 4096 || len(req.Name) > 240 || !utf8.ValidString(req.Name) {
		jsonError(w, "invalid device ID or name", http.StatusBadRequest)
		return
	}
	if err := paneldb.SetDisplayDeviceName(panelDB, req.DeviceID, req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"device_id": req.DeviceID, "name": req.Name})
}
