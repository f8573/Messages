package devices

import (
	"encoding/json"
	"net/http"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"time"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/config"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
	"ohmf/services/gateway/internal/sqlutil"
)

type Handler struct {
	svc *Service
}

// removed: trivial constructor wrapper
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var d Device
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	id, err := h.RegisterDevice(r.Context(), userID, d)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "register_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"device_id": id})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	deviceID := chi.URLParam(r, "id")
	if deviceID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing device id", nil)
		return
	}
	var d Device
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	device, err := h.UpdateDevice(r.Context(), userID, deviceID, d)
	if err != nil {
		status := http.StatusInternalServerError
		code := "update_failed"
		if err == ErrDeviceNotFound {
			status = http.StatusNotFound
			code = "device_not_found"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, device)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	ds, err := h.ListDevices(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"devices": ds})
}

func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "missing device id", nil)
		return
	}
	if err := h.RevokeDevice(r.Context(), userID, id); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "revoke_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var ErrDeviceNotFound = errors.New("device_not_found")

type Device struct {
	ID                  string    `json:"device_id"`
	UserID              string    `json:"user_id"`
	Platform            string    `json:"platform"`
	DeviceName          string    `json:"device_name"`
	ClientVersion       string    `json:"client_version"`
	Capabilities        []string  `json:"capabilities"`
	SMSRoleState        string    `json:"sms_role_state"`
	PushToken           string    `json:"push_token"`
	PushProvider        string    `json:"push_provider,omitempty"`
	PushSubscription    string    `json:"push_subscription,omitempty"`
	HasPushSubscription bool      `json:"has_push_subscription,omitempty"`
	PublicKey           string    `json:"public_key"`
	LastSeenAt          time.Time `json:"last_seen_at"`
}


}

func (h *Handler) RegisterDevice(ctx context.Context, userID string, d Device) (string, error) {
	d.Capabilities = normalizeCapabilities(d.Platform, d.Capabilities)
	encryptedSubscription, err := h.encryptSubscription(d.PushSubscription)
	if err != nil {
		return "", err
	}
	var id string
	err = h.db.QueryRow(ctx, `
		INSERT INTO devices (
			user_id,
			platform,
			device_name,
			client_version,
			capabilities,
			sms_role_state,
			push_token,
			push_provider,
			push_subscription,
			push_subscription_updated_at,
			public_key,
			last_seen_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,CASE WHEN NULLIF($9, '') IS NULL THEN NULL ELSE now() END,$10,now())
		RETURNING id::text
	`, userID, d.Platform, d.DeviceName, d.ClientVersion, d.Capabilities, d.SMSRoleState, sqlutil.Nullable(d.PushToken), sqlutil.Nullable(d.PushProvider), sqlutil.Nullable(encryptedSubscription), sqlutil.Nullable(d.PublicKey)).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (h *Handler) UpdateDevice(ctx context.Context, userID, deviceID string, d Device) (Device, error) {
	encryptedSubscription, err := h.encryptSubscription(d.PushSubscription)
	if err != nil {
		return Device{}, err
	}
	tag, err := h.db.Exec(ctx, `
		UPDATE devices
		SET device_name = COALESCE(NULLIF($3, ''), device_name),
		    client_version = COALESCE(NULLIF($4, ''), client_version),
		    capabilities = CASE WHEN $5::bool THEN $6 ELSE capabilities END,
		    push_token = CASE WHEN $7::bool THEN NULLIF($8, '') ELSE push_token END,
		    push_provider = CASE WHEN $9::bool THEN NULLIF($10, '') ELSE push_provider END,
		    push_subscription = CASE WHEN $11::bool THEN NULLIF($12, '') ELSE push_subscription END,
		    push_subscription_updated_at = CASE WHEN $11::bool THEN now() ELSE push_subscription_updated_at END,
		    public_key = CASE WHEN $13::bool THEN NULLIF($14, '') ELSE public_key END,
		    last_seen_at = now(),
		    updated_at = now()
		WHERE id = $1::uuid AND user_id = $2::uuid
	`, deviceID, userID,
		d.DeviceName,
		d.ClientVersion,
		len(d.Capabilities) > 0, normalizeCapabilities(d.Platform, d.Capabilities),
		d.PushToken != "", d.PushToken,
		d.PushProvider != "", d.PushProvider,
		d.PushSubscription != "", encryptedSubscription,
		d.PublicKey != "", d.PublicKey,
	)
	if err != nil {
		return Device{}, err
	}
	if tag.RowsAffected() == 0 {
		return Device{}, ErrDeviceNotFound
	}
	return h.GetDevice(ctx, userID, deviceID)
}

func (h *Handler) GetDevice(ctx context.Context, userID, deviceID string) (Device, error) {
	var d Device
	var caps []string
	var encryptedSubscription string
	err := h.db.QueryRow(ctx, `
		SELECT
			id::text,
			user_id::text,
			platform,
			COALESCE(device_name, ''),
			COALESCE(client_version, ''),
			COALESCE(capabilities, ARRAY[]::text[]),
			COALESCE(sms_role_state, ''),
			COALESCE(push_token, ''),
			COALESCE(push_provider, ''),
			COALESCE(push_subscription, ''),
			COALESCE(public_key, ''),
			COALESCE(last_seen_at, now())
		FROM devices
		WHERE user_id = $1 AND id = $2::uuid
	`, userID, deviceID).Scan(&d.ID, &d.UserID, &d.Platform, &d.DeviceName, &d.ClientVersion, &caps, &d.SMSRoleState, &d.PushToken, &d.PushProvider, &encryptedSubscription, &d.PublicKey, &d.LastSeenAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Device{}, ErrDeviceNotFound
		}
		return Device{}, err
	}
	d.Capabilities = caps
	d.HasPushSubscription = encryptedSubscription != ""
	return d, nil
}

func (h *Handler) ListDevices(ctx context.Context, userID string) ([]Device, error) {
	rows, err := h.db.Query(ctx, `
		SELECT
			id::text,
			user_id::text,
			platform,
			COALESCE(device_name, ''),
			COALESCE(client_version, ''),
			COALESCE(capabilities, ARRAY[]::text[]),
			COALESCE(sms_role_state, ''),
			COALESCE(push_token, ''),
			COALESCE(push_provider, ''),
			COALESCE(push_subscription, ''),
			COALESCE(public_key, ''),
			COALESCE(last_seen_at, now())
		FROM devices
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var caps []string
		var encryptedSubscription string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Platform, &d.DeviceName, &d.ClientVersion, &caps, &d.SMSRoleState, &d.PushToken, &d.PushProvider, &encryptedSubscription, &d.PublicKey, &d.LastSeenAt); err != nil {
			return nil, err
		}
		d.Capabilities = caps
		d.HasPushSubscription = encryptedSubscription != ""
		out = append(out, d)
	}
	return out, nil
}

func (h *Handler) RevokeDevice(ctx context.Context, userID, deviceID string) error {
	_, err := h.db.Exec(ctx, `DELETE FROM devices WHERE user_id = $1 AND id = $2::uuid`, userID, deviceID)
	return err
}

func (h *Handler) ListWebPushSubscriptions(ctx context.Context, userID string) ([]string, error) {
	rows, err := h.db.Query(ctx, `
		SELECT COALESCE(push_subscription, '')
		FROM devices
		WHERE user_id = $1::uuid
		  AND UPPER(COALESCE(push_provider, '')) = 'WEBPUSH'
		  AND NULLIF(push_subscription, '') IS NOT NULL
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 2)
	for rows.Next() {
		var encrypted string
		if err := rows.Scan(&encrypted); err != nil {
			return nil, err
		}
		decrypted, err := h.decryptSubscription(encrypted)
		if err != nil || decrypted == "" {
			continue
		}
		out = append(out, decrypted)
	}
	return out, rows.Err()
}

// removed: duplicate nullable() helper - moved to sqlutil package
}

func normalizeCapabilities(platform string, requested []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(requested)+1)
	for _, capability := range requested {
		capability = strings.TrimSpace(strings.ToUpper(capability))
		if capability == "" {
			continue
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	if strings.EqualFold(platform, "WEB") {
		if _, exists := seen["MINI_APPS"]; !exists {
			out = append(out, "MINI_APPS")
		}
		if _, exists := seen["WEB_PUSH_V1"]; !exists {
			out = append(out, "WEB_PUSH_V1")
		}
	}
	return out
}

// removed: deriveSubscriptionKey() - never called, dead code
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	block, err := aes.NewCipher(h.subscriptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(raw), nil)
	payload := append(nonce, ciphertext...)
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

func (h *Handler) decryptSubscription(encrypted string) (string, error) {
	if strings.TrimSpace(encrypted) == "" {
		return "", nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(h.subscriptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid_push_subscription")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
