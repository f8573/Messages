package miniapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

// Handler dispatches mini-app runtime requests.
// OWNERSHIP: Gateway owns sessions, events, snapshots, joins, shares.
// Apps service owns registry, releases, installs, update detection.
// See: docs/miniapp/ownership-boundaries.md
func NewHandler(svc *Service, registry *RegistryClient) *Handler {
	return &Handler{Svc: svc, Registry: registry}
}

type Handler struct {
	Svc      *Service
	Registry *RegistryClient
}

const (
	maxMiniappParticipants       = 64
	maxMiniappGrantedPermissions = 32
	maxMiniappStateBytes         = 128 * 1024
	maxMiniappEventBodyBytes     = 32 * 1024
	maxMiniappEventNameBytes     = 96
	maxMiniappTTLSeconds         = 24 * 60 * 60
)


func statusOr(actual, fallback int) int {
	if actual > 0 {
		return actual
	}
	return fallback
}

func withQuery(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	encoded := values.Encode()
	if encoded == "" {
		return path
	}
	return path + "?" + encoded
}

func validateGrantedPermissions(values []string) error {
	if len(values) > maxMiniappGrantedPermissions {
		return errors.New("too many capabilities_granted values")
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return errors.New("capabilities_granted contains empty value")
		}
		if len(value) > 120 {
			return errors.New("capabilities_granted entry too long")
		}
		if _, ok := seen[value]; ok {
			return errors.New("capabilities_granted contains duplicate values")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateParticipants(values []SessionParticipant) error {
	if len(values) > maxMiniappParticipants {
		return errors.New("too many participants")
	}
	for _, participant := range values {
		if len(strings.TrimSpace(participant.UserID)) > 120 || len(strings.TrimSpace(participant.Role)) > 64 || len(strings.TrimSpace(participant.DisplayName)) > 160 {
			return errors.New("participant field too long")
		}
	}
	return nil
}

func validatePayloadSize(label string, value any, maxBytes int) error {
	if value == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) > maxBytes {
		return errors.New(label + " exceeds size limit")
	}
	return nil
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

	id, err := h.Svc.RegisterManifest(r.Context(), userID, manifest)
	if err != nil {
		if errors.Is(err, ErrManifestRequired) || errors.Is(err, ErrManifestInvalid) || errors.Is(err, ErrManifestSignatureRequired) || errors.Is(err, ErrManifestSignatureInvalid) {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_manifest", err.Error(), nil)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "register_failed", err.Error(), nil)
		return
	}
	if h.Registry != nil {
		if _, _, err := h.Registry.doJSON(r.Context(), http.MethodPost, "/v1/apps/register", userID, map[string]any{"manifest": manifest}); err != nil {
			httpx.WriteError(w, r, http.StatusBadGateway, "registry_failed", err.Error(), nil)
			return
		}
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
		// Permission expansion: client must acknowledge re-consent if update requires new permissions
		Reconsented bool `json:"reconsented,omitempty"`
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
	if err := validateParticipants(req.Participants); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := validateGrantedPermissions(req.GrantedPermissions); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := validatePayloadSize("state_snapshot", req.StateSnapshot, maxMiniappStateBytes); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	ttl := 30 * time.Minute
	if req.TTLSeconds > 0 {
		if req.TTLSeconds > maxMiniappTTLSeconds {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "ttl_seconds exceeds maximum", nil)
			return
		}
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	resumeExisting := true
	if req.ResumeExisting != nil {
		resumeExisting = *req.ResumeExisting
	}

	session, created, err := h.Svc.CreateSessionForUser(r.Context(), userID, CreateSessionInput{
		ManifestID:         req.ManifestID,
		AppID:              req.AppID,
		ConversationID:     req.ConversationID,
		Viewer:             req.Viewer,
		Participants:       req.Participants,
		GrantedPermissions: req.GrantedPermissions,
		StateSnapshot:      req.StateSnapshot,
		TTL:                ttl,
		ResumeExisting:     resumeExisting,
		Reconsented:        req.Reconsented,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrManifestNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "manifest_not_found", err.Error(), nil)
		case errors.Is(err, ErrReleaseSuspended):
			httpx.WriteError(w, r, http.StatusForbidden, "release_suspended", "this app release has been suspended", nil)
		case errors.Is(err, ErrReleaseRevoked):
			httpx.WriteError(w, r, http.StatusForbidden, "release_revoked", "this app release has been revoked", nil)
		case errors.Is(err, ErrMiniAppConsent):
			httpx.WriteError(w, r, http.StatusForbidden, "reconsent_required", "app update requires permission re-consent", nil)
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, "create_failed", err.Error(), nil)
		}
		return
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}

	// P3.2 Isolated Runtime Origins: Add origin and CSP headers to session response
	if session != nil {
		if appID, ok := session["app_id"].(string); ok {
			if appVersion, ok := session["app_version"].(string); ok {
				h.attachOriginConfig(w, appID, appVersion)
			}
		}
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
	s, err := h.Svc.GetSessionForUser(r.Context(), userID, id)
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

// P4.1 Event Model: GetSessionEvents retrieves the event log for a session
func (h *Handler) GetSessionEvents(w http.ResponseWriter, r *http.Request) {
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

	// Verify user has access to session (check session exists and user can access conversation)
	_, err := h.Svc.GetSessionForUser(r.Context(), userID, id)
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", err.Error(), nil)
		}
		return
	}

	// Parse query parameters for filtering and pagination
	eventType := r.URL.Query().Get("event_type")
	if eventType == "" {
		eventType = ""
	}
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsedL, err := strconv.Atoi(l); err == nil && parsedL > 0 {
			limit = parsedL
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsedO, err := strconv.Atoi(o); err == nil && parsedO >= 0 {
			offset = parsedO
		}
	}

	var eventTypePtr *string
	if eventType != "" {
		eventTypePtr = &eventType
	}

	events, err := h.Svc.GetSessionEvents(r.Context(), id, eventTypePtr, limit, offset)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "events_failed", err.Error(), nil)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"limit":  limit,
		"offset": offset,
	})
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
	if err := h.Svc.EndSessionForUser(r.Context(), userID, id); err != nil {
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
	if len(req.EventName) > maxMiniappEventNameBytes {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "event_name too long", nil)
		return
	}
	if err := validatePayloadSize("event body", req.Body, maxMiniappEventBodyBytes); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	seq, err := h.Svc.AppendEventForUser(r.Context(), userID, id, req.EventName, req.Body)
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		case errors.Is(err, ErrSessionEnded):
			httpx.WriteError(w, r, http.StatusConflict, "session_ended", err.Error(), nil)
		case errors.Is(err, ErrMiniAppConsent):
			httpx.WriteError(w, r, http.StatusForbidden, "consent_required", err.Error(), nil)
		case errors.Is(err, ErrBridgeMethodNotAllowed):
			httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "method not allowed with granted capabilities", nil)
		case errors.Is(err, ErrBridgeMethodRateLimited):
			httpx.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "capability rate limit exceeded", nil)
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
	if err := validateGrantedPermissions(req.GrantedPermissions); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := validatePayloadSize("state", req.State, maxMiniappStateBytes); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	version, err := h.Svc.SnapshotSessionForUser(r.Context(), userID, id, req.State, req.StateVersion)
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
	if err := validateGrantedPermissions(req.GrantedPermissions); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	session, err := h.Svc.JoinSession(r.Context(), userID, id, req.GrantedPermissions)
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
	if err := validateGrantedPermissions(req.GrantedPermissions); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := validatePayloadSize("state_snapshot", req.StateSnapshot, maxMiniappStateBytes); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	resumeExisting := true
	if req.ResumeExisting != nil {
		resumeExisting = *req.ResumeExisting
	}
	result, err := h.Svc.ShareSession(r.Context(), userID, ShareInput{
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
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	if h.Registry == nil {
		items, err := h.Svc.ListManifests(r.Context(), userID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	payload, status, err := h.Registry.doJSON(r.Context(), http.MethodGet, withQuery("/v1/apps", r.URL.Query()), userID, nil)
	if err != nil {
		httpx.WriteError(w, r, statusOr(status, http.StatusBadGateway), "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	appID := chi.URLParam(r, "appID")
	if appID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "app id required", nil)
		return
	}
	if h.Registry == nil {
		item, err := h.Svc.GetManifestByAppID(r.Context(), userID, appID)
		if err != nil {
			if errors.Is(err, ErrManifestNotFound) {
				httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
		return
	}
	payload, status, err := h.Registry.doJSON(r.Context(), http.MethodGet, withQuery("/v1/apps/"+appID, r.URL.Query()), userID, nil)
	if err != nil {
		httpx.WriteError(w, r, statusOr(status, http.StatusBadGateway), "get_failed", err.Error(), nil)
		return
	}
	if h.Svc != nil && payload != nil {
		if m, ok := payload["manifest"].(map[string]any); ok && len(m) > 0 {
			_, _ = h.Svc.RegisterManifest(r.Context(), userID, m)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) InstallApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	appID := chi.URLParam(r, "appID")
	if appID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "app id required", nil)
		return
	}
	if h.Registry == nil {
		item, err := h.Svc.InstallApp(r.Context(), userID, appID)
		if err != nil {
			if errors.Is(err, ErrManifestNotFound) {
				httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError, "install_failed", err.Error(), nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
		return
	}
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	payload, status, err := h.Registry.doJSON(r.Context(), http.MethodPost, withQuery("/v1/apps/"+appID+"/install", r.URL.Query()), userID, body)
	if err != nil {
		httpx.WriteError(w, r, statusOr(status, http.StatusBadGateway), "install_failed", err.Error(), nil)
		return
	}
	if h.Svc != nil && payload != nil {
		if m, ok := payload["manifest"].(map[string]any); ok && len(m) > 0 {
			_, _ = h.Svc.RegisterManifest(r.Context(), userID, m)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) UninstallApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	appID := chi.URLParam(r, "appID")
	if appID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "app id required", nil)
		return
	}
	if h.Registry == nil {
		if err := h.Svc.UninstallApp(r.Context(), userID, appID); err != nil {
			if errors.Is(err, ErrAppInstallNotFound) {
				httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError, "uninstall_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_, status, err := h.Registry.doJSON(r.Context(), http.MethodDelete, "/v1/apps/"+appID+"/install", userID, nil)
	if err != nil {
		httpx.WriteError(w, r, statusOr(status, http.StatusBadGateway), "uninstall_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListInstalledApps(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	if h.Registry == nil {
		items, err := h.Svc.ListManifests(r.Context(), userID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			install, _ := item["install"].(map[string]any)
			if installed, _ := install["installed"].(bool); installed {
				filtered = append(filtered, item)
			}
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": filtered})
		return
	}
	payload, status, err := h.Registry.doJSON(r.Context(), http.MethodGet, withQuery("/v1/apps/installed", r.URL.Query()), userID, nil)
	if err != nil {
		httpx.WriteError(w, r, statusOr(status, http.StatusBadGateway), "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) CheckForUpdates(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	appID := chi.URLParam(r, "appID")
	if appID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "app id required", nil)
		return
	}
	if h.Registry == nil {
		item, err := h.Svc.GetManifestByAppID(r.Context(), userID, appID)
		if err != nil {
			if errors.Is(err, ErrManifestNotFound) {
				httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		install, _ := item["install"].(map[string]any)
		version, _ := item["version"].(string)
		installedVersion, _ := install["installed_version"].(string)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"app_id":                  appID,
			"installed_version":       installedVersion,
			"latest_version":          version,
			"update_available":        installedVersion != "" && version != "" && installedVersion != version,
			"latest_approved_version": version,
		})
		return
	}
	payload, status, err := h.Registry.doJSON(r.Context(), http.MethodGet, withQuery("/v1/apps/"+appID+"/updates", r.URL.Query()), userID, nil)
	if err != nil {
		httpx.WriteError(w, r, statusOr(status, http.StatusBadGateway), "get_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) RegistryPassthrough(path string, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := middleware.UserIDFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
			return
		}
		if h.Registry == nil {
			httpx.WriteError(w, r, http.StatusNotImplemented, "registry_unavailable", "registry backend is not configured", nil)
			return
		}
		var body map[string]any
		if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
				return
			}
		}
		payload, status, err := h.Registry.doJSON(r.Context(), method, withQuery(path, r.URL.Query()), userID, body)
		if err != nil {
			httpx.WriteError(w, r, statusOr(status, http.StatusBadGateway), "registry_failed", err.Error(), nil)
			return
		}
		if status == http.StatusNoContent {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		httpx.WriteJSON(w, statusOr(status, http.StatusOK), payload)
	}
}

// P3.2 Isolated Runtime Origins: attachOriginConfig generates and attaches origin isolation configuration to the response.
// This ensures each mini-app runtime gets a deterministic, unique origin to prevent CSRF and DOM inspection attacks.
func (h *Handler) attachOriginConfig(w http.ResponseWriter, appID, releaseID string) {
	cfg := config.GenerateOriginConfig(config.OriginGenerationParams{
		AppID:       appID,
		ReleaseID:   releaseID,
		BaseDomain:  "miniapp.local", // TODO: make configurable via config
		SubdomainLen: 8,
	})

	// Set Content-Security-Policy header to enforce isolation
	if cfg.CSPHeader != "" {
		w.Header().Set("Content-Security-Policy", cfg.CSPHeader)
	}

	// Set X-Mini-App-Origin header to inform client of assigned origin
	w.Header().Set("X-Mini-App-Origin", cfg.AppOrigin)

	// Set X-Content-Type-Options to prevent MIME sniffing
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Set X-Frame-Options to prevent clickjacking (iframe sandboxing supplements this)
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
}
