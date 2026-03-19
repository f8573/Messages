package notification

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/devices"
	"ohmf/services/gateway/internal/middleware"
	"time"
)


type Handler struct {
	db      *pgxpool.Pool
	devices *devices.Handler
	cfg     config.Config
	client  *http.Client
}

func NewHandler(db *pgxpool.Pool, devicesSvc *devices.Handler, cfg config.Config) *Handler {
	return &Handler{
		db:      db,
		devices: devicesSvc,
		cfg:     cfg,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
} }

type sendRequest struct {
	UserID         string         `json:"user_id"`
	Title          string         `json:"title"`
	Body           string         `json:"body"`
	ConversationID string         `json:"conversation_id,omitempty"`
	Encrypted      bool           `json:"encrypted,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.UserID == "" || req.Title == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	payload := NotificationPayload{
		Title:          req.Title,
		Body:           req.Body,
		ConversationID: req.ConversationID,
		Encrypted:      req.Encrypted,
		Data:           req.Data,
	}
	if err := h.Send(r.Context(), req.UserID, payload); err != nil {
		http.Error(w, "failed to enqueue", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	deviceID, ok := middleware.DeviceIDFromContext(r.Context())
	if !ok || deviceID == "" {
		http.Error(w, "missing_device_id", http.StatusBadRequest)
		return
	}
	prefs, err := h.GetPreferences(r.Context(), userID, deviceID)
	if err != nil {
		http.Error(w, "failed_to_load_preferences", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prefs)
}

func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	deviceID, ok := middleware.DeviceIDFromContext(r.Context())
	if !ok || deviceID == "" {
		http.Error(w, "missing_device_id", http.StatusBadRequest)
		return
	}
	var prefs Preferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	prefs.UserID = userID
	prefs.DeviceID = deviceID
	updated, err := h.UpsertPreferences(r.Context(), prefs)
	if err != nil {
		http.Error(w, "failed_to_update_preferences", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
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

func (h *Handler) Send(ctx context.Context, userID string, p NotificationPayload) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = h.db.Exec(ctx, `
        INSERT INTO notifications (user_id, payload, status, created_at)
        VALUES ($1::uuid, $2::jsonb, 'pending', $3)
	`, userID, string(b), time.Now())
	return err
}

func (h *Handler) GetPreferences(ctx context.Context, userID, deviceID string) (Preferences, error) {
	var prefs Preferences
	err := h.db.QueryRow(ctx, `
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

func (h *Handler) UpsertPreferences(ctx context.Context, prefs Preferences) (Preferences, error) {
	if _, err := h.db.Exec(ctx, `
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
	return h.GetPreferences(ctx, prefs.UserID, prefs.DeviceID)
}

func (h *Handler) DispatchPending(ctx context.Context, limit int) error {
	if !h.cfg.EnableWebPush {
		return nil
	}
	tx, err := h.db.BeginTx(ctx, pgx.TxOptions{})
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
		subscriptions, err := h.devices.ListWebPushSubscriptions(ctx, item.userID)
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
				Subscriber:      h.cfg.WebPushSubject,
				VAPIDPublicKey:  h.cfg.WebPushVAPIDPublicKey,
				VAPIDPrivateKey: h.cfg.WebPushVAPIDPrivateKey,
				TTL:             30,
				HTTPClient:      h.client,
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

func (h *Handler) HasUsableWebPush() bool {
	return h.cfg.EnableWebPush && h.cfg.WebPushVAPIDPublicKey != "" && h.cfg.WebPushVAPIDPrivateKey != ""
}

func IsMissingPrefs(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
