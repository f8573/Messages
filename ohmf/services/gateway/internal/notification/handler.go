package notification

import (
	"encoding/json"
	"net/http"

	"ohmf/services/gateway/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

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
	if err := h.svc.Send(r.Context(), req.UserID, payload); err != nil {
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
	prefs, err := h.svc.GetPreferences(r.Context(), userID, deviceID)
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
	updated, err := h.svc.UpsertPreferences(r.Context(), prefs)
	if err != nil {
		http.Error(w, "failed_to_update_preferences", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}
