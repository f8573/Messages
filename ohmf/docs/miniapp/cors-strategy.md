# P3.4: CORS Strategy for Mini-App Platform

## Overview

This document describes the Cross-Origin Resource Sharing (CORS) strategy for the OHMF mini-app platform. The strategy ensures that mini-apps running in iframes can securely communicate with the gateway API using Bearer token authentication, without relying on cookie-based credentials.

## Key Principles

1. **Bearer Token Authentication**: Mini-apps authenticate via `Authorization: Bearer <token>` headers, not cookies
2. **Origin Validation**: All cross-origin requests are validated against an allowlist
3. **No Credentials Mode**: Fetch requests use `credentials: 'omit'` (default), not `'include'`
4. **Preflight Handling**: OPTIONS requests return 204 No Content with appropriate CORS headers
5. **Production Security**: Explicit origin allowlist, no wildcard in production

## Architecture

### Mini-App Request Flow

```
Mini-App iframe (http://localhost:5174/apps/counter)
    |
    | postMessage (bridge protocol)
    |
    v
Runtime Container (http://localhost:5174)
    |
    | fetch with Bearer token
    | Authorization: Bearer <access_token>
    |
    v
Gateway API (http://localhost:18081)
    |
    | CORS validation
    | Origin header check
    |
    v
Protected Endpoint
```

### CORS Validation Logic

1. Extract `Origin` header from request
2. Check against `AllowedOrigins` list from configuration
3. If allowed:
   - Set `Access-Control-Allow-Origin: <origin>`
   - Set `Access-Control-Allow-Methods`
   - Set `Access-Control-Allow-Headers` (including `Authorization`)
   - Return response with appropriate headers
4. If not allowed:
   - Reject request, do not set CORS headers
   - Return 403 Forbidden for OPTIONS preflight

## Configuration

### Gateway CORS Policy

```go
policy := CORSPolicy{
  AllowedOrigins:   []string{
    "http://localhost:3000",      // Local development
    "http://localhost:5174",      // Mini-app runtime
    "http://127.0.0.1:*",         // Localhost pattern
  },
  AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
  AllowedHeaders:   []string{"Authorization", "Content-Type", "X-CSRF-Token"},
  ExposedHeaders:   []string{"X-Request-ID", "Link"},
  MaxAge:           3600,         // 1 hour
  AllowCredentials: false,         // Bearer tokens, not cookies
}
```

### Environment Variables

```bash
# Development
OHMF_ALLOWED_ORIGINS="http://localhost:3000,http://localhost:5174"

# Production
OHMF_ALLOWED_ORIGINS="https://app.example.com,https://miniapps.example.com"
```

## Bearer Token Auth Pattern

### Mini-App Making Request via Runtime

```javascript
// Runtime container (miniapp-runtime.js)
async function gatewayRequest(path, options = {}) {
  const auth = loadRuntimeAuth();
  if (!auth?.accessToken) {
    throw new Error("No auth session available");
  }

  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${auth.accessToken}`);

  // NOTE: No credentials mode specified (defaults to 'omit')
  const response = await fetch(`${apiBaseUrl}${path}`, {
    method: options.method || "GET",
    headers,
    body: options.body ? JSON.stringify(options.body) : undefined,
  });

  return response.json();
}
```

**Key Properties**:
- Bearer token passed in `Authorization` header
- No `credentials: 'include'` needed (default is `omit`)
- CORS headers automatically applied by browser
- Server validates token in protected middleware

### Mini-App Preflight Sequence

```
1. Browser sends OPTIONS preflight:
   OPTIONS /v1/messages
   Origin: http://localhost:5174
   Access-Control-Request-Method: POST
   Access-Control-Request-Headers: authorization, content-type

2. Gateway CORS middleware validates:
   - Origin in AllowedOrigins? YES
   - Methods allowed? YES
   - Headers allowed? YES (Authorization in list)

3. Gateway returns 204 No Content:
   Access-Control-Allow-Origin: http://localhost:5174
   Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
   Access-Control-Allow-Headers: Authorization, Content-Type, X-CSRF-Token
   Access-Control-Max-Age: 3600

4. Browser sends actual POST request with token:
   POST /v1/messages
   Authorization: Bearer token_abc123
   Content-Type: application/json
```

## Files Modified

### 1. Gateway Middleware: `internal/middleware/cors.go`

Provides:
- `CORSPolicy` struct with configuration
- `ValidateCORS()` function to validate Origin and set response headers
- `HandlePreflight()` to handle OPTIONS requests
- `CORSMiddleware()` to wrap handlers

### 2. Gateway API: `cmd/api/main.go`

Changes:
- Import new CORS middleware
- Load CORS policy from configuration
- Register middleware before route handlers
- Remove old go-chi/cors dependency (optional)

### 3. Configuration: `internal/config/config.go`

Adds:
- `AllowedOrigins` field from environment
- Parse comma-separated origin list
- Support for localhost patterns

### 4. Mini-App Runtime: Verified no credentials mode

Files checked:
- `apps/web/miniapp-runtime.js`: Uses Bearer tokens ✓
- `apps/web/miniapps/counter/app.js`: Uses bridge protocol (postMessage) ✓
- `apps/web/miniapps/eightball/app.js`: Uses bridge protocol (postMessage) ✓

No changes needed - already uses correct pattern.

## Threat Model & Mitigations

### Threat: Cross-Site Request Forgery (CSRF)

**Risk**: Malicious site could trick browser into sending requests to gateway

**Mitigation**:
- Origin validation header prevents unauthorized cross-origin requests
- Bearer tokens don't use cookies (no automatic inclusion)
- Each request requires explicit token in Authorization header
- Preflight checks catch unauthorized requests early

### Threat: Token Leakage via CORS

**Risk**: Overly permissive CORS could expose tokens to unauthorized origins

**Mitigation**:
- Strict allowlist of origins (no wildcards in production)
- Localhost patterns only for development (`http://localhost:*`)
- `AllowCredentials: false` prevents credential inclusion in cross-origin requests
- Tokens have limited TTL via JWT expiration

### Threat: Origin Header Spoofing

**Risk**: Attacker modifies Origin header to impersonate legitimate origin

**Mitigation**:
- Origin header validated by browser (cannot be spoofed from JavaScript)
- Server-side CORS policy enforces strict checks
- Preflight requests require valid origin before actual request sent

### Threat: Sub-Domain Takeover

**Risk**: Attacker controls `subdomain.example.com` which passes CORS check

**Mitigation**:
- Use specific subdomain allowlist, not parent domain
- Example: Allow `https://miniapps.example.com` but not `https://*.example.com`
- Monitor DNS and certificate management

## Testing Checklist

- [x] Unit tests: ValidateCORS function (10+ tests)
- [x] Preflight response: 204 No Content with headers
- [x] Authorized origin: Headers set correctly
- [x] Unauthorized origin: Rejected, no headers
- [x] Bearer token validation: Works across CORS boundary
- [x] Production config: Multiple specific origins
- [x] Localhost pattern: Allows multiple local ports
- [x] Empty origin: Non-CORS requests pass through
- [ ] Integration test: End-to-end mini-app request
- [ ] Security test: Verify token not leaked in headers

## Deployment Guide

### Local Development

```bash
export OHMF_ALLOWED_ORIGINS="http://localhost:3000,http://localhost:5174,http://127.0.0.1:*"
go run ./cmd/api
```

### Staging

```bash
export OHMF_ALLOWED_ORIGINS="https://staging-app.example.com,https://staging-miniapps.example.com"
go build ./cmd/api
./api
```

### Production

```bash
export OHMF_ALLOWED_ORIGINS="https://app.example.com,https://miniapps.example.com"
go build ./cmd/api
./api
```

## Security Audit Checklist

- [ ] All mini-app fetch calls verified to NOT use `credentials: 'include'`
- [ ] Bearer token auth enforced in all protected endpoints
- [ ] CORS middleware runs before all routes
- [ ] AllowCredentials set to false in production
- [ ] Origin allowlist reviewed and hardened
- [ ] Preflight responses validated in browser DevTools
- [ ] Token expiration tested across CORS boundaries
- [ ] No secrets in CORS headers or exposed headers
- [ ] CDN/S3 CORS configurations aligned with gateway policy
- [ ] Monitoring: Log CORS rejections and failed preflight requests

## Related Documents

- [CDN CORS Configuration](./CDN_CORS_CONFIG.md)
- [S3 CORS Configuration](./S3_CORS_CONFIG.md)
- [Mini-App SDK Documentation](../miniapp/SDK.md)
- [API Authentication Guide](../API_AUTH.md)

## References

- [MDN: CORS](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS)
- [MDN: Authorization Header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Authorization)
- [OWASP: CORS](https://owasp.org/www-community/attacks/attack_summary_cross_site_request_forgery)
- [TC39: fetch() credentials mode](https://fetch.spec.whatwg.org/#credentials-mode)
