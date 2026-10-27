package paneldb

import "testing"

func TestBumpUsersRev(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	before, err := LoadUsersRev(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := BumpUsersRev(db); err != nil {
		t.Fatal(err)
	}
	after, err := LoadUsersRev(db)
	if err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("revision did not advance: before=%d after=%d", before, after)
	}
}
