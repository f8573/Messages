package e2ee

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

// Handler handles E2EE HTTP endpoints
type Handler struct {
	db *sql.DB
	sm *SessionManager
}

// NewHandler creates a new E2EE handler
func NewHandler(db *sql.DB, pgxPool *pgxpool.Pool) *Handler {
	return &Handler{
		db: db,
		sm: NewSessionManager(db),
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
func (h *Handler) ListDeviceKeys(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")

	query := `
		SELECT d.id, d.user_id, dik.bundle_version, dik.identity_key_alg,
		       dik.identity_public_key, dik.signing_key_alg, dik.signing_public_key,
		       dik.fingerprint, dik.published_at
		FROM device_identity_keys dik
		JOIN devices d ON dik.device_id = d.id
		WHERE d.user_id = $1
	`

	rows, err := h.db.QueryContext(r.Context(), query, userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "query_failed", err.Error(), nil)
		return
	}
	defer rows.Close()

	var keys []DeviceKeyResponse
	for rows.Next() {
		var key DeviceKeyResponse
		var publishedAt *string
		if err := rows.Scan(
			&key.DeviceID, &key.UserID, &key.BundleVersion, &key.IdentityKeyAlg,
			&key.IdentityPublicKey, &key.SigningKeyAlg, &key.SigningPublicKey,
			&key.Fingerprint, &publishedAt,
		); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "scan_failed", err.Error(), nil)
			return
		}
		key.PublishedAt = publishedAt
		keys = append(keys, key)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": keys,
	})
}

// GetDeviceKeyBundle handles GET /v1/device-keys/{user_id}/{device_id}/bundle
// Returns the full key bundle for X3DH key exchange
func (h *Handler) GetDeviceKeyBundle(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	deviceID := chi.URLParam(r, "deviceID")

	query := `
		SELECT device_id, user_id, bundle_version, identity_key_alg,
		       identity_public_key, agreement_identity_public_key, signing_key_alg,
		       signing_public_key, signed_prekey_id, signed_prekey_public_key,
		       signed_prekey_signature, fingerprint
		FROM device_identity_keys
		WHERE device_id = $1 AND user_id = $2
	`

	var bundle BundleResponse
	err := h.db.QueryRowContext(r.Context(), query, deviceID, userID).Scan(
		&bundle.DeviceID, &bundle.UserID, &bundle.BundleVersion, &bundle.IdentityKeyAlg,
		&bundle.IdentityPublicKey, &bundle.AgreementIdentityPublicKey, &bundle.SigningKeyAlg,
		&bundle.SigningPublicKey, &bundle.SignedPrekeyID, &bundle.SignedPrekeyPublicKey,
		&bundle.SignedPrekeySignature, &bundle.Fingerprint,
	)

	if err == sql.ErrNoRows {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "device key bundle not found", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "query_failed", err.Error(), nil)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, bundle)
}

// ClaimOneTimePrekey handles POST /v1/device-keys/{device_id}/claim-otp
// Atomically claims the next available one-time prekey
func (h *Handler) ClaimOneTimePrekey(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}

	deviceID := chi.URLParam(r, "deviceID")

	// Verify the device belongs to the actor
	var deviceUserID string
	err := h.db.QueryRowContext(r.Context(), `
		SELECT user_id FROM devices WHERE id = $1
	`, deviceID).Scan(&deviceUserID)

	if err == sql.ErrNoRows {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "device not found", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "query_failed", err.Error(), nil)
		return
	}

	if deviceUserID != actor {
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "device not owned by user", nil)
		return
	}

	// Claim the prekey
	query := `
		UPDATE device_one_time_prekeys
		SET consumed_at = NOW()
		WHERE device_id = $1 AND consumed_at IS NULL
		ORDER BY prekey_id ASC
		LIMIT 1
		RETURNING prekey_id, public_key
	`

	var resp ClaimOTPResponse
	err = h.db.QueryRowContext(r.Context(), query, deviceID).Scan(&resp.PrekeyID, &resp.PublicKey)

	if err == sql.ErrNoRows {
		httpx.WriteError(w, r, http.StatusConflict, "no_prekeys_available", "no available one-time prekeys", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "claim_failed", err.Error(), nil)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

// VerifyDeviceFingerprint handles POST /v1/e2ee/session/verify
// Verifies and records TOFU trust for device fingerprints
func (h *Handler) VerifyDeviceFingerprint(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}

	var req VerifyFingerprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body", nil)
		return
	}

	// Validate input
	if req.ContactUserID == "" || req.ContactDeviceID == "" || req.Fingerprint == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing required fields", nil)
		return
	}

	// Retrieve the contact's device key to verify fingerprint
	query := `
		SELECT signing_public_key
		FROM device_identity_keys
		WHERE device_id = $1 AND user_id = $2
	`

	var pubKeyBase64 string
	err := h.db.QueryRowContext(r.Context(), query, req.ContactDeviceID, req.ContactUserID).Scan(&pubKeyBase64)

	if err == sql.ErrNoRows {
		httpx.WriteError(w, r, http.StatusNotFound, "device_not_found", "contact device not found", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "query_failed", err.Error(), nil)
		return
	}

	// Compute fingerprint from stored key
	computedFingerprint, err := ComputeFingerprint(pubKeyBase64)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "fingerprint_failed", err.Error(), nil)
		return
	}

	// Verify fingerprint matches
	if computedFingerprint != req.Fingerprint {
		httpx.WriteError(w, r, http.StatusBadRequest, "fingerprint_mismatch", "provided fingerprint doesn't match device key", nil)
		return
	}

	// Store TOFU trust
	err = h.sm.EstablishTOFUTrust(r.Context(), actor, req.ContactUserID, req.ContactDeviceID,
		req.Fingerprint, pubKeyBase64)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "trust_failed", err.Error(), nil)
		return
	}

	// Return success
	httpx.WriteJSON(w, http.StatusOK, VerifyFingerprintResponse{
		Verified:   true,
		TrustState: "TOFU",
		Message:    "Device fingerprint verified and trusted",
	})
}

// GetTrustState handles GET /v1/e2ee/session/trust-state
// Returns the current trust state for a device
func (h *Handler) GetTrustState(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}

	contactUserID := r.URL.Query().Get("contact_user_id")
	contactDeviceID := r.URL.Query().Get("contact_device_id")

	if contactUserID == "" || contactDeviceID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing query parameters", nil)
		return
	}

	trust, err := h.sm.GetTrustState(r.Context(), actor, contactUserID, contactDeviceID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "query_failed", err.Error(), nil)
		return
	}

	if trust == nil {
		// No trust state recorded yet
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"trust_state": "UNKNOWN",
			"verified":    false,
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"trust_state":              trust.State,
		"fingerprint":              trust.Fingerprint,
		"trust_established_at":     trust.TrustEstablishedAt,
		"verified_at":              trust.VerifiedAt,
		"verified":                 trust.State == "VERIFIED",
		"blocked":                  trust.State == "BLOCKED",
	})
}

// HandleError is a helper for standard error responses
func (h *Handler) HandleError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode string, message string, err error) {
	details := map[string]any{"error": err.Error()}
	if err == nil {
		details = nil
	}
	httpx.WriteError(w, r, statusCode, errorCode, message, details)
}
