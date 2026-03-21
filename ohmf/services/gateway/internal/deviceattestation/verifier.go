package deviceattestation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Config struct {
	Secret       string
	AndroidAppID string
	IOSAppID     string
	WebAppID     string
	MaxSkew      time.Duration
}

type Verifier struct {
	cfg Config
}

type Statement struct {
	AttestationType string `json:"attestation_type"`
	Payload         string `json:"payload"`
	Signature       string `json:"signature"`
}

type Expected struct {
	Platform            string
	DeviceID            string
	Nonce               string
	DevicePublicKeyHash string
}

var (
	ErrVerifierDisabled     = errors.New("device_attestation_disabled")
	ErrInvalidStatement     = errors.New("invalid_attestation_statement")
	ErrInvalidSignature     = errors.New("invalid_attestation_signature")
	ErrInvalidChallenge     = errors.New("invalid_attestation_challenge")
	ErrInvalidAppID         = errors.New("invalid_attestation_app_id")
	ErrInvalidPlatform      = errors.New("invalid_attestation_platform")
	ErrInvalidDeviceBinding = errors.New("invalid_attestation_device_binding")
	ErrUntrustedVerdict     = errors.New("untrusted_attestation_verdict")
	ErrExpiredStatement     = errors.New("expired_attestation_statement")
)

func NewVerifier(cfg Config) *Verifier {
	if cfg.MaxSkew <= 0 {
		cfg.MaxSkew = 5 * time.Minute
	}
	return &Verifier{cfg: cfg}
}

func (v *Verifier) Enabled() bool {
	return v != nil && strings.TrimSpace(v.cfg.Secret) != ""
}

func (v *Verifier) Verify(statement Statement, expected Expected) (map[string]any, time.Time, error) {
	if !v.Enabled() {
		return nil, time.Time{}, ErrVerifierDisabled
	}
	if strings.TrimSpace(statement.Payload) == "" || strings.TrimSpace(statement.Signature) == "" {
		return nil, time.Time{}, ErrInvalidStatement
	}
	payloadRaw, err := decodeBase64(statement.Payload)
	if err != nil {
		return nil, time.Time{}, ErrInvalidStatement
	}
	signatureRaw, err := decodeBase64(statement.Signature)
	if err != nil {
		return nil, time.Time{}, ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(v.cfg.Secret))
	mac.Write(payloadRaw)
	if !hmac.Equal(signatureRaw, mac.Sum(nil)) {
		return nil, time.Time{}, ErrInvalidSignature
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return nil, time.Time{}, ErrInvalidStatement
	}

	platform := strings.ToUpper(strings.TrimSpace(stringValue(payload, "platform")))
	if platform == "" || platform != strings.ToUpper(strings.TrimSpace(expected.Platform)) {
		return nil, time.Time{}, ErrInvalidPlatform
	}
	if nonce := strings.TrimSpace(stringValue(payload, "nonce")); nonce == "" || nonce != expected.Nonce {
		return nil, time.Time{}, ErrInvalidChallenge
	}
	if deviceID := strings.TrimSpace(stringValue(payload, "device_id")); deviceID != "" && deviceID != expected.DeviceID {
		return nil, time.Time{}, ErrInvalidDeviceBinding
	}
	if expected.DevicePublicKeyHash != "" {
		if got := strings.TrimSpace(stringValue(payload, "device_public_key_sha256")); got != "" && !strings.EqualFold(got, expected.DevicePublicKeyHash) {
			return nil, time.Time{}, ErrInvalidDeviceBinding
		}
	}

	if appID := strings.TrimSpace(stringValue(payload, "app_id")); appID != "" {
		if expectedAppID := v.expectedAppID(platform); expectedAppID != "" && appID != expectedAppID {
			return nil, time.Time{}, ErrInvalidAppID
		}
	}

	issuedAt, err := parseTime(payload, "issued_at")
	if err != nil {
		return nil, time.Time{}, ErrInvalidStatement
	}
	expiresAt, err := parseTime(payload, "expires_at")
	if err != nil {
		return nil, time.Time{}, ErrInvalidStatement
	}
	now := time.Now().UTC()
	if expiresAt.Before(now) {
		return nil, time.Time{}, ErrExpiredStatement
	}
	if issuedAt.After(now.Add(v.cfg.MaxSkew)) || issuedAt.Before(now.Add(-24*time.Hour)) {
		return nil, time.Time{}, ErrInvalidStatement
	}
	if !trustedVerdict(payload) {
		return nil, time.Time{}, ErrUntrustedVerdict
	}
	if strings.TrimSpace(statement.AttestationType) != "" {
		payload["attestation_type"] = strings.TrimSpace(statement.AttestationType)
	}
	return payload, expiresAt.UTC(), nil
}

func ComputePublicKeyHash(publicKey string) string {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return ""
	}
	raw, err := decodeBase64(publicKey)
	if err != nil {
		raw = []byte(publicKey)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (v *Verifier) expectedAppID(platform string) string {
	switch strings.ToUpper(strings.TrimSpace(platform)) {
	case "ANDROID":
		return strings.TrimSpace(v.cfg.AndroidAppID)
	case "IOS":
		return strings.TrimSpace(v.cfg.IOSAppID)
	case "WEB":
		return strings.TrimSpace(v.cfg.WebAppID)
	default:
		return ""
	}
}

func trustedVerdict(payload map[string]any) bool {
	if truthy(payload["verified"]) {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(stringValue(payload, "verdict"))) {
	case "ALLOW", "VERIFIED", "TRUSTED", "STRONG":
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(stringValue(payload, "integrity"))) {
	case "ALLOW", "VERIFIED", "TRUSTED", "STRONG":
		return true
	}
	if truthy(payload["cts_profile_match"]) || truthy(payload["basic_integrity"]) || truthy(payload["app_attest_valid"]) {
		return true
	}
	return false
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		parsed := strings.ToLower(strings.TrimSpace(t))
		return parsed == "true" || parsed == "1" || parsed == "yes"
	case float64:
		return t != 0
	default:
		return false
	}
}

func stringValue(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}
	return ""
}

func parseTime(payload map[string]any, key string) (time.Time, error) {
	value := strings.TrimSpace(stringValue(payload, key))
	return time.Parse(time.RFC3339Nano, value)
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(value)
}
