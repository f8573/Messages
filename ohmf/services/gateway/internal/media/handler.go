package media

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"ohmf/services/gateway/internal/httpx"
	"ohmf/services/gateway/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		AttachmentID string `json:"attachment_id"`
		MessageID    string `json:"message_id"`
		FileName     string `json:"file_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.AttachmentID == "" || req.MessageID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "attachment_id and message_id are required", nil)
		return
	}
	if err := h.svc.AssociateAttachment(r.Context(), req.AttachmentID, req.MessageID, req.FileName); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "register_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) Purge(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "attachment id required", nil)
		return
	}
	if _, err := h.svc.PurgeAttachment(r.Context(), id); err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "purge_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateUploadToken(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	var req struct {
		AttachmentID   string `json:"attachment_id"`
		MimeType       string `json:"mime_type"`
		Size           int64  `json:"size_bytes"`
		FileName       string `json:"file_name"`
		ChecksumSHA256 string `json:"checksum_sha256"`
		TTLSeconds     int    `json:"ttl_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid body", nil)
		return
	}
	if req.MimeType == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "mime_type required", nil)
		return
	}
	ttl := 15 * time.Minute
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	upload, err := h.svc.CreateUploadToken(r.Context(), req.AttachmentID, req.MimeType, req.FileName, req.ChecksumSHA256, req.Size, ttl)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "token_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, upload)
}

func (h *Handler) UploadObject(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "token required", nil)
		return
	}
	defer r.Body.Close()
	upload, err := h.svc.UploadObject(r.Context(), token, io.LimitReader(r.Body, 100<<20))
	if err != nil {
		status := http.StatusInternalServerError
		code := "upload_failed"
		switch {
		case errors.Is(err, ErrUploadTokenNotFound):
			status = http.StatusNotFound
			code = "upload_token_not_found"
		case errors.Is(err, ErrChecksumMismatch):
			status = http.StatusBadRequest
			code = "checksum_mismatch"
		case errors.Is(err, ErrUploadIncomplete):
			status = http.StatusBadRequest
			code = "upload_incomplete"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, upload)
}

func (h *Handler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "token required", nil)
		return
	}
	attachmentID, err := h.svc.CompleteUpload(r.Context(), token)
	if err != nil {
		status := http.StatusInternalServerError
		code := "complete_failed"
		switch {
		case errors.Is(err, ErrUploadTokenNotFound):
			status = http.StatusNotFound
			code = "upload_token_not_found"
		case errors.Is(err, ErrChecksumMismatch):
			status = http.StatusBadRequest
			code = "checksum_mismatch"
		case errors.Is(err, ErrUploadIncomplete):
			status = http.StatusBadRequest
			code = "upload_incomplete"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"attachment_id": attachmentID})
}

func (h *Handler) CreateDownload(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing auth", nil)
		return
	}
	attachmentID := chi.URLParam(r, "id")
	if attachmentID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "attachment id required", nil)
		return
	}
	download, err := h.svc.CreateDownload(r.Context(), userID, attachmentID, 10*time.Minute)
	if err != nil {
		status := http.StatusInternalServerError
		code := "download_failed"
		if errors.Is(err, ErrAttachmentForbidden) {
			status = http.StatusForbidden
			code = "attachment_forbidden"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, download)
}

func (h *Handler) DownloadObject(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", "download token required", nil)
		return
	}
	reader, fileName, mimeType, sizeBytes, err := h.svc.OpenDownload(r.Context(), token)
	if err != nil {
		status := http.StatusInternalServerError
		code := "download_failed"
		if errors.Is(err, ErrAttachmentForbidden) {
			status = http.StatusForbidden
			code = "attachment_forbidden"
		}
		httpx.WriteError(w, r, status, code, err.Error(), nil)
		return
	}
	defer reader.Close()
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	if fileName != "" {
		w.Header().Set("Content-Disposition", `inline; filename="`+fileName+`"`)
	}
	if sizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
	}
	_, _ = io.Copy(w, reader)
}
