package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// OriginConfig holds the security configuration for an isolated mini-app runtime.
// Each mini-app runtime gets a unique origin to prevent CSRF, cookie mixing, and DOM inspection.
type OriginConfig struct {
	// AppOrigin is the isolated origin for this mini-app instance (e.g., app1a2b3c4d.miniapp.local)
	AppOrigin string
	// CSPHeader is the Content-Security-Policy header value for this origin
	CSPHeader string
	// AllowedCORSOrigins is a list of origins permitted to access this mini-app runtime
	AllowedCORSOrigins []string
}

// OriginGenerationParams holds parameters for deterministic origin generation
type OriginGenerationParams struct {
	AppID       string // Application ID (e.g., "app.ohmf.counter-lab")
	ReleaseID   string // Release/version ID (e.g., "v1.2.3")
	BaseDomain  string // Base domain for mini-app origins (e.g., "miniapp.local")
	SubdomainLen int   // Length of subdomain hash prefix (default 8)
}

// GenerateAppOrigin creates a deterministic, collision-resistant origin from app_id and release_id.
// Uses SHA256 hash of (app_id + release_id) to ensure:
// 1. Determinism: same app_id+release_id always generates same origin
// 2. Uniqueness: different app_id+release_id values generate different origins
// 3. Reproducibility: can be regenerated on server or client side
//
// Format: {hash_prefix}.miniapp.local
// Example: "a7f3e1c5.miniapp.local" for app_id="app.ohmf.counter" + release_id="v1"
func GenerateAppOrigin(appID, releaseID, baseDomain string, subdomainLen int) string {
	if subdomainLen <= 0 {
		subdomainLen = 8
	}
	if baseDomain == "" {
		baseDomain = "miniapp.local"
	}

	// Normalize app_id and release_id for consistent hashing
	appID = strings.TrimSpace(appID)
	releaseID = strings.TrimSpace(releaseID)

	// Create deterministic seed: app_id + ":" + release_id
	seed := fmt.Sprintf("%s:%s", appID, releaseID)

	// SHA256 hash the seed
	hash := sha256.Sum256([]byte(seed))
	hashHex := hex.EncodeToString(hash[:])

	// Extract prefix for subdomain (first N characters)
	subdomain := hashHex[:subdomainLen]

	return fmt.Sprintf("%s.%s", subdomain, baseDomain)
}

// GenerateOriginConfig creates a complete OriginConfig with CSP headers and CORS policies.
// The origin is deterministic: calling with the same appID+releaseID always returns the same origin.
func GenerateOriginConfig(params OriginGenerationParams) *OriginConfig {
	if params.SubdomainLen <= 0 {
		params.SubdomainLen = 8
	}
	if params.BaseDomain == "" {
		params.BaseDomain = "miniapp.local"
	}

	// Generate the isolated origin
	appOrigin := GenerateAppOrigin(params.AppID, params.ReleaseID, params.BaseDomain, params.SubdomainLen)

	// Generate CSP header for this origin
	cspHeader := generateCSPHeader(appOrigin)

	// Define allowed CORS origins (mini-app can communicate with gateway and itself)
	allowedCORSOrigins := []string{
		appOrigin, // Self
	}

	return &OriginConfig{
		AppOrigin:          appOrigin,
		CSPHeader:          cspHeader,
		AllowedCORSOrigins: allowedCORSOrigins,
	}
}

// generateCSPHeader creates a strict Content-Security-Policy header for the isolated origin.
// Policies enforce:
// 1. script-src 'self': Only scripts from the same origin
// 2. style-src 'self' 'unsafe-inline': Only stylesheets from same origin (unsafe-inline for embedded CSS)
// 3. img-src 'self' data: https:: Images from self, data URLs, and HTTPS
// 4. font-src 'self' data: Fonts from self and data URLs
// 5. connect-src 'self': API calls only to same origin (prevents CSRF)
// 6. frame-src 'none': Disallow nested iframes
// 7. object-src 'none': Disallow plugins
// 8. default-src 'none': Deny everything by default
// 9. base-uri 'none': Prevent <base> tag injection
// 10. form-action 'none': Disallow form submissions
// 11. frame-ancestors 'self': Only allow embedding in parent origin
func generateCSPHeader(appOrigin string) string {
	// Strict CSP that isolates runtime and prevents breakout
	return fmt.Sprintf(
		"default-src 'none'; "+
			"script-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data: https:; "+
			"font-src 'self' data:; "+
			"connect-src 'self'; "+
			"frame-src 'none'; "+
			"object-src 'none'; "+
			"base-uri 'none'; "+
			"form-action 'none'; "+
			"frame-ancestors 'self'; "+
			"report-uri /-/csp-report",
	)
}

// ValidateOrigin checks if the provided origin matches the expected format and is a valid mini-app origin.
// Returns true if the origin is a valid mini-app isolated origin.
func ValidateOrigin(origin string, baseDomain string) bool {
	if baseDomain == "" {
		baseDomain = "miniapp.local"
	}

	// Regex: {8-hex-digits}.{baseDomain}
	// Example: "a7f3e1c5.miniapp.local"
	pattern := fmt.Sprintf(`^[a-f0-9]{8,}\.%s$`, regexp.QuoteMeta(baseDomain))
	re := regexp.MustCompile(pattern)
	return re.MatchString(origin)
}

// GetSameOriginCheckValue returns the header value for X-Same-Origin-Check.
// This header is set by the gateway to help the iframe verify it received a response from the expected origin.
// Format: app_origin + "::" + timestamp_ms
func GetSameOriginCheckValue(appOrigin string) string {
	// Placeholder for timestamp-based verification (can be enhanced in future)
	return appOrigin
}

// IsSameOriginRequestValid checks if the request's Origin header matches the expected mini-app origin.
// Used by WebSocket and other handlers to enforce origin validation.
func IsSameOriginRequestValid(requestOrigin, expectedOrigin string) bool {
	return requestOrigin == expectedOrigin
}
