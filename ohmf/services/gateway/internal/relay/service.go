package relay

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

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

func (s *Service) FinishJob(ctx context.Context, id string, result any, status string) error {
	resB, _ := json.Marshal(result)
	_, err := s.db.Exec(ctx, `UPDATE relay_jobs SET result = $2::jsonb, status = $3, updated_at = now() WHERE id = $1::uuid`, id, string(resB), status)
	return err
}
