package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMVPFlow(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	waitForHealth(t, baseURL)

	aStart := postJSON(t, baseURL+"/v1/auth/phone/start", map[string]any{"phone_e164": "+1555" + runID[len(runID)-6:] + "01", "channel": "SMS"}, "")
	bStart := postJSON(t, baseURL+"/v1/auth/phone/start", map[string]any{"phone_e164": "+1555" + runID[len(runID)-6:] + "02", "channel": "SMS"}, "")

	aVerify := postJSON(t, baseURL+"/v1/auth/phone/verify", map[string]any{
		"challenge_id": aStart["challenge_id"],
		"otp_code":     "123456",
		"device": map[string]any{
			"platform":    "WEB",
			"device_name": "Chrome",
		},
	}, "")
	bVerify := postJSON(t, baseURL+"/v1/auth/phone/verify", map[string]any{
		"challenge_id": bStart["challenge_id"],
		"otp_code":     "123456",
		"device": map[string]any{
			"platform":    "WEB",
			"device_name": "Firefox",
		},
	}, "")

	aTokens := mustMap(t, aVerify["tokens"])
	aUser := mustMap(t, aVerify["user"])
	bUser := mustMap(t, bVerify["user"])
	aToken := mustString(t, aTokens["access_token"])
	bUserID := mustString(t, bUser["user_id"])

	conv := postJSON(t, baseURL+"/v1/conversations", map[string]any{
		"type":         "DM",
		"participants": []string{bUserID},
	}, aToken)
	convID := mustString(t, conv["conversation_id"])

	msgReq := map[string]any{
		"conversation_id": convID,
		"idempotency_key": "idem-int-" + runID,
		"content_type":    "text",
		"content":         map[string]any{"text": "hello"},
	}
	msg1 := postJSON(t, baseURL+"/v1/messages", msgReq, aToken)
	msg2 := postJSON(t, baseURL+"/v1/messages", msgReq, aToken)

	if mustString(t, msg1["message_id"]) != mustString(t, msg2["message_id"]) {
		t.Fatalf("expected idempotent message_id")
	}
	if mustFloat64(t, msg1["server_order"]) != mustFloat64(t, msg2["server_order"]) {
		t.Fatalf("expected idempotent server_order")
	}

	list := getJSON(t, baseURL+"/v1/conversations/"+convID+"/messages", aToken)
	items := mustSlice(t, list["items"])
	if len(items) != 1 {
		t.Fatalf("expected 1 message, got %d", len(items))
	}

	status := postStatus(t, baseURL+"/v1/conversations/"+convID+"/read", map[string]any{"through_server_order": 1}, aToken)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204 on mark-read, got %d", status)
	}

	if mustString(t, aUser["user_id"]) == "" {
		t.Fatalf("expected non-empty user id")
	}
}

func TestRefreshAndLogout(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	waitForHealth(t, baseURL)

	start := postJSON(t, baseURL+"/v1/auth/phone/start", map[string]any{"phone_e164": "+1555" + runID[len(runID)-6:] + "03", "channel": "SMS"}, "")
	verify := postJSON(t, baseURL+"/v1/auth/phone/verify", map[string]any{
		"challenge_id": start["challenge_id"],
		"otp_code":     "123456",
		"device": map[string]any{
			"platform":    "WEB",
			"device_name": "Edge",
		},
	}, "")

	tokens := mustMap(t, verify["tokens"])
	device := mustMap(t, verify["device"])
	access := mustString(t, tokens["access_token"])
	refresh := mustString(t, tokens["refresh_token"])
	deviceID := mustString(t, device["device_id"])

	refreshResp := postJSON(t, baseURL+"/v1/auth/refresh", map[string]any{"refresh_token": refresh}, "")
	refreshTokens := mustMap(t, refreshResp["tokens"])
	if mustString(t, refreshTokens["access_token"]) == "" || mustString(t, refreshTokens["refresh_token"]) == "" {
		t.Fatalf("expected rotated tokens from refresh")
	}

	status := postStatus(t, baseURL+"/v1/auth/logout", map[string]any{"device_id": deviceID}, access)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204 on logout, got %d", status)
	}
}

func TestInvalidOTP(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	waitForHealth(t, baseURL)

	start := postJSON(t, baseURL+"/v1/auth/phone/start", map[string]any{"phone_e164": "+1555" + runID[len(runID)-6:] + "11", "channel": "SMS"}, "")
	status, body := postJSONWithStatus(t, baseURL+"/v1/auth/phone/verify", map[string]any{
		"challenge_id": start["challenge_id"],
		"otp_code":     "000000",
		"device": map[string]any{
			"platform":    "WEB",
			"device_name": "BadOTP",
		},
	}, "")

	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid otp, got %d", status)
	}
	if mustString(t, body["code"]) != "invalid_otp" {
		t.Fatalf("expected invalid_otp code, got %v", body["code"])
	}
}

func TestRateLimitOTPStart(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	waitForHealth(t, baseURL)
	phone := "+1555" + runID[len(runID)-6:] + "12"

	for i := 0; i < 5; i++ {
		postJSON(t, baseURL+"/v1/auth/phone/start", map[string]any{"phone_e164": phone, "channel": "SMS"}, "")
	}
	status, body := postJSONWithStatus(t, baseURL+"/v1/auth/phone/start", map[string]any{"phone_e164": phone, "channel": "SMS"}, "")
	if status != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after otp start limit, got %d", status)
	}
	if mustString(t, body["code"]) != "rate_limited" {
		t.Fatalf("expected rate_limited code, got %v", body["code"])
	}
}

func TestUnauthorizedProtectedRoutes(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	waitForHealth(t, baseURL)

	status := postStatus(t, baseURL+"/v1/conversations", map[string]any{"type": "DM", "participants": []string{}}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for conversations without token, got %d", status)
	}
}

func TestForbiddenConversationAccess(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	waitForHealth(t, baseURL)

	a := verifyUser(t, baseURL, "+1555"+runID[len(runID)-6:]+"21", "A")
	b := verifyUser(t, baseURL, "+1555"+runID[len(runID)-6:]+"22", "B")
	c := verifyUser(t, baseURL, "+1555"+runID[len(runID)-6:]+"23", "C")

	conv := postJSON(t, baseURL+"/v1/conversations", map[string]any{
		"type":         "DM",
		"participants": []string{mustString(t, b["user_id"])},
	}, mustString(t, a["access_token"]))
	convID := mustString(t, conv["conversation_id"])

	status, body := postJSONWithStatus(t, baseURL+"/v1/messages", map[string]any{
		"conversation_id": convID,
		"idempotency_key": "idem-forbidden-" + runID,
		"content_type":    "text",
		"content":         map[string]any{"text": "should fail"},
	}, mustString(t, c["access_token"]))
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member send, got %d", status)
	}
	if mustString(t, body["code"]) != "forbidden" {
		t.Fatalf("expected forbidden code, got %v", body["code"])
	}
}

func TestInvalidRefreshToken(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	waitForHealth(t, baseURL)

	status, body := postJSONWithStatus(t, baseURL+"/v1/auth/refresh", map[string]any{"refresh_token": "definitely-invalid"}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid refresh token, got %d", status)
	}
	if mustString(t, body["code"]) != "invalid_refresh" {
		t.Fatalf("expected invalid_refresh code, got %v", body["code"])
	}
}

func TestTwoWayMessaging(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	waitForHealth(t, baseURL)

	a := verifyUser(t, baseURL, "+1555"+runID[len(runID)-6:]+"31", "A2")
	b := verifyUser(t, baseURL, "+1555"+runID[len(runID)-6:]+"32", "B2")

	conv := postJSON(t, baseURL+"/v1/conversations", map[string]any{
		"type":         "DM",
		"participants": []string{mustString(t, b["user_id"])},
	}, mustString(t, a["access_token"]))
	convID := mustString(t, conv["conversation_id"])

	postJSON(t, baseURL+"/v1/messages", map[string]any{
		"conversation_id": convID,
		"idempotency_key": "idem-a-" + runID,
		"content_type":    "text",
		"content":         map[string]any{"text": "hi from A"},
	}, mustString(t, a["access_token"]))

	postJSON(t, baseURL+"/v1/messages", map[string]any{
		"conversation_id": convID,
		"idempotency_key": "idem-b-" + runID,
		"content_type":    "text",
		"content":         map[string]any{"text": "hi from B"},
	}, mustString(t, b["access_token"]))

	aList := getJSON(t, baseURL+"/v1/conversations/"+convID+"/messages", mustString(t, a["access_token"]))
	bList := getJSON(t, baseURL+"/v1/conversations/"+convID+"/messages", mustString(t, b["access_token"]))

	aItems := mustSlice(t, aList["items"])
	bItems := mustSlice(t, bList["items"])
	if len(aItems) != 2 || len(bItems) != 2 {
		t.Fatalf("expected both users to see 2 messages, got a=%d b=%d", len(aItems), len(bItems))
	}

	first := mustMap(t, aItems[0])
	second := mustMap(t, aItems[1])
	if mustFloat64(t, first["server_order"]) != 1 || mustFloat64(t, second["server_order"]) != 2 {
		t.Fatalf("expected ordered messages with server_order 1 then 2")
	}
}

func TestMessageNonRegisteredPhone(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	waitForHealth(t, baseURL)

	a := verifyUser(t, baseURL, "+1555"+runID[len(runID)-6:]+"41", "A3")
	phone := "+1555" + runID[len(runID)-6:] + "99"

	resp := postJSON(t, baseURL+"/v1/messages/phone", map[string]any{
		"phone_e164":      phone,
		"idempotency_key": "idem-phone-" + runID,
		"content_type":    "text",
		"content":         map[string]any{"text": "hello non-registered"},
	}, mustString(t, a["access_token"]))

	convID := mustString(t, resp["conversation_id"])
	if convID == "" {
		t.Fatalf("expected conversation_id for phone message")
	}

	conv := getJSON(t, baseURL+"/v1/conversations/"+convID, mustString(t, a["access_token"]))
	phones := mustSlice(t, conv["external_phones"])
	if len(phones) != 1 || mustString(t, phones[0]) != phone {
		t.Fatalf("expected external phone participant %s, got %v", phone, phones)
	}

	msgs := getJSON(t, baseURL+"/v1/conversations/"+convID+"/messages", mustString(t, a["access_token"]))
	items := mustSlice(t, msgs["items"])
	if len(items) != 1 {
		t.Fatalf("expected 1 message in phone conversation, got %d", len(items))
	}
}

func TestObservabilityEndpoints(t *testing.T) {
	baseURL := requireIntegrationEnv(t)
	waitForHealth(t, baseURL)

	client := &http.Client{Timeout: 5 * time.Second}

	readyReq, _ := http.NewRequest(http.MethodGet, baseURL+"/readyz", nil)
	readyResp, err := client.Do(readyReq)
	if err != nil {
		t.Fatalf("readyz request failed: %v", err)
	}
	defer readyResp.Body.Close()
	if readyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from readyz, got %d", readyResp.StatusCode)
	}

	metricsReq, _ := http.NewRequest(http.MethodGet, baseURL+"/metrics", nil)
	metricsResp, err := client.Do(metricsReq)
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer metricsResp.Body.Close()
	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from metrics, got %d", metricsResp.StatusCode)
	}
	body, err := io.ReadAll(metricsResp.Body)
	if err != nil {
		t.Fatalf("reading metrics failed: %v", err)
	}
	if !strings.Contains(string(body), "ohmf_gateway_http_requests_total") {
		t.Fatalf("expected gateway http metrics in response body")
	}
}

func waitForHealth(t *testing.T, baseURL string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/healthz", nil)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("service not healthy at %s", baseURL)
}

func requireIntegrationEnv(t *testing.T) string {
	t.Helper()
	if os.Getenv("OHMF_RUN_INTEGRATION") != "1" {
		t.Skip("set OHMF_RUN_INTEGRATION=1 to run gateway integration tests against a live service")
	}
	return getenv("OHMF_BASE_URL", "http://localhost:18080")
}

func postJSON(t *testing.T, url string, body map[string]any, bearer string) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var b map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&b)
		t.Fatalf("post %s status=%d body=%v", url, resp.StatusCode, b)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response %s: %v", url, err)
	}
	return out
}

func postStatus(t *testing.T, url string, body map[string]any, bearer string) int {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post status %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func postJSONWithStatus(t *testing.T, url string, body map[string]any, bearer string) (int, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post status %s: %v", url, err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func getJSON(t *testing.T, url, bearer string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var b map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&b)
		t.Fatalf("get %s status=%d body=%v", url, resp.StatusCode, b)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response %s: %v", url, err)
	}
	return out
}

func mustMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	return m
}

func mustSlice(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("expected slice, got %T", v)
	}
	return a
}

func mustString(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string, got %T", v)
	}
	return s
}

func mustFloat64(t *testing.T, v any) float64 {
	t.Helper()
	n, ok := v.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", v)
	}
	return n
}

func verifyUser(t *testing.T, baseURL, phone, name string) map[string]string {
	t.Helper()
	start := postJSON(t, baseURL+"/v1/auth/phone/start", map[string]any{"phone_e164": phone, "channel": "SMS"}, "")
	verify := postJSON(t, baseURL+"/v1/auth/phone/verify", map[string]any{
		"challenge_id": start["challenge_id"],
		"otp_code":     "123456",
		"device": map[string]any{
			"platform":    "WEB",
			"device_name": name,
		},
	}, "")
	user := mustMap(t, verify["user"])
	tokens := mustMap(t, verify["tokens"])
	return map[string]string{
		"user_id":      mustString(t, user["user_id"]),
		"access_token": mustString(t, tokens["access_token"]),
	}
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
