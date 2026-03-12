package relay

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) CreateMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		Destination   any    `json:"destination"`
		TransportHint string `json:"transport_hint"`
		Content       any    `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	id, err := h.svc.CreateJob(r.Context(), userID, req.Destination, req.TransportHint, req.Content)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "create_failed", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"relay_job_id": id})
}

func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	job, err := h.svc.GetJob(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func (h *Handler) ListAvailable(w http.ResponseWriter, r *http.Request) {
	// device-authenticated agents poll for available jobs
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	// optional limit query param
	q := r.URL.Query().Get("limit")
	limit := 10
	if q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	jobs, err := h.svc.ListQueuedJobs(r.Context(), limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "list_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	// device must provide a signature of the accept action to prove identity
	sig := r.Header.Get("X-Device-Signature")
	ts := r.Header.Get("X-Device-Timestamp")
	if sig == "" || ts == "" {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing device signature headers", nil)
		return
	}
	// validate timestamp (seconds) to avoid replay attacks
	tsec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid timestamp", nil)
		return
	}
	now := time.Now().Unix()
	if tsec < now-60 || tsec > now+60 {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "stale timestamp", nil)
		return
	}
	payload := []byte("relay_accept:" + id + ":" + ts)
	if err := h.svc.verifyDeviceSignature(r.Context(), req.DeviceID, payload, sig); err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "invalid device signature", nil)
		return
	}
	if err := h.svc.AcceptJob(r.Context(), id, req.DeviceID); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "accept_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Result(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Status string `json:"status"`
		Result any    `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if err := h.svc.FinishJob(r.Context(), id, req.Result, req.Status); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "result_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
