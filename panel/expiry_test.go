package panel

import (
	"encoding/json"
	"testing"
	"time"
)

func TestExpiryCalendarDays(t *testing.T) {
	loc := time.Local
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)

	cases := []struct {
		name string
		exp  time.Time
		want int
	}{
		{
			name: "end of same calendar day",
			exp:  time.Date(today.Year(), today.Month(), today.Day(), 23, 59, 59, 0, loc),
			want: 0,
		},
		{
			name: "16 days ahead midnight",
			exp:  today.AddDate(0, 0, 16),
			want: 16,
		},
		{
			name: "16 days ahead end of day",
			exp:  time.Date(today.Year(), today.Month(), today.Day(), 23, 59, 59, 0, loc).AddDate(0, 0, 16),
			want: 16,
		},
		{
			name: "yesterday",
			exp:  today.AddDate(0, 0, -1),
			want: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expiryCalendarDays(tc.exp.Unix())
			if got != tc.want {
				t.Fatalf("expiryCalendarDays(%v) = %d, want %d", tc.exp, got, tc.want)
			}
		})
	}
}

func TestUserTrafficLimitStatus(t *testing.T) {
	entry := &PasswordEntry{TotalBytes: 100, UpBytes: 40, DownBytes: 60}
	if !trafficExceeded(entry) {
		t.Fatal("panel did not mark exhausted quota")
	}
	entry.TotalBytes = 0
	if trafficExceeded(entry) {
		t.Fatal("unlimited user marked as exhausted")
	}
}

func TestPartialUserUpdatePreservesSpeedAndQuota(t *testing.T) {
	req := userUpdateReqFromEntry(&PasswordEntry{TotalBytes: 2 * oneGB, MaxDownMBps: 1.5, MaxUpMBps: 0.25})
	if err := json.Unmarshal([]byte(`{"active":false}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.TotalGB != 2 || req.MaxDownMBps != 1.5 || req.MaxUpMBps != 0.25 || req.Active == nil || *req.Active {
		t.Fatalf("partial update lost user limits: %#v", req)
	}
	if err := json.Unmarshal([]byte(`{"max_down_mbps":0}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.MaxDownMBps != 0 {
		t.Fatal("explicit zero did not remove the speed limit")
	}
}

func TestPasswordExpiryLabels(t *testing.T) {
	loc := time.Local
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)

	if got := passwordExpiry(nil); got != "бессрочно" {
		t.Fatalf("nil: got %q", got)
	}
	if got := passwordExpiry(&PasswordEntry{}); got != "бессрочно" {
		t.Fatalf("zero: got %q", got)
	}
	exp16 := today.AddDate(0, 0, 16)
	if got := passwordExpiry(&PasswordEntry{ExpiresAt: exp16.Unix()}); got != "16 дн." {
		t.Fatalf("16 days: got %q", got)
	}
	expToday := time.Date(today.Year(), today.Month(), today.Day(), 23, 59, 0, 0, loc)
	if got := passwordExpiry(&PasswordEntry{ExpiresAt: expToday.Unix()}); got != "сегодня" {
		t.Fatalf("today: got %q", got)
	}
}
