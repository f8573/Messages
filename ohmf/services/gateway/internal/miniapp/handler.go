package miniapp

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

type Handler struct {
	svc *Service
}

// removed: trivial constructor wrapper
func (h *Handler) RegisterManifest(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}

	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}

	manifest := any(raw)
	if wrapped, ok := raw["manifest"]; ok {
		manifest = wrapped
	}

	id, err := h.svc.RegisterManifest(r.Context(), userID, manifest)
	if err != nil {
		if errors.Is(err, ErrManifestRequired) || errors.Is(err, ErrManifestInvalid) || errors.Is(err, ErrManifestSignatureRequired) || errors.Is(err, ErrManifestSignatureInvalid) {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_manifest", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "register_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}

	var req struct {
		ManifestID         string               `json:"manifest_id"`
		AppID              string               `json:"app_id"`
		ConversationID     string               `json:"conversation_id"`
		Viewer             SessionParticipant   `json:"viewer"`
		Participants       []SessionParticipant `json:"participants"`
		GrantedPermissions []string             `json:"capabilities_granted"`
		TTLSeconds         int                  `json:"ttl_seconds"`
		StateSnapshot      any                  `json:"state_snapshot"`
		ResumeExisting     *bool                `json:"resume_existing"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.ConversationID == "" || (req.ManifestID == "" && req.AppID == "") {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "conversation_id and app_id or manifest_id are required", nil)
		return
	}

	if req.Viewer.UserID == "" {
		req.Viewer.UserID = userID
	}
	if req.Viewer.Role == "" {
		req.Viewer.Role = "PLAYER"
	}

	ttl := 30 * time.Minute
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	resumeExisting := true
	if req.ResumeExisting != nil {
		resumeExisting = *req.ResumeExisting
	}

	session, created, err := h.svc.CreateSessionForUser(r.Context(), userID, CreateSessionInput{
		ManifestID:         req.ManifestID,
		AppID:              req.AppID,
		ConversationID:     req.ConversationID,
		Viewer:             req.Viewer,
		Participants:       req.Participants,
		GrantedPermissions: req.GrantedPermissions,
		StateSnapshot:      req.StateSnapshot,
		TTL:                ttl,
		ResumeExisting:     resumeExisting,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrManifestNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "manifest_not_found", err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "create_failed", err.Error(), nil)
		}
		return
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	httpx.WriteJSON(w, status, session)
}

func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
		return
	}
	s, err := h.svc.GetSessionForUser(r.Context(), userID, id)
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		case errors.Is(err, ErrSessionEnded):
			httpx.WriteError(w, r, http.StatusConflict, "session_ended", err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "get_failed", err.Error(), nil)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}

func (h *Handler) EndSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
		return
	}
	if err := h.svc.EndSessionForUser(r.Context(), userID, id); err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		case errors.Is(err, ErrSessionEnded):
			httpx.WriteError(w, r, http.StatusConflict, "session_ended", err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "end_failed", err.Error(), nil)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AppendEvent(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
		return
	}
	var req struct {
		EventName string `json:"event_name"`
		Body      any    `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.EventName == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "event_name required", nil)
		return
	}
	seq, err := h.svc.AppendEventForUser(r.Context(), userID, id, req.EventName, req.Body)
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		case errors.Is(err, ErrSessionEnded):
			httpx.WriteError(w, r, http.StatusConflict, "session_ended", err.Error(), nil)
		case errors.Is(err, ErrMiniAppConsent):
			httpx.WriteError(w, r, http.StatusForbidden, "consent_required", err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "append_failed", err.Error(), nil)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"event_seq": seq})
}

func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
		return
	}
	var req struct {
		State              any      `json:"state"`
		StateVersion       int      `json:"state_version"`
		GrantedPermissions []string `json:"capabilities_granted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	version, err := h.svc.SnapshotSessionForUser(r.Context(), userID, id, req.State, req.StateVersion)
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		case errors.Is(err, ErrSessionEnded):
			httpx.WriteError(w, r, http.StatusConflict, "session_ended", err.Error(), nil)
		case errors.Is(err, ErrStateVersionConflict):
			httpx.WriteError(w, r, http.StatusConflict, "state_version_conflict", err.Error(), nil)
		case errors.Is(err, ErrMiniAppConsent):
			httpx.WriteError(w, r, http.StatusForbidden, "consent_required", err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "snapshot_failed", err.Error(), nil)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"state_version": version})
}

func (h *Handler) JoinSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "session id required", nil)
		return
	}
	var req struct {
		GrantedPermissions []string `json:"capabilities_granted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	session, err := h.svc.JoinSession(r.Context(), userID, id, req.GrantedPermissions)
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		case errors.Is(err, ErrSessionEnded):
			httpx.WriteError(w, r, http.StatusConflict, "session_ended", err.Error(), nil)
		case err.Error() == "conversation_access_denied":
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, session)
}

func (h *Handler) Share(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		ManifestID         string   `json:"manifest_id"`
		AppID              string   `json:"app_id"`
		ConversationID     string   `json:"conversation_id"`
		GrantedPermissions []string `json:"capabilities_granted"`
		StateSnapshot      any      `json:"state_snapshot"`
		ResumeExisting     *bool    `json:"resume_existing"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.ConversationID == "" || (req.ManifestID == "" && req.AppID == "") {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "conversation_id and app_id or manifest_id are required", nil)
		return
	}
	resumeExisting := true
	if req.ResumeExisting != nil {
		resumeExisting = *req.ResumeExisting
	}
	result, err := h.svc.ShareSession(r.Context(), userID, ShareInput{
		ManifestID:         req.ManifestID,
		AppID:              req.AppID,
		ConversationID:     req.ConversationID,
		GrantedPermissions: req.GrantedPermissions,
		StateSnapshot:      req.StateSnapshot,
		ResumeExisting:     resumeExisting,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrManifestNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "manifest_not_found", err.Error(), nil)
		case errors.Is(err, ErrMiniAppUnsupported):
			httpx.WriteError(w, r, http.StatusConflict, "miniapp_unsupported", "conversation is not eligible for app sharing", nil)
		case err.Error() == "conversation_access_denied":
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "share_failed", err.Error(), nil)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (h *Handler) ListApps(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	items, err := h.svc.ListManifests(r.Context())
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	appID := chi.URLParam(r, "appID")
	if appID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "app id required", nil)
		return
	}
	item, err := h.svc.GetManifestByAppID(r.Context(), appID)
	if err != nil {
		if errors.Is(err, ErrManifestNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "get_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
