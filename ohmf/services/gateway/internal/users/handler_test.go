package users

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"ohmf/services/gateway/internal/middleware"
)

func TestBlockUserUsesRouteParamWithoutBody(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	handler := NewHandler(&Service{db: mock})
	router := chi.NewRouter()
	router.Post("/blocks/{id}", func(w http.ResponseWriter, r *http.Request) {
		handler.BlockUser(w, r.WithContext(middleware.WithUserID(r.Context(), "actor-1")))
	})

	mock.ExpectExec(`INSERT INTO user_blocks \(blocker_user_id, blocked_user_id, created_at\)`).
		WithArgs("actor-1", "target-1").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	request := httptest.NewRequest(http.MethodPost, "/blocks/target-1", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d: %s", http.StatusNoContent, recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
