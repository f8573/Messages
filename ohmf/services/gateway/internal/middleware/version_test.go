package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ohmf/services/gateway/internal/config"
)

func TestAPIVersionMiddleware_BasicHeaders(t *testing.T) {
	cfg := config.Config{}
	mw := APIVersionMiddleware(cfg)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-OHMF-API-Version"); got == "" {
		t.Fatalf("expected X-OHMF-API-Version header, got empty")
	}
	if got := rr.Header().Get("X-OHMF-Spec-Version"); got == "" {
		t.Fatalf("expected X-OHMF-Spec-Version header, got empty")
	}
}

func TestAPIVersionMiddleware_ClientAPIWarning(t *testing.T) {
	cfg := config.Config{}
	mw := APIVersionMiddleware(cfg)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-OHMF-Client-API", "v2")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if warn := rr.Header().Get("Warning"); warn == "" {
		t.Fatalf("expected Warning header for incompatible client API")
	}
}

func TestAPIVersionMiddleware_AcceptVersion_Negotiation(t *testing.T) {
	cfg := config.Config{}
	mw := APIVersionMiddleware(cfg)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Version", ">=2.0.0 <3.0.0")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotAcceptable {
		t.Fatalf("expected 406 Not Acceptable, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "client requested unacceptable API version") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestAPIVersionMiddleware_AcceptVersion_SemverAccepts(t *testing.T) {
	cfg := config.Config{}
	mw := APIVersionMiddleware(cfg)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Version", ">=1.0.0 <2.0.0")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
}

// removed: test input comments repeated the header values

func TestAPIVersionMiddleware_DeprecationAndSunset(t *testing.T) {
	cfg := config.Config{APIDeprecation: "Tue, 01 Jul 2025 00:00:00 GMT", APISunset: "2026-01-01"}
	mw := APIVersionMiddleware(cfg)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Deprecation"); got != cfg.APIDeprecation {
		t.Fatalf("expected Deprecation header %q, got %q", cfg.APIDeprecation, got)
	}
	if got := rr.Header().Get("Sunset"); got != cfg.APISunset {
		t.Fatalf("expected Sunset header %q, got %q", cfg.APISunset, got)
	}
}
