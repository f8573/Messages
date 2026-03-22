package e2ee

import (
	"database/sql"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/httpx"
)

// Handler handles E2EE HTTP endpoints
type Handler struct {
	pool *pgxpool.Pool
	sm   *SessionManager
}

// NewHandler creates a new E2EE handler
func NewHandler(db *sql.DB, pgxPool *pgxpool.Pool) *Handler {
	return &Handler{
		pool: pgxPool,
		sm:   NewSessionManager(db),
	}
}

// DeviceKeyResponse represents a device key in API response
type DeviceKeyResponse struct {
	DeviceID            string `json:"device_id"`
	UserID              string `json:"user_id"`
	BundleVersion       string `json:"bundle_version"`
	IdentityKeyAlg      string `json:"identity_key_alg"`
	IdentityPublicKey   string `json:"identity_public_key"`
	SigningKeyAlg       string `json:"signing_key_alg"`
	SigningPublicKey    string `json:"signing_public_key"`
	Fingerprint         string `json:"fingerprint"`
	PublishedAt         *string `json:"published_at,omitempty"`
}

// BundleResponse represents a full Signal protocol key bundle
type BundleResponse struct {
	DeviceID                   string `json:"device_id"`
	UserID                     string `json:"user_id"`
	BundleVersion              string `json:"bundle_version"`
	IdentityKeyAlg             string `json:"identity_key_alg"`
	IdentityPublicKey          string `json:"identity_public_key"`
	AgreementIdentityPublicKey string `json:"agreement_identity_public_key"`
	SigningKeyAlg              string `json:"signing_key_alg"`
	SigningPublicKey           string `json:"signing_public_key"`
	SignedPrekeyID             int64 `json:"signed_prekey_id"`
	SignedPrekeyPublicKey      string `json:"signed_prekey_public_key"`
	SignedPrekeySignature      string `json:"signed_prekey_signature"`
	Fingerprint                string `json:"fingerprint"`
	ClaimedOneTimePrekey       *struct {
		PrekeyID  int64 `json:"prekey_id"`
		PublicKey string `json:"public_key"`
	} `json:"claimed_one_time_prekey,omitempty"`
}

// ClaimOTPResponse represents the response when claiming an OTP
type ClaimOTPResponse struct {
	PrekeyID  int64 `json:"prekey_id"`
	PublicKey string `json:"public_key"`
}

// VerifyFingerprintRequest represents the request to verify a device fingerprint
type VerifyFingerprintRequest struct {
	ContactUserID    string `json:"contact_user_id"`
	ContactDeviceID  string `json:"contact_device_id"`
	Fingerprint      string `json:"fingerprint"`
}

// VerifyFingerprintResponse represents the response from fingerprint verification
type VerifyFingerprintResponse struct {
	Verified   bool   `json:"verified"`
	TrustState string `json:"trust_state"`
	Message    string `json:"message,omitempty"`
}

// ListDeviceKeys handles GET /v1/device-keys/{user_id}
// Returns the public keys for all devices of a user
// TODO: Implement with proper pgxpool integration
func (h *Handler) ListDeviceKeys(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusNotImplemented, "not_implemented", "E2EE endpoints pending integration", nil)
}

// GetDeviceKeyBundle handles GET /v1/device-keys/{user_id}/{device_id}/bundle
// Returns the full key bundle for X3DH key exchange
// TODO: Implement with proper pgxpool integration
func (h *Handler) GetDeviceKeyBundle(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusNotImplemented, "not_implemented", "E2EE endpoints pending integration", nil)
}

// ClaimOneTimePrekey handles POST /v1/device-keys/{device_id}/claim-otp
// Atomically claims the next available one-time prekey
// TODO: Implement with proper pgxpool integration
func (h *Handler) ClaimOneTimePrekey(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusNotImplemented, "not_implemented", "E2EE endpoints pending integration", nil)
}

// VerifyDeviceFingerprint handles POST /v1/e2ee/session/verify
// Verifies and records TOFU trust for device fingerprints
// TODO: Implement with proper pgxpool integration
func (h *Handler) VerifyDeviceFingerprint(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusNotImplemented, "not_implemented", "E2EE endpoints pending integration", nil)
}

// GetTrustState handles GET /v1/e2ee/session/trust-state
// Returns the current trust state for a device
// TODO: Implement with proper pgxpool integration
func (h *Handler) GetTrustState(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusNotImplemented, "not_implemented", "E2EE endpoints pending integration", nil)
}

// HandleError is a helper for standard error responses
func (h *Handler) HandleError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode string, message string, err error) {
	details := map[string]any{"error": err.Error()}
	if err == nil {
		details = nil
	}
	httpx.WriteError(w, r, statusCode, errorCode, message, details)
}
