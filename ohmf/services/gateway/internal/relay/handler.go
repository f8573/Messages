package relay

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
	"strconv"
	"strings"
	"time"
)

func NewHandler(db DB, requireAttestation bool) *Handler {
	return &Handler{db: db, requireAttestation: requireAttestation}
}
func NewHandlerWithDB(db DB) *Handler { return NewHandler(db, false) }

type Handler struct {
	db                 DB
	requireAttestation bool
}

type relayJobRecord struct {
	RelayJob
	destinationRaw []byte
	contentRaw     []byte
	resultRaw      []byte
	createdAt      *time.Time
	updatedAt      *time.Time
	expiresAt      *time.Time
	acceptedAt     *time.Time
	attestedAt     *time.Time
}

type relayDeviceRecord struct {
	publicKey            string
	capabilities         []string
	smsRoleState         string
	lastSeenAt           time.Time
	attestationState     string
	attestationExpiresAt sql.NullTime
}

// removed: trivial constructor wrapper - now uses NewHandlerWithOptions
func (h *Handler) CreateMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		Destination   any    `json:"destination"`
		TransportHint string `json:"transport_hint"`
		Content       any    `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.Destination == nil || req.Content == nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "destination and content required", nil)
		return
	}
	transportHint, requiredCapability := h.canonicalRelayPolicy(req.TransportHint, req.Content)
	id, err := h.CreateJob(r.Context(), userID, req.Destination, transportHint, req.Content, requiredCapability)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "create_failed", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"relay_job_id": id})
}

func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	actorUserID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	job, err := h.GetJobForActor(r.Context(), actorUserID, id)
	if err != nil {
		status := http.StatusNotFound
		code := "not_found"
		if errors.Is(err, ErrRelayUnauthorized) {
			status = http.StatusForbidden
			code = "forbidden"
		} else if errors.Is(err, ErrRelayExpired) {
			status = http.StatusGone
			code = "expired"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func (h *Handler) ListAvailable(w http.ResponseWriter, r *http.Request) {
	// device-authenticated agents poll for available jobs
	actorUserID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	// optional limit query param
	q := r.URL.Query().Get("limit")
	limit := 10
	if q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	jobs, err := h.ListQueuedJobsForActor(r.Context(), actorUserID, limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	actorUserID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	job, err := h.loadJobRecordForActor(r.Context(), actorUserID, id)
	if err != nil {
		status := http.StatusNotFound
		code := "not_found"
		if errors.Is(err, ErrRelayUnauthorized) {
			status = http.StatusForbidden
			code = "forbidden"
		} else if errors.Is(err, ErrRelayExpired) {
			status = http.StatusGone
			code = "expired"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	sig := r.Header.Get("X-Device-Signature")
	ts := r.Header.Get("X-Device-Timestamp")
	device, err := h.loadDeviceRecord(r.Context(), actorUserID, req.DeviceID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
		return
	}
	attested := device.isAttested()
	if sig == "" && ts == "" {
		if h.requireAttestation || device.publicKey != "" || job.RequiredCapability == "ANDROID_CARRIER" {
			httpx.WriteError(w, r, http.StatusUnauthorized, "attestation_required", "device attestation required", nil)
			return
		}
	} else {
		if sig == "" || ts == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "incomplete device signature headers", nil)
			return
		}
		tsec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid timestamp", nil)
			return
		}
		now := time.Now().Unix()
		if tsec < now-60 || tsec > now+60 {
			httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "stale timestamp", nil)
			return
		}
		if err := h.verifyAcceptanceSignature(r.Context(), job, req.DeviceID, ts, sig); err != nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "invalid device signature", nil)
			return
		}
	}
	if err := h.AcceptJobForActor(r.Context(), actorUserID, id, req.DeviceID, attested, job.RequiredCapability); err != nil {
		status := http.StatusBadRequest
		code := "accept_failed"
		if errors.Is(err, ErrRelayUnauthorized) {
			status = http.StatusForbidden
			code = "forbidden"
		} else if errors.Is(err, ErrRelayAttestationRequired) {
			status = http.StatusUnauthorized
			code = "attestation_required"
		} else if errors.Is(err, ErrRelayExpired) {
			status = http.StatusGone
			code = "expired"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Result(w http.ResponseWriter, r *http.Request) {
	actorUserID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		DeviceID string `json:"device_id"`
		Status   string `json:"status"`
		Result   any    `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	job, err := h.loadJobRecordForActor(r.Context(), actorUserID, id)
	if err != nil {
		status := http.StatusNotFound
		code := "not_found"
		if errors.Is(err, ErrRelayUnauthorized) {
			status = http.StatusForbidden
			code = "forbidden"
		} else if errors.Is(err, ErrRelayExpired) {
			status = http.StatusGone
			code = "expired"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	if err := h.FinishJobForActor(r.Context(), actorUserID, req.DeviceID, id, req.Result, req.Status, job.RequiredCapability); err != nil {
		status := http.StatusInternalServerError
		code := "result_failed"
		if errors.Is(err, ErrRelayUnauthorized) {
			status = http.StatusForbidden
			code = "forbidden"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type RowScanner interface {
	Scan(dest ...any) error
}

type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
}

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (any, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) RowScanner
}

type pgxPoolAdapter struct {
	p *pgxpool.Pool
}

func (a *pgxPoolAdapter) QueryRow(ctx context.Context, sql string, args ...any) RowScanner {
	return a.p.QueryRow(ctx, sql, args...)
}

func (a *pgxPoolAdapter) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	return a.p.Query(ctx, sql, args...)
}

func (a *pgxPoolAdapter) Exec(ctx context.Context, sql string, args ...any) (any, error) {
	return a.p.Exec(ctx, sql, args...)
}

type RelayJob struct {
	ID                 string `json:"id"`
	CreatorUserID      string `json:"creator_user_id"`
	Destination        any    `json:"destination"`
	TransportHint      string `json:"transport_hint"`
	Content            any    `json:"content"`
	Status             string `json:"status"`
	ExecutingDeviceID  string `json:"executing_device_id,omitempty"`
	ConsentState       string `json:"consent_state,omitempty"`
	RequiredCapability string `json:"required_capability,omitempty"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	AcceptedAt         string `json:"accepted_at,omitempty"`
	AttestedAt         string `json:"attested_at,omitempty"`
	Result             any    `json:"result,omitempty"`
}

func (h *Handler) CreateJob(ctx context.Context, creatorID string, destination any, transportHint string, content any, requiredCapability string) (string, error) {
	id := uuid.New().String()
	if strings.TrimSpace(requiredCapability) == "" {
		requiredCapability = "RELAY_EXECUTOR"
	}
	destB, _ := json.Marshal(destination)
	contentB, _ := json.Marshal(content)
	_, err := h.db.Exec(ctx, `
		INSERT INTO relay_jobs (
			id,
			creator_user_id,
			destination,
			transport_hint,
			content,
			status,
			consent_state,
			required_capability,
			expires_at,
			created_at
		)
		VALUES ($1::uuid, $2::uuid, $3::jsonb, $4, $5::jsonb, 'queued', 'PENDING_DEVICE', $6, now() + interval '10 minute', now())
	`, id, creatorID, string(destB), transportHint, string(contentB), requiredCapability)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListQueuedJobs returns up to `limit` jobs that are currently queued on the server.
func (h *Handler) ListQueuedJobs(ctx context.Context, limit int) ([]RelayJob, error) {
	return h.listQueuedJobs(ctx, "", limit)
}

func (h *Handler) ListQueuedJobsForActor(ctx context.Context, actorUserID string, limit int) ([]RelayJob, error) {
	return h.listQueuedJobs(ctx, actorUserID, limit)
}

func (h *Handler) listQueuedJobs(ctx context.Context, actorUserID string, limit int) ([]RelayJob, error) {
	query := `SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, consent_state, required_capability, expires_at, accepted_at, attested_at, result, created_at, updated_at FROM relay_jobs WHERE status = 'queued' AND (expires_at IS NULL OR expires_at > now())`
	args := []any{}
	if strings.TrimSpace(actorUserID) != "" {
		query += ` AND creator_user_id = $1::uuid ORDER BY created_at ASC LIMIT $2`
		args = append(args, actorUserID, limit)
	} else {
		query += ` ORDER BY created_at ASC LIMIT $1`
		args = append(args, limit)
	}
	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RelayJob
	for rows.Next() {
		var id, creator, transport, status, execID, consentState, requiredCapability string
		var destB, contentB, resultB []byte
		var createdAt, updatedAt, expiresAt, acceptedAt, attestedAt sql.NullTime
		if err := rows.Scan(&id, &creator, &destB, &transport, &contentB, &status, &execID, &consentState, &requiredCapability, &expiresAt, &acceptedAt, &attestedAt, &resultB, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var destination any
		var content any
		var result any
		_ = json.Unmarshal(destB, &destination)
		_ = json.Unmarshal(contentB, &content)
		if len(resultB) > 0 {
			_ = json.Unmarshal(resultB, &result)
		}
		job := RelayJob{
			ID:                 id,
			CreatorUserID:      creator,
			Destination:        destination,
			TransportHint:      transport,
			Content:            content,
			Status:             status,
			ExecutingDeviceID:  execID,
			ConsentState:       consentState,
			RequiredCapability: requiredCapability,
			Result:             result,
		}
		if expiresAt.Valid {
			job.ExpiresAt = expiresAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if acceptedAt.Valid {
			job.AcceptedAt = acceptedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if attestedAt.Valid {
			job.AttestedAt = attestedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, job)
	}
	return out, nil
}

// Common failure/status codes for relay jobs.
const (
	StatusQueued                    = "QUEUED_ON_SERVER"
	StatusAccepted                  = "ACCEPTED_BY_DEVICE"
	StatusCompleted                 = "COMPLETED"
	StatusNoEligible                = "NO_ELIGIBLE_ANDROID_DEVICE"
	StatusDeviceOffline             = "DEVICE_OFFLINE"
	StatusSmsRoleNotHeld            = "SMS_ROLE_NOT_HELD"
	StatusSendPermissionUnavailable = "SEND_PERMISSION_UNAVAILABLE"
	StatusCarrierSendFailed         = "CARRIER_SEND_FAILED"
	StatusContentTooLarge           = "CONTENT_TOO_LARGE_FOR_SMS"
	StatusMmsNotConfigured          = "MMS_NOT_CONFIGURED"
	StatusUserDisabled              = "USER_DISABLED_RELAY"
)

var (
	ErrRelayUnauthorized        = errors.New("relay_unauthorized")
	ErrRelayExpired             = errors.New("relay_expired")
	ErrRelayAttestationRequired = errors.New("relay_attestation_required")
)

func (h *Handler) fetchJob(ctx context.Context, id string) (*RelayJob, error) {
	rec, err := h.loadJobRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	return &rec.RelayJob, nil
}

func (h *Handler) loadJobRecord(ctx context.Context, id string) (*relayJobRecord, error) {
	var destB, contentB, resultB []byte
	var transport, status, creator, execID, consentState, requiredCapability string
	var updatedAt, createdAt, expiresAt, acceptedAt, attestedAt sql.NullTime
	err := h.db.QueryRow(ctx, `SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, consent_state, required_capability, expires_at, accepted_at, attested_at, result, created_at, updated_at FROM relay_jobs WHERE id = $1::uuid`, id).Scan(&id, &creator, &destB, &transport, &contentB, &status, &execID, &consentState, &requiredCapability, &expiresAt, &acceptedAt, &attestedAt, &resultB, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	var destination any
	var content any
	var result any
	_ = json.Unmarshal(destB, &destination)
	_ = json.Unmarshal(contentB, &content)
	if len(resultB) > 0 {
		_ = json.Unmarshal(resultB, &result)
	}
	job := RelayJob{
		ID:                 id,
		CreatorUserID:      creator,
		Destination:        destination,
		TransportHint:      transport,
		Content:            content,
		Status:             status,
		ExecutingDeviceID:  execID,
		ConsentState:       consentState,
		RequiredCapability: requiredCapability,
		Result:             result,
	}
	if expiresAt.Valid {
		job.ExpiresAt = expiresAt.Time.UTC().Format(time.RFC3339Nano)
	}
	if acceptedAt.Valid {
		job.AcceptedAt = acceptedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	if attestedAt.Valid {
		job.AttestedAt = attestedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return &relayJobRecord{
		RelayJob:       job,
		destinationRaw: destB,
		contentRaw:     contentB,
		resultRaw:      resultB,
		createdAt:      nullableTimePtr(createdAt),
		updatedAt:      nullableTimePtr(updatedAt),
		expiresAt:      nullableTimePtr(expiresAt),
		acceptedAt:     nullableTimePtr(acceptedAt),
		attestedAt:     nullableTimePtr(attestedAt),
	}, nil
}

func nullableTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func (h *Handler) loadJobRecordForActor(ctx context.Context, actorUserID, id string) (*relayJobRecord, error) {
	rec, err := h.loadJobRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.CreatorUserID != actorUserID {
		return nil, ErrRelayUnauthorized
	}
	if rec.expiresAt != nil && !rec.expiresAt.After(time.Now()) && rec.Status != "completed" {
		return nil, ErrRelayExpired
	}
	return rec, nil
}

func (h *Handler) loadDeviceRecord(ctx context.Context, actorUserID, deviceID string) (*relayDeviceRecord, error) {
	var caps []string
	var publicKey, smsRoleState, attestationState string
	var lastSeenAt time.Time
	var attestationExpiresAt sql.NullTime
	err := h.db.QueryRow(ctx, `
		SELECT COALESCE(public_key, ''),
		       COALESCE(capabilities, ARRAY[]::text[]),
		       COALESCE(sms_role_state, ''),
		       COALESCE(last_seen_at, now()),
		       COALESCE(attestation_state, 'UNVERIFIED'),
		       attestation_expires_at
		FROM devices
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
	`, deviceID, actorUserID).Scan(&publicKey, &caps, &smsRoleState, &lastSeenAt, &attestationState, &attestationExpiresAt)
	if err != nil {
		return nil, err
	}
	return &relayDeviceRecord{
		publicKey:            publicKey,
		capabilities:         caps,
		smsRoleState:         smsRoleState,
		lastSeenAt:           lastSeenAt,
		attestationState:     attestationState,
		attestationExpiresAt: attestationExpiresAt,
	}, nil
}

func (h *Handler) GetJobForActor(ctx context.Context, actorUserID, id string) (*RelayJob, error) {
	rec, err := h.loadJobRecordForActor(ctx, actorUserID, id)
	if err != nil {
		return nil, err
	}
	return &rec.RelayJob, nil
}

func (h *Handler) AcceptJob(ctx context.Context, id, deviceID string) error {
	_, err := h.db.Exec(ctx, `UPDATE relay_jobs SET executing_device_id = $2::uuid, status = 'accepted', updated_at = now() WHERE id = $1::uuid`, id, deviceID)
	return err
}

func (h *Handler) AcceptJobForActor(ctx context.Context, actorUserID, id, deviceID string, attested bool, requiredCapability string) error {
	if err := h.ensureRelayDeviceAuthorized(ctx, actorUserID, deviceID, requiredCapability); err != nil {
		return err
	}
	if h.requireAttestation && !attested {
		return ErrRelayAttestationRequired
	}
	res, err := h.db.Exec(ctx, `
		UPDATE relay_jobs
		SET executing_device_id = $3::uuid,
		    status = 'accepted',
		    consent_state = 'DEVICE_CONFIRMED',
		    accepted_at = now(),
		    attested_at = CASE WHEN $4::bool THEN now() ELSE attested_at END,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND creator_user_id = $2::uuid
		  AND status = 'queued'
		  AND required_capability = $5
		  AND (expires_at IS NULL OR expires_at > now())
	`, id, actorUserID, deviceID, attested, requiredCapability)
	if err != nil {
		return err
	}
	if tag, ok := res.(interface{ RowsAffected() int64 }); ok && tag.RowsAffected() == 0 {
		return ErrRelayExpired
	}
	return nil
}

var ErrInvalidDeviceSignature = errors.New("invalid_device_signature")

// verifyDeviceSignature verifies a base64-encoded ed25519 signature against the
// stored public key for the given device. The payload should be the exact bytes
// that were signed by the device (for example: "relay_accept:<jobID>:<timestamp>").
func (h *Handler) verifyDeviceSignature(ctx context.Context, deviceID string, payload []byte, sigB64 string) error {
	var pubKeyB64 string
	err := h.db.QueryRow(ctx, `SELECT public_key FROM devices WHERE id = $1::uuid`, deviceID).Scan(&pubKeyB64)
	if err != nil {
		return err
	}
	if pubKeyB64 == "" {
		return ErrInvalidDeviceSignature
	}
	pubBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return err
	}
	if len(pubBytes) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return ErrInvalidDeviceSignature
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), payload, sig) {
		return ErrInvalidDeviceSignature
	}
	return nil
}

func (h *Handler) verifyAcceptanceSignature(ctx context.Context, job *relayJobRecord, deviceID, ts, sig string) error {
	v2Payload := []byte(fmt.Sprintf(
		"relay_accept:v2:%s:%s:%s:%s:%s:%s:%s",
		job.ID,
		deviceID,
		ts,
		job.TransportHint,
		job.RequiredCapability,
		job.ConsentState,
		job.Status,
	))
	if err := h.verifyDeviceSignature(ctx, deviceID, v2Payload, sig); err == nil {
		return nil
	}
	if h.requireAttestation {
		return ErrInvalidDeviceSignature
	}
	return h.verifyDeviceSignature(ctx, deviceID, []byte("relay_accept:"+job.ID+":"+ts), sig)
}

func (d *relayDeviceRecord) hasCapability(required string) bool {
	required = strings.ToUpper(strings.TrimSpace(required))
	if required == "" {
		required = "RELAY_EXECUTOR"
	}
	for _, cap := range d.capabilities {
		switch required {
		case "RELAY_EXECUTOR":
			if strings.EqualFold(cap, "RELAY_EXECUTOR") || strings.EqualFold(cap, "ANDROID_CARRIER") {
				return true
			}
		case "ANDROID_CARRIER":
			if strings.EqualFold(cap, "ANDROID_CARRIER") {
				return true
			}
		default:
			if strings.EqualFold(cap, required) {
				return true
			}
		}
	}
	return false
}

func (h *Handler) canonicalRelayPolicy(transportHint string, content any) (string, string) {
	hint := strings.ToUpper(strings.TrimSpace(transportHint))
	hasMedia := relayContentHasMedia(content)
	switch hint {
	case "", "AUTO":
		if hasMedia {
			return "RELAY_MMS", "ANDROID_CARRIER"
		}
		return "RELAY_SMS", "RELAY_EXECUTOR"
	case "SMS", "RELAY_SMS":
		if hasMedia {
			return "RELAY_MMS", "ANDROID_CARRIER"
		}
		return "RELAY_SMS", "RELAY_EXECUTOR"
	case "MMS", "RELAY_MMS":
		if !hasMedia {
			return "RELAY_SMS", "RELAY_EXECUTOR"
		}
		return "RELAY_MMS", "ANDROID_CARRIER"
	default:
		if hasMedia {
			return "RELAY_MMS", "ANDROID_CARRIER"
		}
		return "RELAY_SMS", "RELAY_EXECUTOR"
	}
}

func relayContentHasMedia(content any) bool {
	switch v := content.(type) {
	case map[string]any:
		for _, key := range []string{"media", "media_json", "attachments", "attachment", "attachment_ids"} {
			if value, ok := v[key]; ok && relayValueHasContent(value) {
				return true
			}
		}
		for _, value := range v {
			if relayContentHasMedia(value) {
				return true
			}
		}
	case []any:
		for _, value := range v {
			if relayContentHasMedia(value) {
				return true
			}
		}
	}
	return false
}

func relayValueHasContent(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(t) != ""
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return true
	}
}

func (h *Handler) FinishJob(ctx context.Context, id string, result any, status string) error {
	resB, _ := json.Marshal(result)
	_, err := h.db.Exec(ctx, `UPDATE relay_jobs SET result = $2::jsonb, status = $3, updated_at = now() WHERE id = $1::uuid`, id, string(resB), status)
	return err
}

func (h *Handler) FinishJobForActor(ctx context.Context, actorUserID, deviceID, id string, result any, status string, requiredCapability string) error {
	if err := h.ensureRelayDeviceAuthorized(ctx, actorUserID, deviceID, requiredCapability); err != nil {
		return err
	}
	resB, _ := json.Marshal(result)
	res, err := h.db.Exec(ctx, `
		UPDATE relay_jobs
		SET result = $4::jsonb,
		    status = $5,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND creator_user_id = $2::uuid
		  AND executing_device_id = $3::uuid
		  AND status = 'accepted'
		  AND (expires_at IS NULL OR expires_at > now())
	`, id, actorUserID, deviceID, string(resB), status)
	if err != nil {
		return err
	}
	if tag, ok := res.(interface{ RowsAffected() int64 }); ok && tag.RowsAffected() == 0 {
		return ErrRelayUnauthorized
	}
	return nil
}

func (h *Handler) ensureRelayDeviceAuthorized(ctx context.Context, actorUserID, deviceID, requiredCapability string) error {
	device, err := h.loadDeviceRecord(ctx, actorUserID, deviceID)
	if err != nil {
		return err
	}
	if requiredCapability == "" {
		requiredCapability = "RELAY_EXECUTOR"
	}
	if !device.hasCapability(requiredCapability) {
		return ErrRelayUnauthorized
	}
	if requiredCapability == "ANDROID_CARRIER" && strings.TrimSpace(device.smsRoleState) != "DEFAULT_SMS_HANDLER" {
		return ErrRelayUnauthorized
	}
	if (requiredCapability == "ANDROID_CARRIER" || h.requireAttestation) && !device.isAttested() {
		return ErrRelayAttestationRequired
	}
	if (requiredCapability == "ANDROID_CARRIER" || h.requireAttestation) && time.Since(device.lastSeenAt) > 24*time.Hour {
		return ErrRelayUnauthorized
	}
	return nil
}

func (d *relayDeviceRecord) isAttested() bool {
	if d == nil || !strings.EqualFold(strings.TrimSpace(d.attestationState), "VERIFIED") {
		return false
	}
	if d.attestationExpiresAt.Valid && !d.attestationExpiresAt.Time.After(time.Now().UTC()) {
		return false
	}
	return true
}
