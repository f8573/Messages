package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
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


type Handler struct {
	db                  DB
	requireAttestation  bool
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
	id, err := h.CreateJob(r.Context(), userID, req.Destination, req.TransportHint, req.Content)
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
	sig := r.Header.Get("X-Device-Signature")
	ts := r.Header.Get("X-Device-Timestamp")
	attested := false
	if sig != "" || ts != "" {
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
		payload := []byte("relay_accept:" + id + ":" + ts)
		if err := h.verifyDeviceSignature(r.Context(), req.DeviceID, payload, sig); err != nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "invalid device signature", nil)
			return
		}
		attested = true
	}
	if err := h.AcceptJobForActor(r.Context(), actorUserID, id, req.DeviceID, attested); err != nil {
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
	if err := h.FinishJobForActor(r.Context(), actorUserID, req.DeviceID, id, req.Result, req.Status); err != nil {
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

func (h *Handler) CreateJob(ctx context.Context, creatorID string, destination any, transportHint string, content any) (string, error) {
	id := uuid.New().String()
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
		VALUES ($1::uuid, $2::uuid, $3::jsonb, $4, $5::jsonb, 'queued', 'PENDING_DEVICE', 'RELAY_EXECUTOR', now() + interval '10 minute', now())
	`, id, creatorID, string(destB), transportHint, string(contentB))
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
		var createdAt, updatedAt, expiresAt, acceptedAt, attestedAt *time.Time
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
		if expiresAt != nil {
			job.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
		}
		if acceptedAt != nil {
			job.AcceptedAt = acceptedAt.UTC().Format(time.RFC3339Nano)
		}
		if attestedAt != nil {
			job.AttestedAt = attestedAt.UTC().Format(time.RFC3339Nano)
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

func (h *Handler) GetJob(ctx context.Context, id string) (*RelayJob, error) {
	var destB, contentB, resultB []byte
	var transport, status, creator, execID, consentState, requiredCapability string
	var updatedAt, createdAt, expiresAt, acceptedAt, attestedAt *time.Time
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
	job := &RelayJob{
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
	if expiresAt != nil {
		job.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	if acceptedAt != nil {
		job.AcceptedAt = acceptedAt.UTC().Format(time.RFC3339Nano)
	}
	if attestedAt != nil {
		job.AttestedAt = attestedAt.UTC().Format(time.RFC3339Nano)
	}
	return job, nil
}

func (h *Handler) GetJobForActor(ctx context.Context, actorUserID, id string) (*RelayJob, error) {
	job, err := h.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.CreatorUserID != actorUserID {
		return nil, ErrRelayUnauthorized
	}
	return job, nil
}

func (h *Handler) AcceptJob(ctx context.Context, id, deviceID string) error {
	_, err := h.db.Exec(ctx, `UPDATE relay_jobs SET executing_device_id = $2::uuid, status = 'accepted', updated_at = now() WHERE id = $1::uuid`, id, deviceID)
	return err
}

func (h *Handler) AcceptJobForActor(ctx context.Context, actorUserID, id, deviceID string, attested bool) error {
	if h.requireAttestation && !attested {
		return ErrRelayAttestationRequired
	}
	if err := h.ensureRelayDeviceAuthorized(ctx, actorUserID, deviceID); err != nil {
		return err
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
		  AND (expires_at IS NULL OR expires_at > now())
	`, id, actorUserID, deviceID, attested)
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

func (h *Handler) FinishJob(ctx context.Context, id string, result any, status string) error {
	resB, _ := json.Marshal(result)
	_, err := h.db.Exec(ctx, `UPDATE relay_jobs SET result = $2::jsonb, status = $3, updated_at = now() WHERE id = $1::uuid`, id, string(resB), status)
	return err
}

func (h *Handler) FinishJobForActor(ctx context.Context, actorUserID, deviceID, id string, result any, status string) error {
	if err := h.ensureRelayDeviceAuthorized(ctx, actorUserID, deviceID); err != nil {
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
	`, id, actorUserID, deviceID, string(resB), status)
	if err != nil {
		return err
	}
	if tag, ok := res.(interface{ RowsAffected() int64 }); ok && tag.RowsAffected() == 0 {
		return ErrRelayUnauthorized
	}
	return nil
}

func (h *Handler) ensureRelayDeviceAuthorized(ctx context.Context, actorUserID, deviceID string) error {
	var exists bool
	err := h.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM devices
			WHERE id = $1::uuid
			  AND user_id = $2::uuid
			  AND ('RELAY_EXECUTOR' = ANY(capabilities) OR 'ANDROID_CARRIER' = ANY(capabilities))
		)
	`, deviceID, actorUserID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrRelayUnauthorized
	}
	return nil
}
