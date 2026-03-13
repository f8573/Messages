# OHMF Web (Barebones)

This is a barebones Google Messages-style web interface wired to the OHMF gateway API.

It also includes a standalone mini-app runtime test page for the embedded app API.

## Run

From `apps/web`:

```powershell
py -m http.server 5173
```

Then open:

- `http://localhost:5173`
- `http://localhost:5173/miniapp-runtime.html`

Gateway API should be running at:

- `http://localhost:18080`

## Current Scope

- Two-pane layout:
  - Conversation list
  - Active thread
- Responsive mobile behavior (thread-only view with back button)
- Login via phone OTP:
  - `POST /v1/auth/phone/start`
  - `POST /v1/auth/phone/verify`
- Token refresh and logout:
  - `POST /v1/auth/refresh`
  - `POST /v1/auth/logout`
- Conversation loading:
  - `GET /v1/conversations`
  - `GET /v1/conversations/{id}/messages`
- New message and sent indicators:
  - SMS delivery statuses: `SENT`, `FAIL_SEND`
  - OHMF delivery statuses: `SENT`, `DELIVERED`, `READ`, `FAIL_DELIVERY`, `FAIL_SEND`
  - OHMF outgoing messages show an iMessage-style delivery indicator label
- Start new message to phone number:
  - Creates a local draft thread from phone input
  - First send uses `POST /v1/messages/phone`
  - Conversation creation/selection handled automatically from API response
- Conversation store:
  - Per-user conversation state cached in `localStorage`
  - Auth session stored in `sessionStorage`
- Mini-app runtime lab:
  - Loads `./miniapps/counter/manifest.json`
  - Runs the app in a sandboxed iframe
  - Exposes a minimal bridge with `host.getLaunchContext`, `conversation.readContext`, `conversation.sendMessage`, `storage.session.get`, `storage.session.set`, and `session.updateState`
  - Lets you toggle granted permissions to verify host-side capability enforcement

## Security And Dev Guardrails

- No `innerHTML` rendering for user/message text; DOM is built with element APIs and `textContent`.
- Client-side message input is normalized to strip control characters and bounded to 1000 chars.
- `index.html` sets a restrictive CSP and related browser policies:
  - `default-src 'self'`
  - no inline/eval script execution (`script-src 'self'`)
  - `object-src 'none'`, `base-uri 'none'`, `frame-ancestors 'none'`
  - tight `connect-src`, `img-src`, `font-src` defaults (includes localhost API)
- No secrets or API keys are stored in frontend code.
- Third-party hosted assets are avoided by default in this baseline.
- No DOM HTML injection sinks (`innerHTML`, `outerHTML`, `insertAdjacentHTML`) are used.

## Not Yet Wired

- Realtime updates
- Contact names/profile resolution
- Media attachments and rich content types
- Backend-backed mini-app session persistence through the gateway mini-app endpoints
