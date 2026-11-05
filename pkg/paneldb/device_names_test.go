package paneldb

import "testing"

func TestDeviceNameManualOverride(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if err := UpsertReportedDeviceName(db, "device-1", "Pixel 8"); err != nil {
		t.Fatal(err)
	}
	if err := SetDisplayDeviceName(db, "device-1", "Телефон"); err != nil {
		t.Fatal(err)
	}
	names, err := DeviceNames(db, []string{"device-1"})
	if err != nil || names["device-1"] != "Телефон" {
		t.Fatalf("manual label lost: names=%v err=%v", names, err)
	}
	if err := SetDisplayDeviceName(db, "device-1", ""); err != nil {
		t.Fatal(err)
	}
	names, err = DeviceNames(db, []string{"device-1"})
	if err != nil || names["device-1"] != "Pixel 8" {
		t.Fatalf("reported label not restored: names=%v err=%v", names, err)
	}
}
