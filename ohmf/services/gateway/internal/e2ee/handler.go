package e2ee

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

// Handler handles E2EE HTTP endpoints
type Handler struct {
	pool *pgxpool.Pool
	sm   *SessionManager
}

// NewHandler creates a new E2EE handler
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{
		pool: pool,
		sm:   NewSessionManager(pool),
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

// ListDeviceKeys handles GET /v1/e2ee/keys
// Returns the public keys for all devices of the authenticated user
func (h *Handler) ListDeviceKeys(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "user context required", nil)
		return
	}

	query := `
		SELECT device_id, user_id, key_version, identity_key_alg, identity_public_key,
		       signing_key_alg, signing_public_key, published_at
		FROM device_identity_keys
		WHERE user_id = $1::uuid
		ORDER BY published_at DESC
	`

	rows, err := h.pool.Query(r.Context(), query, userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to query device keys", nil)
		return
	}
	defer rows.Close()

	var keys []DeviceKeyResponse
	for rows.Next() {
		var deviceID, userID, identityKeyAlg, identityPublicKey, signingKeyAlg, signingPublicKey string
		var keyVersion int
		var publishedAt *time.Time

		if err := rows.Scan(&deviceID, &userID, &keyVersion, &identityKeyAlg, &identityPublicKey,
			&signingKeyAlg, &signingPublicKey, &publishedAt); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to scan device key", nil)
			return
		}

		// Compute fingerprint
		fingerprint, err := ComputeFingerprint(signingPublicKey)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "crypto_error", "failed to compute fingerprint", nil)
			return
		}

		var publishedAtStr *string
		if publishedAt != nil {
			s := publishedAt.String()
			publishedAtStr = &s
		}

		keys = append(keys, DeviceKeyResponse{
			DeviceID:          deviceID,
			UserID:            userID,
			BundleVersion:     "OHMF_SIGNAL_V1",
			IdentityKeyAlg:    identityKeyAlg,
			IdentityPublicKey: identityPublicKey,
			SigningKeyAlg:     signingKeyAlg,
			SigningPublicKey:  signingPublicKey,
			Fingerprint:       fingerprint,
			PublishedAt:       publishedAtStr,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, keys)
}

// GetDeviceKeyBundle handles GET /v1/e2ee/keys/{user_id}/{device_id}
// Returns the full key bundle for X3DH key exchange including a claimed one-time prekey
func (h *Handler) GetDeviceKeyBundle(w http.ResponseWriter, r *http.Request) {
	targetUserID := chi.URLParam(r, "user_id")
	targetDeviceID := chi.URLParam(r, "device_id")

	if targetUserID == "" {
		targetUserID = chi.URLParam(r, "userID") // Support alternate naming convention
	}
	if targetDeviceID == "" {
		targetDeviceID = chi.URLParam(r, "deviceID") // Support alternate naming convention
	}

	if targetUserID == "" || targetDeviceID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "missing_param", "user_id and device_id path parameters required", nil)
		return
	}

	// Query device bundle for specific device
	query := `
		SELECT device_id, user_id, signed_prekey_id, identity_key_alg, identity_public_key,
		       signed_prekey_public_key, signed_prekey_signature, signing_key_alg, signing_public_key
		FROM device_identity_keys
		WHERE user_id = $1::uuid AND device_id = $2::uuid
		LIMIT 1
	`

	var deviceID, userID, identityKeyAlg, identityPublicKey, signedPrekeyPublicKey, signedPrekeySignature, signingKeyAlg, signingPublicKey string
	var signedPrekeyID int64

	err := h.pool.QueryRow(r.Context(), query, targetUserID, targetDeviceID).Scan(
		&deviceID, &userID, &signedPrekeyID, &identityKeyAlg, &identityPublicKey,
		&signedPrekeyPublicKey, &signedPrekeySignature, &signingKeyAlg, &signingPublicKey,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", "device key bundle not found", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to query device bundle", nil)
		return
	}

	// Compute fingerprint
	fingerprint, err := ComputeFingerprint(signingPublicKey)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "crypto_error", "failed to compute fingerprint", nil)
		return
	}

	// Try to claim an available one-time prekey
	otpQuery := `
		UPDATE device_one_time_prekeys
		SET consumed_at = NOW()
		WHERE device_id = $1::uuid AND consumed_at IS NULL
		RETURNING prekey_id, public_key
		LIMIT 1
	`

	var otpPreKeyID int64
	var otpPublicKey string
	var claimedOTP *struct {
		PrekeyID  int64  `json:"prekey_id"`
		PublicKey string `json:"public_key"`
	}

	err = h.pool.QueryRow(r.Context(), otpQuery, deviceID).Scan(&otpPreKeyID, &otpPublicKey)
	if err == nil {
		claimedOTP = &struct {
			PrekeyID  int64  `json:"prekey_id"`
			PublicKey string `json:"public_key"`
		}{
			PrekeyID:  otpPreKeyID,
			PublicKey: otpPublicKey,
		}
	} else if err != pgx.ErrNoRows {
		httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to claim prekey", nil)
		return
	}
	// If no rows, claimedOTP stays nil (pool exhausted is acceptable)

	bundle := BundleResponse{
		DeviceID:                   deviceID,
		UserID:                     userID,
		BundleVersion:              "OHMF_SIGNAL_V1",
		IdentityKeyAlg:             identityKeyAlg,
		IdentityPublicKey:          identityPublicKey,
		AgreementIdentityPublicKey: identityPublicKey, // For X3DH
		SigningKeyAlg:              signingKeyAlg,
		SigningPublicKey:           signingPublicKey,
		SignedPrekeyID:             signedPrekeyID,
		SignedPrekeyPublicKey:      signedPrekeyPublicKey,
		SignedPrekeySignature:      signedPrekeySignature,
		Fingerprint:                fingerprint,
		ClaimedOneTimePrekey:       claimedOTP,
	}

	httpx.WriteJSON(w, http.StatusOK, bundle)
}

// ClaimOneTimePrekey handles POST /v1/e2ee/claim-prekey
// Atomically claims the next available one-time prekey for a device
func (h *Handler) ClaimOneTimePrekey(w http.ResponseWriter, r *http.Request) {
	// Verify user is authenticated
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "user context required", nil)
		return
	}

	// Parse request body to get target device
	var req struct {
		UserID   string `json:"user_id"`
		DeviceID string `json:"device_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_json", "failed to parse request body", nil)
		return
	}

	if req.UserID == "" || req.DeviceID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "missing_field", "user_id and device_id required", nil)
		return
	}

	// Verify device exists and belongs to target user
	verifyQuery := `
		SELECT user_id FROM devices WHERE id = $1::uuid
	`
	var deviceUserID string
	err := h.pool.QueryRow(r.Context(), verifyQuery, req.DeviceID).Scan(&deviceUserID)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", "device not found", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to verify device", nil)
		return
	}

	if deviceUserID != req.UserID {
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "device does not belong to specified user", nil)
		return
	}

	// Atomically claim next available prekey
	claimQuery := `
		UPDATE device_one_time_prekeys
		SET consumed_at = NOW()
		WHERE device_id = $1::uuid AND consumed_at IS NULL
		RETURNING prekey_id, public_key
		LIMIT 1
	`

	var prekeyID int64
	var publicKey string

	err = h.pool.QueryRow(r.Context(), claimQuery, req.DeviceID).Scan(&prekeyID, &publicKey)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.WriteError(w, r, http.StatusNotFound, "prekey_exhausted", "no available one-time prekeys for device", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to claim prekey", nil)
		return
	}

	response := ClaimOTPResponse{
		PrekeyID:  prekeyID,
		PublicKey: publicKey,
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

// VerifyDeviceFingerprint handles POST /v1/e2ee/verify
// Verifies and records TOFU (Trust on First Use) trust for device fingerprints
func (h *Handler) VerifyDeviceFingerprint(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "user context required", nil)
		return
	}

	var req VerifyFingerprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_json", "failed to parse request body", nil)
		return
	}

	if req.ContactUserID == "" || req.ContactDeviceID == "" || req.Fingerprint == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "missing_field", "contact_user_id, contact_device_id, and fingerprint required", nil)
		return
	}

	// Get stored fingerprint for this device if it exists
	queryExisting := `
		SELECT trust_state, fingerprint FROM device_key_trust
		WHERE user_id = $1::uuid AND contact_user_id = $2::uuid AND contact_device_id = $3::uuid
	`

	var existingTrustState, storedFingerprint string
	err := h.pool.QueryRow(r.Context(), queryExisting, userID, req.ContactUserID, req.ContactDeviceID).Scan(&existingTrustState, &storedFingerprint)

	response := VerifyFingerprintResponse{
		Verified:   false,
		TrustState: "UNKNOWN",
	}

	if err == nil {
		// Trust record exists
		response.TrustState = existingTrustState

		if storedFingerprint == req.Fingerprint {
			response.Verified = true

			// If TOFU, upgrade to VERIFIED
			if existingTrustState == "TOFU" {
				updateQuery := `
					UPDATE device_key_trust
					SET trust_state = 'VERIFIED', verified_at = NOW()
					WHERE user_id = $1::uuid AND contact_user_id = $2::uuid AND contact_device_id = $3::uuid
				`
				if _, err := h.pool.Exec(r.Context(), updateQuery, userID, req.ContactUserID, req.ContactDeviceID); err != nil {
					httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to update trust state", nil)
					return
				}
				response.TrustState = "VERIFIED"
			}

			response.Message = "fingerprint verified and matches stored value"
		} else {
			response.Message = "fingerprint does not match stored value - possible MITM!"
		}
	} else if err == pgx.ErrNoRows {
		// First contact - establish TOFU trust
		response.Verified = true
		response.TrustState = "TOFU"
		response.Message = "first contact - TOFU trust established"

		// Get device bundle to get the actual signing public key
		bundleQuery := `
			SELECT signing_public_key FROM device_identity_keys
			WHERE user_id = $1::uuid AND device_id = $2::uuid
		`
		var signingPublicKey string
		err := h.pool.QueryRow(r.Context(), bundleQuery, req.ContactUserID, req.ContactDeviceID).Scan(&signingPublicKey)
		if err != nil {
			if err == pgx.ErrNoRows {
				httpx.WriteError(w, r, http.StatusNotFound, "not_found", "contact device not found", nil)
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to query device bundle", nil)
			return
		}

		// Insert trust record
		insertQuery := `
			INSERT INTO device_key_trust (user_id, contact_user_id, contact_device_id, trust_state, fingerprint, trusted_device_public_key, trust_established_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'TOFU', $4, $5, NOW())
			ON CONFLICT DO NOTHING
		`
		if _, err := h.pool.Exec(r.Context(), insertQuery, userID, req.ContactUserID, req.ContactDeviceID, req.Fingerprint, signingPublicKey); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to store trust state", nil)
			return
		}
	} else {
		httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to query trust state", nil)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

// GetTrustState handles GET /v1/e2ee/trust/{contact_user_id}/{device_id}
// Returns the current trust state for a device
func (h *Handler) GetTrustState(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "user context required", nil)
		return
	}

	contactUserID := chi.URLParam(r, "contact_user_id")
	contactDeviceID := chi.URLParam(r, "device_id")

	if contactUserID == "" || contactDeviceID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "missing_param", "contact_user_id and device_id path parameters required", nil)
		return
	}

	query := `
		SELECT trust_state, fingerprint, trust_established_at, verified_at
		FROM device_key_trust
		WHERE user_id = $1::uuid AND contact_user_id = $2::uuid AND contact_device_id = $3::uuid
	`

	var trustState, fingerprint string
	var trustEstablishedAt time.Time
	var verifiedAt *time.Time

	err := h.pool.QueryRow(r.Context(), query, userID, contactUserID, contactDeviceID).Scan(
		&trustState, &fingerprint, &trustEstablishedAt, &verifiedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			// No trust record - first encounter would establish TOFU
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", "no trust state recorded for this device", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "database_error", "failed to query trust state", nil)
		return
	}

	response := map[string]interface{}{
		"trust_state":           trustState,
		"fingerprint":           fingerprint,
		"trust_established_at": trustEstablishedAt,
		"verified_at":          verifiedAt,
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}
