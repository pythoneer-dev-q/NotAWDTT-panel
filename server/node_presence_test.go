package server

import "testing"

func TestNodePresenceReportsBindingsAndNewActivity(t *testing.T) {
	dbMutex.Lock()
	previousDB := db
	db = &Database{Passwords: map[string]*PasswordEntry{
		"user": {DeviceIDs: []string{"phone"}, MaxDevices: 2, LastSeenAt: 100, UpBytes: 10, DownBytes: 20},
	}}
	defer func() {
		db = previousDB
		dbMutex.Unlock()
	}()

	reported := make(map[string]reportedNodePresence)
	users, states := pendingNodeUserPresenceLocked(reported, map[string]bool{"user": true}, 500)
	if len(users) != 1 || users[0].Password != "user" || users[0].LastSeenAt != 100 ||
		len(users[0].DeviceIDs) != 1 || users[0].DeviceIDs[0] != "phone" || !users[0].Online ||
		users[0].UpBytes != 10 || users[0].DownBytes != 20 {
		t.Fatalf("node omitted bound device or last activity: %#v", users)
	}
	// Failed heartbeats leave the report pending; successful ones are acknowledged.
	if pending, _ := pendingNodeUserPresenceLocked(reported, nil, 500); len(pending) != 1 {
		t.Fatal("unacknowledged presence was dropped")
	}
	reported["user"] = states["user"]
	if pending, next := pendingNodeUserPresenceLocked(reported, nil, 500); len(pending) != 1 || pending[0].Online {
		t.Fatal("offline transition was not reported")
	} else {
		reported["user"] = next["user"]
	}
	if pending, _ := pendingNodeUserPresenceLocked(reported, nil, 500); len(pending) != 0 {
		t.Fatal("unchanged offline presence was sent again")
	}
	db.Passwords["user"].LastSeenAt = 101
	db.Passwords["user"].UpBytes = 15
	if pending, _ := pendingNodeUserPresenceLocked(reported, nil, 500); len(pending) != 1 || pending[0].LastSeenAt != 101 || len(pending[0].DeviceIDs) != 1 || pending[0].UpBytes != 15 {
		t.Fatal("new activity did not trigger a heartbeat update")
	}
}

func TestNodePresenceRefreshesOnlineUsers(t *testing.T) {
	dbMutex.Lock()
	previousDB := db
	db = &Database{Passwords: map[string]*PasswordEntry{"user": {LastSeenAt: 100}}}
	defer func() {
		db = previousDB
		dbMutex.Unlock()
	}()

	reported := map[string]reportedNodePresence{
		"user": {lastSeenAt: 100, online: true},
	}
	users, _ := pendingNodeUserPresenceLocked(reported, map[string]bool{"user": true}, 500)
	if len(users) != 1 || !users[0].Online {
		t.Fatalf("online refresh omitted: %#v", users)
	}
}
