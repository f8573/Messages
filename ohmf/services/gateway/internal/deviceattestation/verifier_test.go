package deviceattestation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestVerifyAcceptsSignedMatchingPayload(t *testing.T) {
	claims := map[string]any{
		"platform":                 "ANDROID",
		"app_id":                   "com.ohmf.android",
		"nonce":                    "nonce-1",
		"device_id":                "device-1",
		"device_public_key_sha256": "hash-1",
		"issued_at":                time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
		"expires_at":               time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano),
		"verdict":                  "ALLOW",
	}
	raw, _ := json.Marshal(claims)
	mac := hmac.New(sha256.New, []byte("secret-1"))
	mac.Write(raw)

	verifier := NewVerifier(Config{
		Secret:       "secret-1",
		AndroidAppID: "com.ohmf.android",
	})
	payload, expiresAt, err := verifier.Verify(Statement{
		AttestationType: "ANDROID_PLAY_INTEGRITY",
		Payload:         base64.RawStdEncoding.EncodeToString(raw),
		Signature:       base64.RawStdEncoding.EncodeToString(mac.Sum(nil)),
	}, Expected{
		Platform:            "ANDROID",
		DeviceID:            "device-1",
		Nonce:               "nonce-1",
		DevicePublicKeyHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if payload["attestation_type"] != "ANDROID_PLAY_INTEGRITY" {
		t.Fatalf("unexpected attestation type: %#v", payload["attestation_type"])
	}
	if expiresAt.Before(time.Now().UTC()) {
		t.Fatalf("unexpected expiry: %v", expiresAt)
	}
}
