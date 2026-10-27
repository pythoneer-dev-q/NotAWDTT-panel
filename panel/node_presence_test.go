package panel

import (
	"testing"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
)

func TestNodePresenceRejectsInvalidActivity(t *testing.T) {
	now := int64(1000)
	if err := validateNodeUserPresence([]paneldb.NodeUserPresence{{
		Password: "user", DeviceIDs: []string{"phone"}, LastSeenAt: now,
	}}, now); err != nil {
		t.Fatalf("valid node activity rejected: %v", err)
	}
	for _, user := range []paneldb.NodeUserPresence{
		{Password: "user", LastSeenAt: now + 61},
		{Password: "user", DeviceIDs: []string{"  "}},
		{Password: "user", UpBytes: -1},
		{Password: "user", DownBytes: 1 << 61},
		{Password: "", DeviceIDs: []string{"phone"}},
	} {
		if err := validateNodeUserPresence([]paneldb.NodeUserPresence{user}, now); err == nil {
			t.Fatalf("invalid node activity accepted: %#v", user)
		}
	}
}

func TestMostRecentUserOwnsSharedDeviceInPanel(t *testing.T) {
	db := &PasswordsDB{Passwords: map[string]*PasswordEntry{
		"old": {DeviceIDs: []string{"phone"}, LastSeenAt: 10},
		"new": {DeviceIDs: []string{"phone"}, LastSeenAt: 20},
	}}
	dedupePasswordDeviceBindings(db)
	if len(db.Passwords["old"].DeviceIDs) != 0 || len(db.Passwords["new"].DeviceIDs) != 1 {
		t.Fatalf("device shown on stale user: old=%v new=%v", db.Passwords["old"].DeviceIDs, db.Passwords["new"].DeviceIDs)
	}
}
