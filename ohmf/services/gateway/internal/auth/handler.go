package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) StartPhone(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	resp, err := h.svc.StartPhoneVerification(r.Context(), req, r.RemoteAddr)
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) VerifyPhone(w http.ResponseWriter, r *http.Request) {
	var req VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	resp, err := h.svc.VerifyPhone(r.Context(), req, r.RemoteAddr)
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	resp, err := h.svc.Refresh(r.Context(), req)
	if err != nil {
		handleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tokens": resp})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req LogoutRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.svc.Logout(r.Context(), userID, req); err != nil {
		handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrChallengeNotFound):
		httpx.WriteError(w, r, http.StatusBadRequest, "challenge_not_found", err.Error(), nil)
	case errors.Is(err, ErrChallengeExpired):
		httpx.WriteError(w, r, http.StatusBadRequest, "challenge_expired", err.Error(), nil)
	case errors.Is(err, ErrInvalidOTP):
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_otp", err.Error(), nil)
	case errors.Is(err, ErrInvalidRefresh):
		httpx.WriteError(w, r, http.StatusUnauthorized, "invalid_refresh", err.Error(), nil)
	case errors.Is(err, ErrRateLimited):
		httpx.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", err.Error(), nil)
	case errors.Is(err, ErrOTPDeliveryFailed):
		httpx.WriteError(w, r, http.StatusBadGateway, "otp_delivery_failed", err.Error(), nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal_error", err.Error(), nil)
	}
}
