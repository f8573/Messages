package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/devices"
)

type Service struct {
	db      *pgxpool.Pool
	devices *devices.Service
	cfg     config.Config
	client  *http.Client
}

func NewService(db *pgxpool.Pool, devicesSvc *devices.Service, cfg config.Config) *Service {
	return &Service{
		db:      db,
		devices: devicesSvc,
		cfg:     cfg,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

type Preferences struct {
	UserID                         string `json:"user_id"`
	DeviceID                       string `json:"device_id"`
	PushEnabled                    bool   `json:"push_enabled"`
	MuteUnknownSenders             bool   `json:"mute_unknown_senders"`
	ShowPreviews                   bool   `json:"show_previews"`
	MutedConversationNotifications bool   `json:"muted_conversation_notifications"`
}

type NotificationPayload struct {
	Title          string         `json:"title"`
	Body           string         `json:"body"`
	ConversationID string         `json:"conversation_id,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	Encrypted      bool           `json:"encrypted,omitempty"`
}

type subscriptionEnvelope struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (s *Service) Send(ctx context.Context, userID string, p NotificationPayload) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
        INSERT INTO notifications (user_id, payload, status, created_at)
        VALUES ($1::uuid, $2::jsonb, 'pending', $3)
	`, userID, string(b), time.Now())
	return err
}

func (s *Service) GetPreferences(ctx context.Context, userID, deviceID string) (Preferences, error) {
	var prefs Preferences
	err := s.db.QueryRow(ctx, `
		SELECT user_id::text, device_id::text, push_enabled, mute_unknown_senders, show_previews, muted_conversation_notifications
		FROM notification_preferences
		WHERE user_id = $1::uuid AND device_id = $2::uuid
	`, userID, deviceID).Scan(&prefs.UserID, &prefs.DeviceID, &prefs.PushEnabled, &prefs.MuteUnknownSenders, &prefs.ShowPreviews, &prefs.MutedConversationNotifications)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Preferences{
				UserID:                         userID,
				DeviceID:                       deviceID,
				PushEnabled:                    true,
				ShowPreviews:                   true,
				MutedConversationNotifications: false,
			}, nil
		}
		return Preferences{}, err
	}
	return prefs, nil
}

func (s *Service) UpsertPreferences(ctx context.Context, prefs Preferences) (Preferences, error) {
	if _, err := s.db.Exec(ctx, `
		INSERT INTO notification_preferences (
			user_id,
			device_id,
			push_enabled,
			mute_unknown_senders,
			show_previews,
			muted_conversation_notifications,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, now(), now())
		ON CONFLICT (user_id, device_id)
		DO UPDATE SET
			push_enabled = EXCLUDED.push_enabled,
			mute_unknown_senders = EXCLUDED.mute_unknown_senders,
			show_previews = EXCLUDED.show_previews,
			muted_conversation_notifications = EXCLUDED.muted_conversation_notifications,
			updated_at = now()
	`, prefs.UserID, prefs.DeviceID, prefs.PushEnabled, prefs.MuteUnknownSenders, prefs.ShowPreviews, prefs.MutedConversationNotifications); err != nil {
		return Preferences{}, err
	}
	return s.GetPreferences(ctx, prefs.UserID, prefs.DeviceID)
}

func (s *Service) DispatchPending(ctx context.Context, limit int) error {
	if !s.cfg.EnableWebPush {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id::text, user_id::text, payload
		FROM notifications
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pending struct {
		id      string
		userID  string
		payload NotificationPayload
	}
	items := make([]pending, 0, limit)
	for rows.Next() {
		var id, userID string
		var raw []byte
		if err := rows.Scan(&id, &userID, &raw); err != nil {
			return err
		}
		var payload NotificationPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			payload = NotificationPayload{Title: "OHMF", Body: "New message"}
		}
		items = append(items, pending{id: id, userID: userID, payload: payload})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range items {
		subscriptions, err := s.devices.ListWebPushSubscriptions(ctx, item.userID)
		if err != nil {
			return err
		}
		delivered := false
		lastErr := ""
		for _, raw := range subscriptions {
			env := subscriptionEnvelope{}
			if err := json.Unmarshal([]byte(raw), &env); err != nil {
				lastErr = err.Error()
				continue
			}
			sub := &webpush.Subscription{
				Endpoint: env.Endpoint,
				Keys: webpush.Keys{
					P256dh: env.Keys.P256DH,
					Auth:   env.Keys.Auth,
				},
			}
			body := item.payload.Body
			if item.payload.Encrypted {
				body = "New encrypted message"
			}
			payload, _ := json.Marshal(map[string]any{
				"title":           item.payload.Title,
				"body":            body,
				"conversation_id": item.payload.ConversationID,
				"data":            item.payload.Data,
			})
			resp, err := webpush.SendNotification(payload, sub, &webpush.Options{
				Subscriber:      s.cfg.WebPushSubject,
				VAPIDPublicKey:  s.cfg.WebPushVAPIDPublicKey,
				VAPIDPrivateKey: s.cfg.WebPushVAPIDPrivateKey,
				TTL:             30,
				HTTPClient:      s.client,
			})
			if err != nil {
				lastErr = err.Error()
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				delivered = true
			} else {
				lastErr = resp.Status
			}
		}

		status := "skipped"
		if delivered {
			status = "delivered"
		} else if lastErr != "" {
			status = "failed"
		}
		if _, err := tx.Exec(ctx, `
			UPDATE notifications
			SET status = $2,
			    attempt_count = attempt_count + 1,
			    delivered = $3,
			    delivered_at = CASE WHEN $3::bool THEN now() ELSE delivered_at END,
			    last_error = NULLIF($4, ''),
			    updated_at = now()
			WHERE id = $1::uuid
		`, item.id, status, delivered, lastErr); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Service) HasUsableWebPush() bool {
	return s.cfg.EnableWebPush && s.cfg.WebPushVAPIDPublicKey != "" && s.cfg.WebPushVAPIDPrivateKey != ""
}

func IsMissingPrefs(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
