package miniapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"ohmf/services/gateway/internal/config"
)

func TestVerifyManifestSignature(t *testing.T) {
	// generate test RSA key
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&pk.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	// create sample manifest
	manifest := map[string]any{
		"app_id":     "com.example.test",
		"name":       "test-app",
		"version":    "1.0.0",
		"owner":      uuid.New().String(),
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"entrypoint": map[string]any{
			"type": "url",
			"url":  "https://example.com/app",
		},
		"permissions":  []string{"storage"},
		"capabilities": map[string]any{"storage": true},
	}

	// prepare payload without signature
	payload, _ := json.Marshal(manifest)
	h := sha256.Sum256(payload)
	sig, err := rsa.SignPKCS1v15(rand.Reader, pk, crypto.SHA256, h[:])
	if err != nil {
		t.Fatal(err)
	}
	manifest["signature"] = map[string]any{
		"alg": "RS256",
		"kid": "test-key",
		"sig": base64.StdEncoding.EncodeToString(sig),
	}

	// construct service with public key
	cfg := config.Config{MiniappPublicKeyPEM: string(pubPem)}
	s := NewService(nil, cfg)

	// verify
	if err := s.verifyManifestSignature(manifest); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestValidateManifestRejectsSchemaMismatch(t *testing.T) {
	svc := &Service{}
	_, err := svc.RegisterManifest(nil, "owner-1", map[string]any{
		"app_id":      "com.example.invalid",
		"name":        "broken",
		"version":     "1.0",
		"entrypoint":  "https://example.com",
		"permissions": []string{"storage"},
		"capabilities": map[string]any{
			"storage": true,
		},
		"signature": "legacy-sig",
	})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected ErrManifestInvalid, got %v", err)
	}
}
