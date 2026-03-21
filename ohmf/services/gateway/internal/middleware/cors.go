package middleware

import (
	"net/http"
	"strings"
)

// CORSPolicy defines the CORS configuration for the gateway.
type CORSPolicy struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	MaxAge           int
	AllowCredentials bool
}

// ValidateCORS validates the Origin header and sets appropriate CORS response headers.
// For Bearer token auth, AllowCredentials must be false to allow wildcard or multiple origins.
func ValidateCORS(w http.ResponseWriter, r *http.Request, policy CORSPolicy) bool {
	origin := r.Header.Get("Origin")

	// Empty origin means request is not from a browser or CORS is not required
	if origin == "" {
		return true
	}

	// Check if origin is allowed
	allowed := isOriginAllowed(origin, policy.AllowedOrigins)
	if !allowed {
		// Origin not in allowlist, reject preflight and don't set CORS headers
		return false
	}

	// Set CORS response headers
	w.Header().Set("Access-Control-Allow-Origin", origin)

	if len(policy.AllowedMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(policy.AllowedMethods, ", "))
	}

	if len(policy.AllowedHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(policy.AllowedHeaders, ", "))
	}

	if len(policy.ExposedHeaders) > 0 {
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(policy.ExposedHeaders, ", "))
	}

	if policy.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", string(rune(policy.MaxAge)))
	}

	if policy.AllowCredentials {
		// Note: When AllowCredentials is true, must specify exact origin, not "*"
		// This is incompatible with Bearer token auth (no cookies needed)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	return true
}

// HandlePreflight handles OPTIONS preflight requests and returns 204 No Content
func HandlePreflight(w http.ResponseWriter, r *http.Request, policy CORSPolicy) {
	if r.Method != http.MethodOptions {
		return
	}

	if !ValidateCORS(w, r, policy) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// isOriginAllowed checks if origin is in the allowlist or matches a pattern
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if allowed == "*" {
			return true
		}
		if allowed == origin {
			return true
		}
		// Support localhost pattern: http://localhost:*
		if strings.Contains(allowed, ":*") {
			pattern := strings.TrimSuffix(allowed, ":*")
			if strings.HasPrefix(origin, pattern+":") {
				return true
			}
		}
	}
	return false
}

// CORSMiddleware returns a middleware function that validates CORS for all requests.
// It handles preflight OPTIONS requests and sets appropriate headers for actual requests.
func CORSMiddleware(policy CORSPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle preflight requests
			if r.Method == http.MethodOptions {
				HandlePreflight(w, r, policy)
				// Preflight requests should terminate here
				if !ValidateCORS(w, r, policy) {
					return
				}
				return
			}

			// For non-preflight requests, validate CORS and set headers
			ValidateCORS(w, r, policy)
			next.ServeHTTP(w, r)
		})
	}
}
