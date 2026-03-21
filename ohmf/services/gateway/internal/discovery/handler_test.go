package discovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	pgxmock "github.com/pashagolub/pgxmock/v4"
	"ohmf/services/gateway/internal/middleware"
)

func TestDiscoverVersionedHashMatch(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("new mock pool: %v", err)
	}
	defer mock.Close()

	handler := NewHandler(mock, "pepper")
	phone := "+15555550123"
	hash := handler.hashPhone(discoveryAlgorithmV2, phone)

	mock.ExpectQuery("(?s)SELECT ec\\.phone_e164, COALESCE\\(u\\.id::text, ''\\), COALESCE\\(u\\.display_name, ''\\).*FROM external_contacts ec.*LEFT JOIN users u ON u\\.primary_phone_e164 = ec\\.phone_e164").WillReturnRows(
		pgxmock.NewRows([]string{"phone_e164", "id", "display_name"}).
			AddRow(phone, "user-123", "Ada"),
	)

	req := httptest.NewRequest(http.MethodPost, "/discovery", bytes.NewBufferString(`{"algorithm":"SHA256_PEPPERED_V2","contacts":[{"hash":"`+hash+`"}]}`))
	req = req.WithContext(middleware.WithUserID(context.Background(), "user-1"))
	rec := httptest.NewRecorder()

	handler.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDiscoverRejectsOversizedContactBatchesAndRateLimits(t *testing.T) {
	t.Setenv("APP_DISCOVERY_RATE_PER_USER", "1")

	handler := NewHandler(nil, "pepper")
	body := `{"algorithm":"SHA256_PEPPERED_V1","contacts":[]}`

	req := httptest.NewRequest(http.MethodPost, "/discovery", bytes.NewBufferString(body))
	req = req.WithContext(middleware.WithUserID(context.Background(), "user-1"))
	rec := httptest.NewRecorder()
	handler.Discover(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/discovery", bytes.NewBufferString(body))
	req = req.WithContext(middleware.WithUserID(context.Background(), "user-1"))
	rec = httptest.NewRecorder()
	handler.Discover(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d: %s", rec.Code, rec.Body.String())
	}

	big := make([]Contact, discoveryContactLimit()+1)
	for i := range big {
		sum := sha256.Sum256([]byte("phone-" + string(rune(i))))
		big[i] = Contact{Hash: hex.EncodeToString(sum[:])}
	}

	payload := `{"algorithm":"SHA256_PEPPERED_V1","contacts":[`
	for i, c := range big {
		if i > 0 {
			payload += ","
		}
		payload += `{"hash":"` + c.Hash + `"}`
	}
	payload += `]}`

	req = httptest.NewRequest(http.MethodPost, "/discovery", bytes.NewBufferString(payload))
	req = req.WithContext(middleware.WithUserID(context.Background(), "user-2"))
	rec = httptest.NewRecorder()
	handler = NewHandler(nil, "pepper")
	handler.Discover(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected oversized batch to be rejected, got %d: %s", rec.Code, rec.Body.String())
	}
}
