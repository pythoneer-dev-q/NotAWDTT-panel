package server

import "testing"

func TestNodePresenceReportsBindingsAndNewActivity(t *testing.T) {
	dbMutex.Lock()
	previousDB := db
	db = &Database{Passwords: map[string]*PasswordEntry{
		"user": {DeviceIDs: []string{"phone"}, MaxDevices: 2, LastSeenAt: 100},
	}}
	defer func() {
		db = previousDB
		dbMutex.Unlock()
	}()

	reported := make(map[string]reportedNodePresence)
	users, states := pendingNodeUserPresenceLocked(reported, 500)
	if len(users) != 1 || users[0].Password != "user" || users[0].LastSeenAt != 100 ||
		len(users[0].DeviceIDs) != 1 || users[0].DeviceIDs[0] != "phone" {
		t.Fatalf("node omitted bound device or last activity: %#v", users)
	}
	// Failed heartbeats leave the report pending; successful ones are acknowledged.
	if pending, _ := pendingNodeUserPresenceLocked(reported, 500); len(pending) != 1 {
		t.Fatal("unacknowledged presence was dropped")
	}
	reported["user"] = states["user"]
	if pending, _ := pendingNodeUserPresenceLocked(reported, 500); len(pending) != 0 {
		t.Fatal("unchanged presence was sent again")
	}
	db.Passwords["user"].LastSeenAt = 101
	if pending, _ := pendingNodeUserPresenceLocked(reported, 500); len(pending) != 1 || pending[0].LastSeenAt != 101 || len(pending[0].DeviceIDs) != 1 {
		t.Fatal("new activity did not trigger a heartbeat update")
	}
}
