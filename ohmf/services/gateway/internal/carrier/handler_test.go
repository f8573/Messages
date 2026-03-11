package carrier

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pgxmock "github.com/pashagolub/pgxmock"

	"github.com/go-chi/chi/v5"

	"ohmf/services/gateway/internal/middleware"
)

type mockSvc struct {
	wantCarrierID string
	wantServerID  string
}

func (m *mockSvc) ImportCarrierMessage(ctx context.Context, deviceID string, c CarrierMessage) (CarrierMessage, error) {
	return CarrierMessage{}, nil
}
func (m *mockSvc) ListCarrierMessagesForDevice(ctx context.Context, deviceID string, since time.Time, limit int) ([]CarrierMessage, error) {
	return nil, nil
}
func (m *mockSvc) SetServerMessageLink(ctx context.Context, carrierMessageID string, serverMessageID string, actor string) (CarrierMessage, error) {
	// simple verification — echo back the serverMessageID
	return CarrierMessage{ID: carrierMessageID, ServerMessageID: &serverMessageID}, nil
}

func (m *mockSvc) ListCarrierMessageLinks(ctx context.Context, carrierMessageID string, limit int) ([]CarrierMessageLinkAudit, error) {
	// Return a couple of sample audit entries for testing
	s1 := "22222222-2222-2222-2222-222222222222"
	now := time.Now().UTC()
	return []CarrierMessageLinkAudit{{ID: "a1", CarrierMessageID: carrierMessageID, ServerMessageID: &s1, SetAt: now, Actor: "tester"}}, nil
}

func (m *mockSvc) ListCarrierMessageLinksByActor(ctx context.Context, actor string, limit int) ([]CarrierMessageLinkAudit, error) {
	// Return a sample entry when actor matches "tester"
	if actor == "tester" {
		s1 := "22222222-2222-2222-2222-222222222222"
		now := time.Now().UTC()
		return []CarrierMessageLinkAudit{{ID: "a1", CarrierMessageID: "11111111-1111-1111-1111-111111111111", ServerMessageID: &s1, SetAt: now, Actor: "tester"}}, nil
	}
	return nil, nil
}

func TestLinkHandler_Success(t *testing.T) {
	svc := &mockSvc{}
	h := NewHandler(svc, nil)

	// Create request
	body := map[string]string{"server_message_id": "22222222-2222-2222-2222-222222222222"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/carrier/messages/11111111-1111-1111-1111-111111111111/link", bytes.NewReader(b))
	// chi URL params are stored in the request context; set them for the test
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "11111111-1111-1111-1111-111111111111")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	// attach a dummy user id in context to satisfy auth check
	ctx := middleware.WithUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.Link(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp CarrierMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected id: %s", resp.ID)
	}
	if resp.ServerMessageID == nil || *resp.ServerMessageID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("unexpected server_message_id: %v", resp.ServerMessageID)
	}
}

func TestLinkHandler_NotOwner(t *testing.T) {
	// Setup a pgx mock pool to simulate no ownership of the carrier message
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	// Service should not be called when ownership check fails; provide a
	// mock that would fail if called.
	svc := &mockSvc{}
	h := NewHandler(svc, &mockAdapter{p: mockPool})

	// Expect the EXISTS query to return false (not owner)
	mockPool.ExpectQuery("SELECT EXISTS").WithArgs("11111111-1111-1111-1111-111111111111", "user-1").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).
			AddRow(false))

	// Create request
	body := map[string]string{"server_message_id": "22222222-2222-2222-2222-222222222222"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/carrier/messages/11111111-1111-1111-1111-111111111111/link", bytes.NewReader(b))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "11111111-1111-1111-1111-111111111111")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	ctx := middleware.WithUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.Link(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLinkHandler_InvalidUUID(t *testing.T) {
	svc := &mockSvc{}
	// Use nil DB so the handler skips ownership check
	h := NewHandler(svc, nil)

	// Create request with invalid UUID
	body := map[string]string{"server_message_id": "not-a-uuid"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/carrier/messages/11111111-1111-1111-1111-111111111111/link", bytes.NewReader(b))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "11111111-1111-1111-1111-111111111111")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	ctx := middleware.WithUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.Link(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestListLinks_Success(t *testing.T) {
	svc := &mockSvc{}
	h := NewHandler(svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/carrier/messages/11111111-1111-1111-1111-111111111111/links", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "11111111-1111-1111-1111-111111111111")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	ctx := middleware.WithUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ListLinks(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Links []CarrierMessageLinkAudit `json:"links"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(resp.Links))
	}
}

func TestListLinks_WithLimit(t *testing.T) {
	svc := &mockSvc{}
	h := NewHandler(svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/carrier/messages/11111111-1111-1111-1111-111111111111/links?limit=1", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "11111111-1111-1111-1111-111111111111")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	ctx := middleware.WithUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ListLinks(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Links []CarrierMessageLinkAudit `json:"links"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(resp.Links))
	}
}

func TestListLinks_NotOwner(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	svc := &mockSvc{}
	h := NewHandler(svc, &mockAdapter{p: mockPool})

	// Expect the EXISTS query to return false (not owner)
	mockPool.ExpectQuery("SELECT EXISTS").WithArgs("11111111-1111-1111-1111-111111111111", "user-1").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).
			AddRow(false))

	req := httptest.NewRequest(http.MethodGet, "/v1/carrier/messages/11111111-1111-1111-1111-111111111111/links", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "11111111-1111-1111-1111-111111111111")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	ctx := middleware.WithUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ListLinks(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAdminListLinks_Forbidden(t *testing.T) {
	svc := &mockSvc{}
	h := NewHandler(svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/carrier_message_links?actor=tester", nil)
	// attach user id but no ADMIN profile
	ctx := middleware.WithUserID(req.Context(), "user-1")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.AdminListLinks(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminListLinks_Success(t *testing.T) {
	svc := &mockSvc{}
	h := NewHandler(svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/carrier_message_links?actor=tester", nil)
	// attach user id and ADMIN profile
	ctx := middleware.WithUserID(req.Context(), "admin-1")
	ctx = middleware.WithProfiles(ctx, []string{"ADMIN"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.AdminListLinks(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
