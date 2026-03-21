package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	pgxmock "github.com/pashagolub/pgxmock"
	"ohmf/services/gateway/internal/middleware"
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

	svc := NewHandlerWithDB(&mockAdapter{p: mock})
	ctx := context.Background()
	destination := map[string]any{"phone_e164": "+15551234567"}
	content := map[string]any{"text": "hello"}
	transportHint, requiredCapability := svc.canonicalRelayPolicy("SMS", content)

	mock.ExpectExec(`INSERT INTO relay_jobs`).
		WithArgs(pgxmock.AnyArg(), "11111111-1111-1111-1111-111111111111", string(mustJSON(t, destination)), transportHint, string(mustJSON(t, content)), requiredCapability).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	jobID, err := svc.CreateJob(ctx, "11111111-1111-1111-1111-111111111111", destination, transportHint, content, requiredCapability)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}
	if jobID == "" {
		t.Fatalf("expected job id")
	}

	rows := pgxmock.NewRows([]string{"id", "creator_user_id", "destination", "transport_hint", "content", "status", "executing_device_id", "consent_state", "required_capability", "expires_at", "accepted_at", "attested_at", "result", "created_at", "updated_at"}).
		AddRow(jobID, "11111111-1111-1111-1111-111111111111", mustJSON(t, destination), transportHint, mustJSON(t, content), "queued", "", "PENDING_DEVICE", requiredCapability, nil, nil, nil, []byte(nil), nil, nil)
	mock.ExpectQuery(`SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, consent_state, required_capability, expires_at, accepted_at, attested_at, result, created_at, updated_at FROM relay_jobs WHERE status = 'queued'`).
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

func TestCanonicalRelayPolicyFallbacks(t *testing.T) {
	svc := NewHandlerWithDB(&mockAdapter{p: nil})

	transport, required := svc.canonicalRelayPolicy("MMS", map[string]any{"text": "hello"})
	if transport != "RELAY_SMS" || required != "RELAY_EXECUTOR" {
		t.Fatalf("expected MMS hint with text-only content to downgrade to SMS, got %s/%s", transport, required)
	}

	transport, required = svc.canonicalRelayPolicy("SMS", map[string]any{"attachments": []any{map[string]any{"id": "att-1"}}})
	if transport != "RELAY_MMS" || required != "ANDROID_CARRIER" {
		t.Fatalf("expected SMS hint with attachments to upgrade to MMS, got %s/%s", transport, required)
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

	svc := NewHandlerWithDB(&mockAdapter{p: mock})
	if err := svc.verifyDeviceSignature(context.Background(), "33333333-3333-3333-3333-333333333333", payload, base64.StdEncoding.EncodeToString(signature)); err != nil {
		t.Fatalf("verifyDeviceSignature failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestVerifyAcceptanceSignatureV2(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	job := &relayJobRecord{
		RelayJob: RelayJob{
			ID:                 "job-1",
			TransportHint:      "RELAY_MMS",
			ConsentState:       "DEVICE_CONFIRMED",
			Status:             "queued",
			RequiredCapability: "ANDROID_CARRIER",
		},
	}
	ts := "1700000000"
	payload := []byte(fmt.Sprintf("relay_accept:v2:%s:%s:%s:%s:%s:%s:%s", job.ID, "33333333-3333-3333-3333-333333333333", ts, job.TransportHint, job.RequiredCapability, job.ConsentState, job.Status))
	signature := ed25519.Sign(privateKey, payload)

	rows := pgxmock.NewRows([]string{"public_key"}).AddRow(base64.StdEncoding.EncodeToString(publicKey))
	mock.ExpectQuery(`SELECT public_key FROM devices WHERE id = \$1::uuid`).
		WithArgs("33333333-3333-3333-3333-333333333333").
		WillReturnRows(rows)

	svc := NewHandlerWithDB(&mockAdapter{p: mock})
	if err := svc.verifyAcceptanceSignature(context.Background(), job, "33333333-3333-3333-3333-333333333333", ts, base64.StdEncoding.EncodeToString(signature)); err != nil {
		t.Fatalf("verifyAcceptanceSignature failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAcceptJobForActorRequiresDeviceCapability(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	now := time.Now().UTC()
	rows := pgxmock.NewRows([]string{"public_key", "capabilities", "sms_role_state", "last_seen_at", "attestation_state", "attestation_expires_at"}).
		AddRow("", []string{"RELAY_EXECUTOR"}, "DEFAULT_SMS_HANDLER", now, "UNVERIFIED", nil)
	mock.ExpectQuery(`SELECT COALESCE\(public_key, ''\),\s+COALESCE\(capabilities, ARRAY\[\]::text\[\]\),\s+COALESCE\(sms_role_state, ''\),\s+COALESCE\(last_seen_at, now\(\)\),\s+COALESCE\(attestation_state, 'UNVERIFIED'\),\s+attestation_expires_at\s+FROM devices\s+WHERE id = \$1::uuid\s+AND user_id = \$2::uuid`).
		WithArgs("44444444-4444-4444-4444-444444444444", "11111111-1111-1111-1111-111111111111").
		WillReturnRows(rows)

	svc := NewHandlerWithDB(&mockAdapter{p: mock})
	err = svc.ensureRelayDeviceAuthorized(context.Background(), "11111111-1111-1111-1111-111111111111", "44444444-4444-4444-4444-444444444444", "ANDROID_CARRIER")
	if !errors.Is(err, ErrRelayUnauthorized) {
		t.Fatalf("expected unauthorized for missing carrier capability, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLoadJobRecordForActorRejectsExpiredQueuedJob(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	expired := time.Now().Add(-time.Minute).UTC()
	rows := pgxmock.NewRows([]string{"id", "creator_user_id", "destination", "transport_hint", "content", "status", "executing_device_id", "consent_state", "required_capability", "expires_at", "accepted_at", "attested_at", "result", "created_at", "updated_at"}).
		AddRow("job-1", "11111111-1111-1111-1111-111111111111", mustJSON(t, map[string]any{"phone_e164": "+15551234567"}), "RELAY_SMS", mustJSON(t, map[string]any{"text": "hello"}), "queued", "", "PENDING_DEVICE", "RELAY_EXECUTOR", sql.NullTime{Time: expired, Valid: true}, nil, nil, []byte(nil), nil, nil)
	mock.ExpectQuery(`SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, consent_state, required_capability, expires_at, accepted_at, attested_at, result, created_at, updated_at FROM relay_jobs WHERE id = \$1::uuid`).
		WithArgs("job-1").
		WillReturnRows(rows)

	svc := NewHandlerWithDB(&mockAdapter{p: mock})
	_, err = svc.loadJobRecordForActor(context.Background(), "11111111-1111-1111-1111-111111111111", "job-1")
	if !errors.Is(err, ErrRelayExpired) {
		t.Fatalf("expected expired error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAcceptHandlerRejectsExpiredJob(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	expired := time.Now().Add(-time.Minute).UTC()
	rows := pgxmock.NewRows([]string{"id", "creator_user_id", "destination", "transport_hint", "content", "status", "executing_device_id", "consent_state", "required_capability", "expires_at", "accepted_at", "attested_at", "result", "created_at", "updated_at"}).
		AddRow("job-1", "11111111-1111-1111-1111-111111111111", mustJSON(t, map[string]any{"phone_e164": "+15551234567"}), "RELAY_SMS", mustJSON(t, map[string]any{"text": "hello"}), "queued", "", "PENDING_DEVICE", "RELAY_EXECUTOR", sql.NullTime{Time: expired, Valid: true}, nil, nil, []byte(nil), nil, nil)
	mock.ExpectQuery(`SELECT id::text, creator_user_id::text, destination, transport_hint, content, status, executing_device_id::text, consent_state, required_capability, expires_at, accepted_at, attested_at, result, created_at, updated_at FROM relay_jobs WHERE id = \$1::uuid`).
		WithArgs("job-1").
		WillReturnRows(rows)

	svc := NewHandlerWithDB(&mockAdapter{p: mock})
	req := httptest.NewRequest(http.MethodPost, "/v1/relay/jobs/job-1/accept", strings.NewReader(`{"device_id":"44444444-4444-4444-4444-444444444444"}`))
	req = req.WithContext(middleware.WithUserID(req.Context(), "11111111-1111-1111-1111-111111111111"))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", "job-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rec := httptest.NewRecorder()

	svc.Accept(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410 for expired job, got %d", rec.Code)
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
