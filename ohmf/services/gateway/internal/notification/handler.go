package notification

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/devices"
	"ohmf/services/gateway/internal/middleware"
	"ohmf/services/gateway/internal/push"
	"time"
)


type Handler struct {
	db        *pgxpool.Pool
	devices   *devices.Service
	cfg       config.Config
	client    *http.Client
	fcmProv   push.Provider
	apnsProv  push.Provider
}

func NewHandler(db *pgxpool.Pool, devicesSvc *devices.Service, cfg config.Config) *Handler {
	return &Handler{
		db:      db,
		devices: devicesSvc,
		cfg:     cfg,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// WithFCMProvider attaches an FCM provider to the handler
func (h *Handler) WithFCMProvider(prov push.Provider) *Handler {
	h.fcmProv = prov
	return h
}

// WithAPNsProvider attaches an APNs provider to the handler
func (h *Handler) WithAPNsProvider(prov push.Provider) *Handler {
	h.apnsProv = prov
	return h
}

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
	if err := h.sendNotification(r.Context(), req.UserID, payload); err != nil {
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
	prefs, err := h.getPrefs(r.Context(), userID, deviceID)
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
	updated, err := h.upsertPrefs(r.Context(), prefs)
	if err != nil {
		http.Error(w, "failed_to_update_preferences", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

