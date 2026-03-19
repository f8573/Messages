package events

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"ohmf/services/gateway/internal/middleware"
)

func TestStreamSupportsWrappedResponseWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = middleware.WithUserID(ctx, "11111111-1111-1111-1111-111111111111")

	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	wrapped := unwrapOnlyWriter{ResponseWriter: rec}

	NewHandler(fakeDB{
		row: fakeRow{values: []any{time.Unix(1, 0).UTC(), time.Unix(2, 0).UTC(), time.Unix(3, 0).UTC(), int64(3)}},
	}, nil, nil).Stream(wrapped, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "event: sync_required") {
		t.Fatalf("expected sync_required event, got %q", body)
	}
}

func TestStreamInitialSnapshotFailureDoesNotReturn500(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = middleware.WithUserID(ctx, "11111111-1111-1111-1111-111111111111")

	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	NewHandler(fakeDB{row: fakeRow{err: errors.New("db unavailable")}}, nil, nil).Stream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `event: error`) || !strings.Contains(body, `snapshot_unavailable`) {
		t.Fatalf("expected snapshot_unavailable event, got %q", body)
	}
}

type unwrapOnlyWriter struct {
	http.ResponseWriter
}

func (w unwrapOnlyWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

type fakeDB struct {
	row fakeRow
}

func (f fakeDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return f.row
}

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *time.Time:
			*d = r.values[i].(time.Time)
		case *int64:
			*d = r.values[i].(int64)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}
