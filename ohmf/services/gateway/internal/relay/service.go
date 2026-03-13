package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
	db DB
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: &pgxPoolAdapter{p: db}} }

func NewServiceWithDB(db DB) *Service { return &Service{db: db} }

type RelayJob struct {
	ID                string `json:"id"`
	CreatorUserID     string `json:"creator_user_id"`
	Destination       any    `json:"destination"`
	TransportHint     string `json:"transport_hint"`
	Content           any    `json:"content"`
	Status            string `json:"status"`
	ExecutingDeviceID string `json:"executing_device_id,omitempty"`
	Result            any    `json:"result,omitempty"`
}

func (s *Service) CreateJob(ctx context.Context, creatorID string, destination any, transportHint string, content any) (string, error) {
	id := uuid.New().String()
	destB, _ := json.Marshal(destination)
	contentB, _ := json.Marshal(content)
	_, err := s.db.Exec(ctx, `INSERT INTO relay_jobs (id, creator_user_id, destination, transport_hint, content, status, created_at) VALUES ($1::uuid, $2::uuid, $3::jsonb, $4, $5::jsonb, 'queued', now())`, id, creatorID, string(destB), transportHint, string(contentB))
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListQueuedJobs returns up to `limit` jobs that are currently queued on the server.
func (s *Service) ListQueuedJobs(ctx context.Context, limit int) ([]RelayJob, error) {
	rows, err := s.db.Query(ctx, `SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, result, created_at, updated_at FROM relay_jobs WHERE status = 'queued' ORDER BY created_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RelayJob
	for rows.Next() {
		var id, creator, transport, status, execID string
		var destB, contentB, resultB []byte
		var createdAt, updatedAt *time.Time
		if err := rows.Scan(&id, &creator, &destB, &transport, &contentB, &status, &execID, &resultB, &createdAt, &updatedAt); err != nil {
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
		out = append(out, RelayJob{
			ID:                id,
			CreatorUserID:     creator,
			Destination:       destination,
			TransportHint:     transport,
			Content:           content,
			Status:            status,
			ExecutingDeviceID: execID,
			Result:            result,
		})
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

func (s *Service) GetJob(ctx context.Context, id string) (*RelayJob, error) {
	var destB, contentB, resultB []byte
	var transport, status, creator, execID string
	var updatedAt, createdAt *time.Time
	err := s.db.QueryRow(ctx, `SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, result, created_at, updated_at FROM relay_jobs WHERE id = $1::uuid`, id).Scan(&id, &creator, &destB, &transport, &contentB, &status, &execID, &resultB, &createdAt, &updatedAt)
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
	return &RelayJob{
		ID:                id,
		CreatorUserID:     creator,
		Destination:       destination,
		TransportHint:     transport,
		Content:           content,
		Status:            status,
		ExecutingDeviceID: execID,
		Result:            result,
	}, nil
}

func (s *Service) AcceptJob(ctx context.Context, id, deviceID string) error {
	_, err := s.db.Exec(ctx, `UPDATE relay_jobs SET executing_device_id = $2::uuid, status = 'accepted', updated_at = now() WHERE id = $1::uuid`, id, deviceID)
	return err
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
