package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateCORS_AllowedOrigin(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000", "http://localhost:5174"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         3600,
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/test", nil)
	r.Header.Set("Origin", "http://localhost:3000")

	result := ValidateCORS(w, r, policy)

	if !result {
		t.Errorf("ValidateCORS: expected true for allowed origin, got false")
	}

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", w.Header().Get("Access-Control-Allow-Origin"), "http://localhost:3000")
	}
}

func TestValidateCORS_UnauthorizedOrigin(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/test", nil)
	r.Header.Set("Origin", "http://evil.com")

	result := ValidateCORS(w, r, policy)

	if result {
		t.Errorf("ValidateCORS: expected false for unauthorized origin, got true")
	}

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Access-Control-Allow-Origin: expected empty for unauthorized origin, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestValidateCORS_EmptyOrigin(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/test", nil)
	// No Origin header

	result := ValidateCORS(w, r, policy)

	if !result {
		t.Errorf("ValidateCORS: expected true for empty origin, got false")
	}
}

func TestValidateCORS_WildcardOrigin(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/test", nil)
	r.Header.Set("Origin", "http://any.origin.com")

	result := ValidateCORS(w, r, policy)

	if !result {
		t.Errorf("ValidateCORS: expected true for wildcard, got false")
	}

	if w.Header().Get("Access-Control-Allow-Origin") != "http://any.origin.com" {
		t.Errorf("Access-Control-Allow-Origin: got %q, want %q", w.Header().Get("Access-Control-Allow-Origin"), "http://any.origin.com")
	}
}

func TestValidateCORS_LocalhostPattern(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
	}

	tests := []struct {
		origin   string
		allowed  bool
		wantHeader string
	}{
		{"http://localhost:3000", true, "http://localhost:3000"},
		{"http://localhost:5174", true, "http://localhost:5174"},
		{"http://localhost:8080", true, "http://localhost:8080"},
		{"http://127.0.0.1:3000", false, ""},
		{"http://evil.com:3000", false, ""},
	}

	for _, tt := range tests {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/v1/test", nil)
		r.Header.Set("Origin", tt.origin)

		result := ValidateCORS(w, r, policy)

		if result != tt.allowed {
			t.Errorf("ValidateCORS(%q): got %v, want %v", tt.origin, result, tt.allowed)
		}

		if w.Header().Get("Access-Control-Allow-Origin") != tt.wantHeader {
			t.Errorf("Access-Control-Allow-Origin for %q: got %q, want %q", tt.origin, w.Header().Get("Access-Control-Allow-Origin"), tt.wantHeader)
		}
	}
}

func TestValidateCORS_ResponseHeaders(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization", "X-Request-ID"},
		ExposedHeaders: []string{"X-Request-ID", "X-RateLimit-Remaining"},
		MaxAge:         7200,
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/test", nil)
	r.Header.Set("Origin", "http://localhost:3000")

	ValidateCORS(w, r, policy)

	if got := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "GET") || !strings.Contains(got, "POST") {
		t.Errorf("Access-Control-Allow-Methods: got %q", got)
	}

	if got := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Errorf("Access-Control-Allow-Headers: got %q", got)
	}

	if got := w.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "X-Request-ID") {
		t.Errorf("Access-Control-Expose-Headers: got %q", got)
	}

	if w.Header().Get("Access-Control-Max-Age") != string(rune(7200)) {
		t.Errorf("Access-Control-Max-Age: got %q", w.Header().Get("Access-Control-Max-Age"))
	}
}

func TestHandlePreflight_ValidOrigin(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         3600,
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/v1/test", nil)
	r.Header.Set("Origin", "http://localhost:3000")

	HandlePreflight(w, r, policy)

	if w.Code != http.StatusNoContent {
		t.Errorf("HandlePreflight: status code got %d, want %d", w.Code, http.StatusNoContent)
	}

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin: got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestHandlePreflight_UnauthorizedOrigin(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/v1/test", nil)
	r.Header.Set("Origin", "http://evil.com")

	HandlePreflight(w, r, policy)

	if w.Code != http.StatusForbidden {
		t.Errorf("HandlePreflight: status code got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandlePreflight_NonOptionsRequest(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/test", nil)
	r.Header.Set("Origin", "http://localhost:3000")

	HandlePreflight(w, r, policy)

	// Should do nothing for non-OPTIONS requests
	if w.Code != http.StatusOK {
		t.Errorf("HandlePreflight(GET): expected no action, got status %d", w.Code)
	}
}

func TestCORSMiddleware_OptionsRequest(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Authorization"},
	}

	handler := CORSMiddleware(policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/v1/test", nil)
	r.Header.Set("Origin", "http://localhost:3000")

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("CORSMiddleware(OPTIONS): got status %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestCORSMiddleware_ActualRequest(t *testing.T) {
	policy := CORSPolicy{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST"},
	}

	handler := CORSMiddleware(policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/test", nil)
	r.Header.Set("Origin", "http://localhost:3000")

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("CORSMiddleware(GET): got status %d, want %d", w.Code, http.StatusOK)
	}

	if w.Body.String() != "success" {
		t.Errorf("CORSMiddleware: body got %q, want %q", w.Body.String(), "success")
	}

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("CORSMiddleware: Access-Control-Allow-Origin got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestBearerTokenAuthNoCookies(t *testing.T) {
	// Verify that Bearer token auth works without credentials mode
	policy := CORSPolicy{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-Request-ID"},
		MaxAge:           3600,
		AllowCredentials: false, // Important: no credentials for Bearer tokens
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Authorization", "Bearer token_123")

	ValidateCORS(w, r, policy)

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("Bearer token CORS: Origin not set")
	}

	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("Bearer token CORS: AllowCredentials should not be set, got %q", w.Header().Get("Access-Control-Allow-Credentials"))
	}
}

func TestCORSPolicy_ProductionConfig(t *testing.T) {
	// Production-like configuration for mini-app platform
	policy := CORSPolicy{
		AllowedOrigins: []string{
			"https://app.example.com",
			"https://miniapps.example.com",
			"http://localhost:*", // for local development
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"X-Request-ID", "Link"},
		MaxAge:           3600,
		AllowCredentials: false, // Bearer tokens, no cookies
	}

	origins := []struct {
		origin  string
		allowed bool
	}{
		{"https://app.example.com", true},
		{"https://miniapps.example.com", true},
		{"http://localhost:3000", true},
		{"http://localhost:5174", true},
		{"https://attacker.com", false},
		{"https://app.example.com:8080", false},
	}

	for _, tt := range origins {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/v1/messages", nil)
		r.Header.Set("Origin", tt.origin)

		result := ValidateCORS(w, r, policy)

		if result != tt.allowed {
			t.Errorf("Production policy for origin %q: got %v, want %v", tt.origin, result, tt.allowed)
		}
	}
}
