package paneldb

import (
	"database/sql"
	"fmt"
	"strings"
)

func EnsureDeviceNamesTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS wdtt_device_names (device_id TEXT PRIMARY KEY, reported_name TEXT NOT NULL DEFAULT '', display_name TEXT NOT NULL DEFAULT '')`)
	return err
}

func UpsertReportedDeviceName(db *sql.DB, deviceID, name string) error {
	if db == nil || deviceID == "" || name == "" {
		return nil
	}
	if err := EnsureDeviceNamesTable(db); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO wdtt_device_names (device_id, reported_name) VALUES (?, ?) ON CONFLICT(device_id) DO UPDATE SET reported_name = excluded.reported_name`, deviceID, name)
	return err
}

func SetDisplayDeviceName(db *sql.DB, deviceID, name string) error {
	if err := EnsureDeviceNamesTable(db); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO wdtt_device_names (device_id, display_name) VALUES (?, ?) ON CONFLICT(device_id) DO UPDATE SET display_name = excluded.display_name`, deviceID, name)
	return err
}

func DeviceNames(db *sql.DB, ids []string) (map[string]string, error) {
	names := make(map[string]string)
	if db == nil || len(ids) == 0 {
		return names, nil
	}
	if len(ids) > 4000 {
		return nil, fmt.Errorf("too many device IDs")
	}
	if err := EnsureDeviceNamesTable(db); err != nil {
		return nil, err
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.Query(`SELECT device_id, reported_name, display_name FROM wdtt_device_names WHERE device_id IN (`+strings.TrimRight(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, reported, display string
		if err := rows.Scan(&id, &reported, &display); err != nil {
			return nil, err
		}
		if display != "" {
			names[id] = display
		} else if reported != "" {
			names[id] = reported
		}
	}
	return names, rows.Err()
}
