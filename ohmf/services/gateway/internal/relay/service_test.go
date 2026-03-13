package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	pgxmock "github.com/pashagolub/pgxmock"
)

type mockAdapter struct{ p pgxmock.PgxPoolIface }

type rowScannerWrapper struct {
	r interface{ Scan(dest ...any) error }
}

func (w *rowScannerWrapper) Scan(dest ...any) error { return w.r.Scan(dest...) }

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

func (m *mockAdapter) QueryRow(ctx context.Context, sql string, args ...any) RowScanner {
	return &rowScannerWrapper{r: m.p.QueryRow(ctx, sql, args...)}
}

func (m *mockAdapter) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := m.p.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &rowsWrapper{r: rows}, nil
}

func (m *mockAdapter) Exec(ctx context.Context, sql string, args ...any) (any, error) {
	return m.p.Exec(ctx, sql, args...)
}

func TestCreateListAcceptAndFinishJob(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	svc := NewServiceWithDB(&mockAdapter{p: mock})
	ctx := context.Background()
	destination := map[string]any{"phone_e164": "+15551234567"}
	content := map[string]any{"text": "hello"}

	mock.ExpectExec(`INSERT INTO relay_jobs`).
		WithArgs(pgxmock.AnyArg(), "11111111-1111-1111-1111-111111111111", string(mustJSON(t, destination)), "SMS", string(mustJSON(t, content))).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	jobID, err := svc.CreateJob(ctx, "11111111-1111-1111-1111-111111111111", destination, "SMS", content)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}
	if jobID == "" {
		t.Fatalf("expected job id")
	}

	rows := pgxmock.NewRows([]string{"id", "creator_user_id", "destination", "transport_hint", "content", "status", "executing_device_id", "result", "created_at", "updated_at"}).
		AddRow(jobID, "11111111-1111-1111-1111-111111111111", mustJSON(t, destination), "SMS", mustJSON(t, content), "queued", "", []byte(nil), nil, nil)
	mock.ExpectQuery(`SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, result, created_at, updated_at FROM relay_jobs WHERE status = 'queued'`).
		WithArgs(10).
		WillReturnRows(rows)

	jobs, err := svc.ListQueuedJobs(ctx, 10)
	if err != nil {
		t.Fatalf("ListQueuedJobs failed: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != jobID {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}

	mock.ExpectExec(`UPDATE relay_jobs SET executing_device_id = \$2::uuid, status = 'accepted', updated_at = now\(\) WHERE id = \$1::uuid`).
		WithArgs(jobID, "22222222-2222-2222-2222-222222222222").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := svc.AcceptJob(ctx, jobID, "22222222-2222-2222-2222-222222222222"); err != nil {
		t.Fatalf("AcceptJob failed: %v", err)
	}

	result := map[string]any{"status": StatusCompleted}
	mock.ExpectExec(`UPDATE relay_jobs SET result = \$2::jsonb, status = \$3, updated_at = now\(\) WHERE id = \$1::uuid`).
		WithArgs(jobID, string(mustJSON(t, result)), StatusCompleted).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := svc.FinishJob(ctx, jobID, result, StatusCompleted); err != nil {
		t.Fatalf("FinishJob failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestVerifyDeviceSignature(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("relay_accept:job-1:1700000000")
	signature := ed25519.Sign(privateKey, payload)

	rows := pgxmock.NewRows([]string{"public_key"}).AddRow(base64.StdEncoding.EncodeToString(publicKey))
	mock.ExpectQuery(`SELECT public_key FROM devices WHERE id = \$1::uuid`).
		WithArgs("33333333-3333-3333-3333-333333333333").
		WillReturnRows(rows)

	svc := NewServiceWithDB(&mockAdapter{p: mock})
	if err := svc.verifyDeviceSignature(context.Background(), "33333333-3333-3333-3333-333333333333", payload, base64.StdEncoding.EncodeToString(signature)); err != nil {
		t.Fatalf("verifyDeviceSignature failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
