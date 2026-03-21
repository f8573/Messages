#!/usr/bin/env bash
# P3.2 Origin Isolation Integration Test
# Validates that sessions include app_origin and CSP headers

set -e

API_BASE=${OHMF_API_BASE:-http://localhost:18081}
AUTH_TOKEN=${OHMF_AUTH_TOKEN:-}

if [ -z "$AUTH_TOKEN" ]; then
  echo "Error: OHMF_AUTH_TOKEN not set"
  exit 1
fi

echo "Testing P3.2: Isolated Runtime Origins"
echo "======================================="
echo ""

echo "1. Creating session..."
SESSION_RESPONSE=$(curl -s -X POST "$API_BASE/v1/apps/sessions" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "app_id": "app.ohmf.counter",
    "conversation_id": "test-conv",
    "viewer": {"user_id": "test-user", "role": "PLAYER"},
    "participants": [],
    "capabilities_granted": ["conversation.read_context"],
    "state_snapshot": {}
  }')

echo "Session created. Checking response..."
echo ""

echo "2. Checking app_origin in response..."
APP_ORIGIN=$(echo "$SESSION_RESPONSE" | jq -r '.app_origin // empty')
if [ -z "$APP_ORIGIN" ]; then
  echo "❌ FAIL: app_origin not found in response"
  echo "Response: $SESSION_RESPONSE"
  exit 1
fi
echo "✓ PASS: app_origin found: $APP_ORIGIN"
echo ""

echo "3. Validating origin format..."
if [[ $APP_ORIGIN =~ ^[a-f0-9]{8}\.miniapp\.local$ ]]; then
  echo "✓ PASS: Origin format is valid (hash.miniapp.local)"
else
  echo "❌ FAIL: Origin format invalid: $APP_ORIGIN"
  exit 1
fi
echo ""

echo "4. Checking CSP header in response..."
CSP_HEADER=$(echo "$SESSION_RESPONSE" | jq -r '.csp_header // empty')
if [ -z "$CSP_HEADER" ]; then
  echo "❌ FAIL: csp_header not found in response"
  exit 1
fi
echo "✓ PASS: CSP header present"
echo ""

echo "5. Validating CSP directives..."
REQUIRED_DIRECTIVES=(
  "default-src 'none'"
  "script-src 'self'"
  "style-src 'self'"
  "connect-src 'self'"
  "frame-src 'none'"
  "object-src 'none'"
)

for directive in "${REQUIRED_DIRECTIVES[@]}"; do
  if echo "$CSP_HEADER" | grep -q "$directive"; then
    echo "  ✓ Found: $directive"
  else
    echo "  ❌ Missing: $directive"
    exit 1
  fi
done
echo ""

echo "6. Checking launch_context includes app_origin..."
LAUNCH_CONTEXT=$(echo "$SESSION_RESPONSE" | jq '.launch_context // empty')
CONTEXT_ORIGIN=$(echo "$LAUNCH_CONTEXT" | jq -r '.app_origin // empty')
if [ "$CONTEXT_ORIGIN" = "$APP_ORIGIN" ]; then
  echo "✓ PASS: launch_context contains matching app_origin"
else
  echo "❌ FAIL: launch_context app_origin mismatch or missing"
  exit 1
fi
echo ""

echo "7. Checking origin determinism..."
SESSION_RESPONSE_2=$(curl -s -X POST "$API_BASE/v1/apps/sessions" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "app_id": "app.ohmf.counter",
    "conversation_id": "test-conv-2",
    "viewer": {"user_id": "test-user", "role": "PLAYER"},
    "participants": [],
    "capabilities_granted": ["conversation.read_context"],
    "state_snapshot": {}
  }')

APP_ORIGIN_2=$(echo "$SESSION_RESPONSE_2" | jq -r '.app_origin // empty')
if [ "$APP_ORIGIN" = "$APP_ORIGIN_2" ]; then
  echo "✓ PASS: Same app produces deterministic origin (origin determinism verified)"
else
  echo "❌ FAIL: Origins should deterministically match for same app"
  echo "  First:  $APP_ORIGIN"
  echo "  Second: $APP_ORIGIN_2"
  exit 1
fi
echo ""

echo "======================================="
echo "✅ All P3.2 tests passed!"
echo "======================================="
