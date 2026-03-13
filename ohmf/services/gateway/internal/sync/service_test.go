package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pgxmock "github.com/pashagolub/pgxmock"

	"ohmf/services/gateway/internal/middleware"
)

type mockDB struct{ p pgxmock.PgxPoolIface }

type rowsWrapper struct {
	r interface {
		Next() bool
		Scan(dest ...any) error
		Close()
	}
}

func (w *rowsWrapper) Next() bool             { return w.r.Next() }
func (w *rowsWrapper) Scan(dest ...any) error { return w.r.Scan(dest...) }
func (w *rowsWrapper) Close()                 { w.r.Close() }

func (m *mockDB) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := m.p.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &rowsWrapper{r: rows}, nil
}

func TestIncrementalSyncBuildsOpaqueCursor(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	createdAt := time.Date(2026, 3, 12, 18, 0, 0, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"id", "conversation_id", "sender_user_id", "content_type", "content", "server_order", "created_at"}).
		AddRow("msg-1", "conv-1", "user-1", "text", []byte(`{"text":"hello"}`), int64(7), createdAt)
	mock.ExpectQuery(`SELECT id::text, conversation_id::text, sender_user_id::text, content_type, content, server_order, created_at FROM messages WHERE created_at > \$1 ORDER BY created_at ASC LIMIT \$2`).
		WithArgs(pgxmock.AnyArg(), 50).
		WillReturnRows(rows)

	svc := NewServiceWithDB(&mockDB{p: mock})
	resp, err := svc.IncrementalSync(context.Background(), "", 50)
	if err != nil {
		t.Fatalf("IncrementalSync failed: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.Events))
	}
	if resp.NextCursor == "" {
		t.Fatalf("expected next cursor")
	}

	cursorBytes, err := base64.StdEncoding.DecodeString(resp.NextCursor)
	if err != nil {
		t.Fatalf("expected opaque cursor, decode failed: %v", err)
	}
	var cursor opaqueCursor
	if err := json.Unmarshal(cursorBytes, &cursor); err != nil {
		t.Fatalf("cursor json failed: %v", err)
	}
	if cursor.LastServerOrder["conv-1"] != 7 {
		t.Fatalf("expected last server order in cursor, got %+v", cursor.LastServerOrder)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestIncrementalHandlerRequiresAuth(t *testing.T) {
	handler := NewHandler(NewServiceWithDB(&mockDB{}))
	req := httptest.NewRequest(http.MethodGet, "/v1/sync", nil)
	rr := httptest.NewRecorder()

	handler.Incremental(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestIncrementalHandlerReturnsPayload(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	createdAt := time.Date(2026, 3, 12, 18, 15, 0, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"id", "conversation_id", "sender_user_id", "content_type", "content", "server_order", "created_at"}).
		AddRow("msg-2", "conv-2", "user-2", "text", []byte(`{"text":"hi"}`), int64(9), createdAt)
	mock.ExpectQuery(`SELECT id::text, conversation_id::text, sender_user_id::text, content_type, content, server_order, created_at FROM messages WHERE created_at > \$1 ORDER BY created_at ASC LIMIT \$2`).
		WithArgs(pgxmock.AnyArg(), 25).
		WillReturnRows(rows)

	handler := NewHandler(NewServiceWithDB(&mockDB{p: mock}))
	req := httptest.NewRequest(http.MethodGet, "/v1/sync?limit=25", nil)
	req = req.WithContext(middleware.WithUserID(req.Context(), "user-2"))
	rr := httptest.NewRecorder()

	handler.Incremental(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "next_cursor") {
		t.Fatalf("expected sync payload with next_cursor")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
