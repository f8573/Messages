package devices

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Device struct {
	ID            string    `json:"device_id"`
	UserID        string    `json:"user_id"`
	Platform      string    `json:"platform"`
	DeviceName    string    `json:"device_name"`
	ClientVersion string    `json:"client_version"`
	Capabilities  []string  `json:"capabilities"`
	SMSRoleState  string    `json:"sms_role_state"`
	PushToken     string    `json:"push_token"`
	PublicKey     string    `json:"public_key"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

func (s *Service) RegisterDevice(ctx context.Context, userID string, d Device) (string, error) {
	d.Capabilities = normalizeCapabilities(d.Platform, d.Capabilities)
	var id string
	err := s.db.QueryRow(ctx, `
		INSERT INTO devices (user_id, platform, device_name, client_version, capabilities, sms_role_state, push_token, public_key, last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
		RETURNING id::text
	`, userID, d.Platform, d.DeviceName, d.ClientVersion, d.Capabilities, d.SMSRoleState, nullable(d.PushToken), nullable(d.PublicKey)).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) ListDevices(ctx context.Context, userID string) ([]Device, error) {
	rows, err := s.db.Query(ctx, `SELECT id::text, user_id::text, platform, device_name, client_version, capabilities, sms_role_state, push_token, public_key, last_seen_at FROM devices WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var caps []string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Platform, &d.DeviceName, &d.ClientVersion, &caps, &d.SMSRoleState, &d.PushToken, &d.PublicKey, &d.LastSeenAt); err != nil {
			return nil, err
		}
		d.Capabilities = caps
		out = append(out, d)
	}
	return out, nil
}

func (s *Service) RevokeDevice(ctx context.Context, userID, deviceID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM devices WHERE user_id = $1 AND id = $2::uuid`, userID, deviceID)
	return err
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
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
	}
	return out
}
