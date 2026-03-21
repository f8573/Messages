package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ohmf/services/gateway/internal/config"
)

func TestPhase1ConfigLoadsPushAndRecoveryEnv(t *testing.T) {
	t.Setenv("APP_FIREBASE_PROJECT_ID", "proj-123")
	t.Setenv("APP_FIREBASE_CREDENTIALS_PATH", "C:/secrets/firebase.json")
	t.Setenv("APP_APNS_CERT_PATH", "C:/secrets/apns-cert.pem")
	t.Setenv("APP_APNS_KEY_PATH", "C:/secrets/apns-key.p8")
	t.Setenv("APP_APNS_BUNDLE_ID", "com.example.messaging")
	t.Setenv("APP_APNS_TEAM_ID", "TEAM12345")
	t.Setenv("APP_APNS_KEY_ID", "KEY12345")

	cfg := config.Load()
	if cfg.FirebaseProjectID != "proj-123" {
		t.Fatalf("expected firebase project id to load, got %q", cfg.FirebaseProjectID)
	}
	if cfg.FirebaseCredentialsPath != "C:/secrets/firebase.json" {
		t.Fatalf("expected firebase credentials path to load, got %q", cfg.FirebaseCredentialsPath)
	}
	if cfg.APNsCertPath != "C:/secrets/apns-cert.pem" {
		t.Fatalf("expected apns cert path to load, got %q", cfg.APNsCertPath)
	}
	if cfg.APNsKeyPath != "C:/secrets/apns-key.p8" {
		t.Fatalf("expected apns key path to load, got %q", cfg.APNsKeyPath)
	}
	if cfg.APNsBundleID != "com.example.messaging" {
		t.Fatalf("expected apns bundle id to load, got %q", cfg.APNsBundleID)
	}
	if cfg.APNsTeamID != "TEAM12345" {
		t.Fatalf("expected apns team id to load, got %q", cfg.APNsTeamID)
	}
	if cfg.APNsKeyID != "KEY12345" {
		t.Fatalf("expected apns key id to load, got %q", cfg.APNsKeyID)
	}
}

func TestPhase1RollbackMigrationsPresent(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file path")
	}
	migrationsDir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "migrations"))

	expectations := map[string][]string{
		"000030_read_receipts_enhancement.down.sql": {
			"DROP TABLE IF EXISTS message_read_receipts",
			"DROP COLUMN IF EXISTS read_at",
			"DROP COLUMN IF EXISTS delivery_at",
		},
		"000031_message_effects.down.sql": {
			"DROP COLUMN IF EXISTS allow_message_effects",
			"DROP TABLE IF EXISTS message_effects",
		},
		"000032_account_recovery.down.sql": {
			"DROP TABLE IF EXISTS two_factor_methods",
			"DROP TABLE IF EXISTS account_recovery_codes",
		},
		"000033_device_push_tokens.down.sql": {
			"DROP TABLE IF EXISTS device_push_tokens",
		},
	}

	for name, snippets := range expectations {
		data, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(data)
		for _, snippet := range snippets {
			if !strings.Contains(text, snippet) {
				t.Fatalf("%s missing %q", name, snippet)
			}
		}
	}
}
