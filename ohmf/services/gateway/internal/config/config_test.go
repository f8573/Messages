package config

import (
	"testing"
	"time"
)

func TestLoadIncludesSecurityTunables(t *testing.T) {
	t.Setenv("APP_DISCOVERY_MAX_CONTACTS", "128")
	t.Setenv("APP_DISCOVERY_RATE_PER_USER", "7")
	t.Setenv("APP_OTP_START_PER_PHONE_LIMIT", "3")
	t.Setenv("APP_OTP_VERIFY_PER_PHONE_LIMIT", "4")
	t.Setenv("APP_OTP_VERIFY_WINDOW_MINUTES", "12")
	t.Setenv("APP_WS_CONNECT_WINDOW_SECONDS", "30")
	t.Setenv("APP_WS_CONNECT_PER_IP_LIMIT", "25")
	t.Setenv("APP_WS_CONNECT_BURST", "50")

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
	if cfg.WSConnectWindow != 30*time.Second {
		t.Fatalf("expected ws connect window 30s, got %v", cfg.WSConnectWindow)
	}
	if cfg.WSConnectPerIPLimit != 25 {
		t.Fatalf("expected ws connect per ip limit 25, got %d", cfg.WSConnectPerIPLimit)
	}
	if cfg.WSConnectBurst != 50 {
		t.Fatalf("expected ws connect burst 50, got %d", cfg.WSConnectBurst)
	}
}

func TestLoadAllowsAppsRegistryToBeDisabled(t *testing.T) {
	t.Setenv("APP_APPS_ADDR", "")

	cfg := Load()
	if cfg.AppsAddr != "" {
		t.Fatalf("expected empty apps addr, got %q", cfg.AppsAddr)
	}
}
