package miniapp

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"testing"

	"ohmf/services/gateway/internal/config"
)

func TestVerifyManifestSignatureRS256(t *testing.T) {
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&pk.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	manifest := validManifest()
	payload, _ := json.Marshal(manifest)
	hash := sha256.Sum256(payload)
	sig, err := rsa.SignPKCS1v15(rand.Reader, pk, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	manifest["signature"] = map[string]any{
		"alg": "RS256",
		"kid": "rsa-key",
		"sig": base64.StdEncoding.EncodeToString(sig),
	}

	svc := NewService(nil, config.Config{MiniappPublicKeyPEM: string(pubPEM)})
	if err := svc.verifyManifestSignature(manifest); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestVerifyManifestSignatureEd25519(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	manifest := validManifest()
	payload, _ := json.Marshal(manifest)
	sig := ed25519.Sign(privKey, payload)
	manifest["signature"] = map[string]any{
		"alg": "Ed25519",
		"kid": "ed-key",
		"sig": base64.StdEncoding.EncodeToString(sig),
	}

	svc := NewService(nil, config.Config{MiniappPublicKeyPEM: string(pubPEM)})
	if err := svc.verifyManifestSignature(manifest); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestValidateManifestRejectsSchemaMismatch(t *testing.T) {
	if err := validateManifest(map[string]any{
		"app_id":      "com.example.invalid",
		"name":        "broken",
		"version":     "1.0",
		"entrypoint":  "https://example.com",
		"permissions": []string{"storage.session"},
		"capabilities": map[string]any{
			"storage.session": true,
		},
		"signature": "legacy-sig",
	}); err == nil {
		t.Fatalf("expected validation error")
	} else if !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected ErrManifestInvalid, got %v", err)
	}
}

func TestValidateManifestAllowsUnsignedWebBundle(t *testing.T) {
	manifest := validManifest()
	manifest["entrypoint"] = map[string]any{
		"type": "web_bundle",
		"url":  "https://example.com/app/index.html",
	}
	if err := validateManifest(manifest); err != nil {
		t.Fatalf("expected manifest to validate, got %v", err)
	}
}

func TestStateEnvelopeFromAnySupportsLegacyShape(t *testing.T) {
	state := stateEnvelopeFromAny(map[string]any{"turn": "usr_02"})
	if state.Snapshot["turn"] != "usr_02" {
		t.Fatalf("expected legacy state to be promoted into snapshot, got %#v", state.Snapshot)
	}
	if len(state.ProjectedMessages) != 0 {
		t.Fatalf("expected empty projected messages, got %#v", state.ProjectedMessages)
	}
}

func TestSanitizeGrantedPermissionsFiltersUndeclaredPermissions(t *testing.T) {
	got := sanitizeGrantedPermissions(
		[]string{"conversation.send_message", "device.identifiers", "conversation.read_context"},
		[]string{"conversation.send_message", "conversation.read_context"},
	)
	want := []string{"conversation.read_context", "conversation.send_message"}
	if len(got) != len(want) {
		t.Fatalf("expected %d permissions, got %#v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("permission mismatch at %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func validManifest() map[string]any {
	return map[string]any{
		"manifest_version": "1.0",
		"app_id":           "com.example.test",
		"name":             "test-app",
		"version":          "1.0.0",
		"entrypoint": map[string]any{
			"type": "url",
			"url":  "https://example.com/app",
		},
		"permissions": []string{
			"conversation.read_context",
			"conversation.send_message",
			"storage.session",
			"realtime.session",
		},
		"capabilities": map[string]any{
			"turn_based": true,
		},
	}
}
