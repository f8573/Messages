package events

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pgxmock "github.com/pashagolub/pgxmock"
	"ohmf/services/gateway/internal/middleware"
)

func TestStreamSupportsWrappedResponseWriter(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	mockPool.ExpectQuery("SELECT").
		WithArgs("11111111-1111-1111-1111-111111111111").
		WillReturnRows(pgxmock.NewRows([]string{"conv_max", "msg_max", "total"}).
			AddRow(time.Unix(1, 0).UTC(), time.Unix(2, 0).UTC(), int64(3)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = middleware.WithUserID(ctx, "11111111-1111-1111-1111-111111111111")

	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	wrapped := unwrapOnlyWriter{ResponseWriter: rec}

	NewHandler(mockPool).Stream(wrapped, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "event: sync_required") {
		t.Fatalf("expected sync_required event, got %q", body)
	}
	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestStreamInitialSnapshotFailureDoesNotReturn500(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	mockPool.ExpectQuery("SELECT").
		WithArgs("11111111-1111-1111-1111-111111111111").
		WillReturnError(errors.New("db unavailable"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = middleware.WithUserID(ctx, "11111111-1111-1111-1111-111111111111")

	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	NewHandler(mockPool).Stream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `event: error`) || !strings.Contains(body, `snapshot_unavailable`) {
		t.Fatalf("expected snapshot_unavailable event, got %q", body)
	}
	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

type unwrapOnlyWriter struct {
	http.ResponseWriter
}

func (w unwrapOnlyWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
