package config

import "testing"

func TestLoadIncludesSecurityTunables(t *testing.T) {
	t.Setenv("APP_DISCOVERY_MAX_CONTACTS", "128")
	t.Setenv("APP_DISCOVERY_RATE_PER_USER", "7")
	t.Setenv("APP_OTP_START_PER_PHONE_LIMIT", "3")
	t.Setenv("APP_OTP_VERIFY_PER_PHONE_LIMIT", "4")
	t.Setenv("APP_OTP_VERIFY_WINDOW_MINUTES", "12")

	cfg := Load()
	if cfg.DiscoveryMaxContacts != 128 {
		t.Fatalf("expected discovery max contacts 128, got %d", cfg.DiscoveryMaxContacts)
	}
	if cfg.DiscoveryRatePerUser != 7 {
		t.Fatalf("expected discovery rate per user 7, got %d", cfg.DiscoveryRatePerUser)
	}
	if cfg.OTPStartPerPhoneLimit != 3 {
		t.Fatalf("expected OTP start per phone limit 3, got %d", cfg.OTPStartPerPhoneLimit)
	}
	if cfg.OTPVerifyPerPhone != 4 {
		t.Fatalf("expected OTP verify per phone limit 4, got %d", cfg.OTPVerifyPerPhone)
	}
	if cfg.OTPVerifyWindow.Minutes() != 12 {
		t.Fatalf("expected OTP verify window 12 minutes, got %v", cfg.OTPVerifyWindow)
	}
}
