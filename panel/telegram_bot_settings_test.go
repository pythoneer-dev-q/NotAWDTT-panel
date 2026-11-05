package panel

import "testing"

func TestTelegramBotSettingsRequireTokenAndAdmin(t *testing.T) {
	s := telegramBotSettings{Enabled: true, DefaultDays: 30, DefaultTrafficGB: 100, DefaultDevices: 2}
	if err := validateTelegramBotSettings(s); err == nil {
		t.Fatal("enabled bot without credentials was accepted")
	}
	s.Token = "123:token"
	s.AdminIDs = "123, 456"
	if err := validateTelegramBotSettings(s); err != nil {
		t.Fatal(err)
	}
	s.DefaultDevices = 21
	if err := validateTelegramBotSettings(s); err == nil {
		t.Fatal("device default above supported limit was accepted")
	}
}
