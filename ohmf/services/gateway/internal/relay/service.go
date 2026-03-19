package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

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

type Service struct {
	db                 DB
	requireAttestation bool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: &pgxPoolAdapter{p: db}} }

func NewServiceWithDB(db DB) *Service { return &Service{db: db} }

type Options struct {
	RequireAttestation bool
}

func NewServiceWithOptions(db *pgxpool.Pool, opts Options) *Service {
	return &Service{db: &pgxPoolAdapter{p: db}, requireAttestation: opts.RequireAttestation}
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

func (s *Service) CreateJob(ctx context.Context, creatorID string, destination any, transportHint string, content any) (string, error) {
	id := uuid.New().String()
	destB, _ := json.Marshal(destination)
	contentB, _ := json.Marshal(content)
	_, err := s.db.Exec(ctx, `
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
func (s *Service) ListQueuedJobs(ctx context.Context, limit int) ([]RelayJob, error) {
	return s.listQueuedJobs(ctx, "", limit)
}

func (s *Service) ListQueuedJobsForActor(ctx context.Context, actorUserID string, limit int) ([]RelayJob, error) {
	return s.listQueuedJobs(ctx, actorUserID, limit)
}

func (s *Service) listQueuedJobs(ctx context.Context, actorUserID string, limit int) ([]RelayJob, error) {
	query := `SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, consent_state, required_capability, expires_at, accepted_at, attested_at, result, created_at, updated_at FROM relay_jobs WHERE status = 'queued' AND (expires_at IS NULL OR expires_at > now())`
	args := []any{}
	if strings.TrimSpace(actorUserID) != "" {
		query += ` AND creator_user_id = $1::uuid ORDER BY created_at ASC LIMIT $2`
		args = append(args, actorUserID, limit)
	} else {
		query += ` ORDER BY created_at ASC LIMIT $1`
		args = append(args, limit)
	}
	rows, err := s.db.Query(ctx, query, args...)
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

func (s *Service) GetJob(ctx context.Context, id string) (*RelayJob, error) {
	var destB, contentB, resultB []byte
	var transport, status, creator, execID, consentState, requiredCapability string
	var updatedAt, createdAt, expiresAt, acceptedAt, attestedAt *time.Time
	err := s.db.QueryRow(ctx, `SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, consent_state, required_capability, expires_at, accepted_at, attested_at, result, created_at, updated_at FROM relay_jobs WHERE id = $1::uuid`, id).Scan(&id, &creator, &destB, &transport, &contentB, &status, &execID, &consentState, &requiredCapability, &expiresAt, &acceptedAt, &attestedAt, &resultB, &createdAt, &updatedAt)
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

func (s *Service) GetJobForActor(ctx context.Context, actorUserID, id string) (*RelayJob, error) {
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.CreatorUserID != actorUserID {
		return nil, ErrRelayUnauthorized
	}
	return job, nil
}

func (s *Service) AcceptJob(ctx context.Context, id, deviceID string) error {
	_, err := s.db.Exec(ctx, `UPDATE relay_jobs SET executing_device_id = $2::uuid, status = 'accepted', updated_at = now() WHERE id = $1::uuid`, id, deviceID)
	return err
}

func (s *Service) AcceptJobForActor(ctx context.Context, actorUserID, id, deviceID string, attested bool) error {
	if s.requireAttestation && !attested {
		return ErrRelayAttestationRequired
	}
	if err := s.ensureRelayDeviceAuthorized(ctx, actorUserID, deviceID); err != nil {
		return err
	}
	res, err := s.db.Exec(ctx, `
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
func (s *Service) verifyDeviceSignature(ctx context.Context, deviceID string, payload []byte, sigB64 string) error {
	var pubKeyB64 string
	err := s.db.QueryRow(ctx, `SELECT public_key FROM devices WHERE id = $1::uuid`, deviceID).Scan(&pubKeyB64)
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

func (s *Service) FinishJob(ctx context.Context, id string, result any, status string) error {
	resB, _ := json.Marshal(result)
	_, err := s.db.Exec(ctx, `UPDATE relay_jobs SET result = $2::jsonb, status = $3, updated_at = now() WHERE id = $1::uuid`, id, string(resB), status)
	return err
}

func (s *Service) FinishJobForActor(ctx context.Context, actorUserID, deviceID, id string, result any, status string) error {
	if err := s.ensureRelayDeviceAuthorized(ctx, actorUserID, deviceID); err != nil {
		return err
	}
	resB, _ := json.Marshal(result)
	res, err := s.db.Exec(ctx, `
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

func (s *Service) ensureRelayDeviceAuthorized(ctx context.Context, actorUserID, deviceID string) error {
	var exists bool
	err := s.db.QueryRow(ctx, `
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
