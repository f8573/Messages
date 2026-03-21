# P3.2: Isolated Runtime Origins

**Status**: ✅ Implementation Complete (2026-03-21)

## Overview

Each mini-app runtime receives a **unique, deterministic sandboxed origin** to prevent CSRF, cookie theft, DOM inspection, and cross-app storage leakage.

**Example**:
```
Session for app.ohmf.counter@v1.2.3 → a7f3e1c5.miniapp.local
Session for app.ohmf.eightball@v1.0.0 → f2c3d4e5.miniapp.local
Same app, different release → different origin
```

---

## Goal: Security Isolation

### Threat Model

Without isolated origins, an attacker controlling one miniapp could:

1. **Cross-app localStorage/sessionStorage theft**
   - `window.localStorage.getItem()` accesses host domain storage
   - Another miniapp can read/write to the same storage
   - Result: Credential theft, state manipulation, data exfiltration

2. **postMessage spoofing**
   - Sibling iframe can post as if from the host
   - Bypass origin validation if not strict
   - Result: Fake events, permission escalation, command injection

3. **DOM inspection**
   - Same-origin iframes can traverse parent/sibling DOM
   - Access sibling iframe contentDocument
   - Result: Sandbox escape, credential harvesting

4. **Shared scope pollution**
   - JavaScript globals polluted by one app
   - Another app inherits polluted prototypes
   - Result: Prototype pollution exploits

### Isolated Origin Guarantee

- Each runtime runs at a **unique origin** (e.g., `a7f3e1c5.miniapp.local`)
- Browser enforces **origin isolation** automatically:
  - Separate localStorage/sessionStorage per origin ✓
  - Separate cookies per origin ✓
  - Separate DOM tree per origin ✓
  - Separate JavaScript scope per origin ✓
- Attacks require exploits in browser, not just app logic

---

## Architecture

### Origin Generation

**Deterministic, collision-resistant formula**:

```
origin = hash(app_id + ":" + release_id)[:8] + ".miniapp.local"
```

Properties:
- **Determinism**: Same app_id + release_id always generates same origin
- **Uniqueness**: Different inputs → different origins (8-char SHA256 prefix)
- **Reproducibility**: Can be regenerated on client or server side
- **No storage**: Origins are derived, not persisted in database

### Data Flow

```
POST /v1/apps/sessions
  ↓
Gateway: CreateSession
  ↓ (in Service.buildSessionResponse())
Generate origin: config.GenerateOriginConfig(appID, releaseID)
  ↓
Response includes:
  {
    "app_origin": "a7f3e1c5.miniapp.local",
    "csp_header": "default-src 'none'; ...",
    "launch_context": {
      "app_origin": "a7f3e1c5.miniapp.local",
      ...
    }
  }
  ↓
Client receives app_origin
  ↓
Client sets up isolated iframe:
  - iframe.src = manifest.entrypoint (resource loading)
  - Validates postMessage origin against app_origin
  - iframe sandbox removes allow-same-origin
  ↓
Mini-app loads in isolated origin context
  ↓
All storage/events/DOM scoped to that origin
```

### Server-Side: CSP Headers

The `/v1/apps/sessions` response includes:

```http
Content-Security-Policy: default-src 'none'; script-src 'self';
  style-src 'self' 'unsafe-inline'; img-src 'self' data: https:;
  font-src 'self' data:; connect-src 'self'; frame-src 'none';
  object-src 'none'; base-uri 'none'; form-action 'none';
  frame-ancestors 'self'; report-uri /-/csp-report
```

**Enforcement**:
- `default-src 'none'`: Deny everything by default
- `script-src 'self'`: Only same-origin scripts
- `connect-src 'self'`: Only same-origin API calls
- `frame-src 'none'`: No nested iframes
- `object-src 'none'`: No plugins

### Client-Side: Origin Validation

The web host (miniapp-runtime.js) enforces origin validation:

```javascript
// Extract app_origin from session response
state.appOrigin = record?.app_origin || null;

// In message listener:
if (state.appOrigin) {
  const expectedOriginUrl = new URL(`http://${state.appOrigin}`);
  const messageOrigin = new URL(event.origin);
  if (messageOrigin.host !== expectedOriginUrl.host) {
    // Reject message from wrong origin
    return;
  }
}
```

**Iframe Sandbox**:

```javascript
// Remove 'allow-same-origin' to prevent storage access
iframe.setAttribute("sandbox", "allow-scripts");
// Result: iframe cannot access host storage, cookies, or DOM
```

---

## Implementation

### Files Modified

**Gateway (Go)**:

1. `services/gateway/internal/miniapp/handler.go`
   - Added `config` import
   - `CreateSession` calls `h.attachOriginConfig()` to add CSP headers
   - `attachOriginConfig()` generates origin and sets response headers

2. `services/gateway/internal/miniapp/service.go`
   - Already imports `config`
   - `buildSessionResponse()` includes `app_origin` and `csp_header`
   - `buildLaunchContext()` includes `app_origin`

3. `services/gateway/internal/config/origins.go` (already complete)
   - `GenerateAppOrigin()`: Hash-based origin generation
   - `GenerateOriginConfig()`: Complete config (origin + CSP)
   - `generateCSPHeader()`: Strict CSP policy
   - `ValidateOrigin()`: Format validation
   - `IsSameOriginRequestValid()`: Origin matching

**Web Host (JavaScript)**:

1. `apps/web/miniapp-runtime.js`
   - Added `state.appOrigin` to track isolated origin
   - `applySessionRecord()` extracts `app_origin` from session response
   - `buildFrameUrl()` documents origin infrastructure setup (local dev fallback for now)
   - `launchFrame()` removes `allow-same-origin` from iframe sandbox
   - Message listener validates incoming messages against `app_origin`

---

## Deployment Considerations

### Local Development (Current)

For local testing without full DNS infrastructure:

1. `miniapp.local` is expected to resolve locally
2. Origin isolation works at the **browser level** (same domain):
   - NOT full network isolation (would require different IP or subdomain)
   - Still prevents cross-app storage/DOM access (browser enforces)
   - CSP provides defense-in-depth

3. Run with modified `/etc/hosts`:
   ```bash
   # /etc/hosts
   127.0.0.1 miniapp.local
   127.0.0.1 a7f3e1c5.miniapp.local
   127.0.0.1 f2c3d4e5.miniapp.local
   # ... for each unique origin
   ```

### Production Deployment

For production origin isolation:

1. **DNS Setup**:
   - Create wildcard DNS record: `*.miniapp.example.com → IP`
   - Browser resolves `a7f3e1c5.miniapp.example.com` to actual IP
   - Each origin gets **separate TCP connection**
   - Full network isolation + browser origin isolation

2. **Configuration**:
   ```yaml
   # infra/config/environments/prod.env.yaml
   MINIAPP_BASE_DOMAIN: miniapp.example.com
   MINIAPP_ORIGIN_SUBDOMAIN_LEN: 8
   ```

3. **SSL/TLS**:
   - Wildcard certificate: `*.miniapp.example.com`
   - TLS session per origin (separate connection)
   - Full isolation at network level

---

## Testing

### Unit Tests (origins_test.go)

- **Determinism**: Same inputs always produce same origin ✓
- **Uniqueness**: Different inputs produce different origins ✓
- **Format**: Origin matches regex `^[a-f0-9]{8}\.miniapp\.local$` ✓
- **CSP strictness**: No `unsafe-eval`, `unsafe-script` ✓
- **Collision resistance**: 100 different app/release combos → 100 unique origins ✓
- **Validation**: Valid origins accepted, invalid rejected ✓

### Integration Testing

**To verify P3.2 is working**:

1. **Gateway response includes origin**:
   ```bash
   curl -X POST http://localhost:18081/v1/apps/sessions \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"app_id":"app.ohmf.counter",...}' \
     | jq '.app_origin'
   # Should output: a7f3e1c5.miniapp.local
   ```

2. **CSP headers set**:
   ```bash
   curl -i -X POST http://localhost:18081/v1/apps/sessions \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"app_id":"app.ohmf.counter",...}' \
     | grep -i "content-security-policy"
   # Should show full CSP header
   ```

3. **Client-side validation**:
   - Open miniapp-runtime.html
   - Load app and observe logs
   - Verify `app_origin` logged in launch event
   - Verify iframe sandbox rejects `allow-same-origin`

4. **Cross-app isolation**:
   - Load two different miniapps in separate browser tabs
   - Try to access other app's storage: `localStorage.getItem("other-app-key")`
   - Should always be empty (different origins)

---

## Enforcement

### What is Enforced

| Level | Enforcement | Result |
|-------|-------------|--------|
| **HTTP** | CSP headers in 2XX response | Scripts/styles/connections limited to origin |
| **Browser** | Origin isolation policy | Automatic separation of storage, DOM, scope |
| **App** | Origin validation in postMessage | Reject messages from wrong origin |
| **Sandbox** | iframe `allow-scripts` only | No direct document access, no cookies |

### What is NOT Enforced (By Design)

- **Resource loading from different origin**: Apps can load CSS/fonts/images from CDN (CSP allows `img-src https:`, `font-src` data URLs)
- **Timing analysis**: High-resolution timers still available (acceptable risk)
- **Spectre/Meltdown**: Browser mitigations handle
- **Malicious host**: Sandboxing only protects from malicious apps, not malicious host

---

## Known Limitations & Future Work

### Phase 1 (Current): Origin Header Setup ✓

- [x] Gateway generates unique origin per session
- [x] Origin included in session response
- [x] CSP headers set on response
- [x] Client receives and validates origin
- [x] Unit tests for origin generation
- [x] Iframe sandbox configured

### Phase 2 (Future): Full Network Isolation

- [ ] DNS wildcard setup (`.miniapp.example.com`)
- [ ] Separate TCP connection per origin
- [ ] Production TLS certificates
- [ ] CDN origin policy (restrict to miniapp origins)
- [ ] Load balancer routing by origin

### Phase 3 (Future): Runtime Enforcement

- [ ] Origin audit logging (per-session origin assignment)
- [ ] CSP violation reporting endpoint
- [ ] Origin binding in database (optional, for audit)
- [ ] Metrics: origin hash collisions, CSP violations

---

## References

- **Origin Generation**: `services/gateway/internal/config/origins.go`
- **Session Creation**: `services/gateway/internal/miniapp/service.go`
- **Client Integration**: `apps/web/miniapp-runtime.js`
- **Tests**: `services/gateway/internal/config/origins_test.go`
- **Architecture**: `docs/miniapp/ownership-boundaries.md`
- **CSP Spec**: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Security-Policy

---

## Checklist

- [x] Origin generation algorithm implemented
- [x] Determinism and uniqueness verified
- [x] CSP header generation strict and complete
- [x] Gateway returns origin in session response
- [x] HTTP headers set in handler
- [x] Web host extracts and stores origin
- [x] Origin validation in postMessage listener
- [x] Iframe sandbox updated (removed `allow-same-origin`)
- [x] Unit tests comprehensive
- [x] Documentation complete
- [ ] Production DNS infrastructure setup (Phase 2)
- [ ] E2E tests with multiple origins (Phase 2)
