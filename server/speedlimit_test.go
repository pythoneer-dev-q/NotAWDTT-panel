package server

import "testing"

func TestMbpsToTcRate(t *testing.T) {
	cases := []struct {
		mbPerSec float64
		want     string
	}{
		{0, "8kbit"},
		{0.0125, "100kbit"}, // UI 0.1 Мбит/с
		{0.25, "2mbit"},     // UI 2 Мбит/с
		{1.0, "8mbit"},      // UI 8 Мбит/с
		{0.001, "8kbit"},    // floor
	}
	for _, c := range cases {
		got := mbpsToTcRate(c.mbPerSec)
		if got != c.want {
			t.Fatalf("mbpsToTcRate(%v)=%q want %q", c.mbPerSec, got, c.want)
		}
	}
}

func TestTcNeedsHTBReset(t *testing.T) {
	fq := "qdisc fq 0: root refcnt 2 limit 10000p"
	if !tcNeedsHTBReset(fq) {
		t.Fatal("fq root must reset")
	}
	htb := "qdisc htb 1: root refcnt 2 r2q 10 default 0x999"
	if tcNeedsHTBReset(htb) {
		t.Fatal("clean HTB must not reset")
	}
	mixed := fq + "\n" + htb
	if !tcNeedsHTBReset(mixed) {
		t.Fatal("fq+htb dual root must reset")
	}
}

func TestIPClassIDsUseBothAddressOctets(t *testing.T) {
	a, err := ipClassID("10.70.66.2")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ipClassID("10.70.67.2")
	if err != nil || a == b {
		t.Fatalf("different RAW clients share a tc class: %d, %d (%v)", a, b, err)
	}
	reserved, err := ipClassID("10.70.3.231")
	if err != nil || reserved == 999 {
		t.Fatalf("tc class conflicts with default class: %d (%v)", reserved, err)
	}
	first, err := ipClassID("10.70.0.1")
	if err != nil || first == 1 || first == reserved {
		t.Fatalf("first RAW client conflicts with parent or default class: %d (%v)", first, err)
	}
}

func TestNodeSyncRestoresPerUserSpeedLimits(t *testing.T) {
	dbMutex.Lock()
	previousDB, previousKeys := db, serverWrapKeys
	db = &Database{Passwords: make(map[string]*PasswordEntry), Devices: make(map[string]*ClientDevice)}
	serverWrapKeys = newWrapKeyStore()
	dbMutex.Unlock()
	defer func() {
		dbMutex.Lock()
		db, serverWrapKeys = previousDB, previousKeys
		dbMutex.Unlock()
	}()

	ApplyNodeSyncUsers([]NodeSyncUser{{Password: "user-a", MaxDownMBps: 1.5, MaxUpMBps: 0.25}})
	dbMutex.Lock()
	entry := db.Passwords["user-a"]
	ok := entry != nil && entry.MaxDownMBps == 1.5 && entry.MaxUpMBps == 0.25
	dbMutex.Unlock()
	if !ok {
		t.Fatalf("node did not receive limits: %#v", entry)
	}
	ApplyNodeSyncUsers([]NodeSyncUser{{Password: "user-a", MaxDownMBps: 0, MaxUpMBps: 2}})
	dbMutex.Lock()
	entry = db.Passwords["user-a"]
	ok = entry != nil && entry.MaxDownMBps == 0 && entry.MaxUpMBps == 2
	dbMutex.Unlock()
	if !ok {
		t.Fatalf("node did not refresh limits: %#v", entry)
	}
}
