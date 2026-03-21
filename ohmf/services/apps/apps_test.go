package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/f8573/Messages/pkg/observability"
)

func testManifest(appID, version, url string) map[string]any {
	return testManifestWithPermissions(appID, version, url, []string{"conversation.read_context"})
}

func testManifestWithPermissions(appID, version, url string, permissions []string) map[string]any {
	return map[string]any{
		"manifest_version": "1.0",
		"app_id":           appID,
		"name":             "Test App",
		"version":          version,
		"entrypoint": map[string]any{
			"type": "url",
			"url":  url,
		},
		"message_preview": map[string]any{
			"type": "static_image",
			"url":  "https://example.com/preview.png",
		},
		"permissions": permissions,
		"capabilities": map[string]any{
			"turn_based": true,
		},
		"signature": map[string]any{
			"alg": "RS256",
			"kid": "dev",
			"sig": "placeholder",
		},
	}
}

func request(t *testing.T, client *http.Client, method, url string, headers map[string]string, body any) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

func TestRegisterListInstallAndUpdates(t *testing.T) {
	observability.Init()
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "registry.json")
	server := newAppsServer(dataFile)
	ts := httptest.NewServer(makeHandler(server))
	defer ts.Close()

	client := ts.Client()
	headers := map[string]string{"X-User-ID": "usr_123"}

	registerBody := map[string]any{"manifest": testManifest("app.test.counter", "1.0.0", "http://localhost:5174/miniapps/counter/index.html")}
	res := request(t, client, http.MethodPost, ts.URL+"/v1/apps/register", headers, registerBody)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodGet, ts.URL+"/v1/apps", headers, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", res.StatusCode)
	}
	var listed map[string]any
	if err := json.NewDecoder(res.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	_ = res.Body.Close()
	items, _ := listed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 listed app, got %#v", listed)
	}

	res = request(t, client, http.MethodPost, ts.URL+"/v1/apps/app.test.counter/install", headers, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("install status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	registerBody = map[string]any{"manifest": testManifest("app.test.counter", "1.1.0", "http://localhost:5174/miniapps/counter/index.html")}
	res = request(t, client, http.MethodPost, ts.URL+"/v1/apps/register", headers, registerBody)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("second register status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodGet, ts.URL+"/v1/apps/app.test.counter/updates", headers, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("updates status = %d", res.StatusCode)
	}
	var updatePayload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&updatePayload); err != nil {
		t.Fatalf("decode updates: %v", err)
	}
	_ = res.Body.Close()
	if updatePayload["update_available"] != true {
		t.Fatalf("expected update_available, got %#v", updatePayload)
	}
}

func TestPublisherReviewFlow(t *testing.T) {
	observability.Init()
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "registry.json")
	server := newAppsServer(dataFile)
	ts := httptest.NewServer(makeHandler(server))
	defer ts.Close()

	client := ts.Client()
	pubHeaders := map[string]string{"X-User-ID": "usr_pub"}
	adminHeaders := map[string]string{"X-User-ID": "usr_admin", "X-User-Role": "admin"}

	res := request(t, client, http.MethodPost, ts.URL+"/v1/publisher/apps", pubHeaders, map[string]any{
		"app_id": "app.reviewed.demo",
		"name":   "Reviewed Demo",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create app status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/publisher/apps/app.reviewed.demo/releases", pubHeaders, map[string]any{
		"manifest": testManifest("app.reviewed.demo", "1.0.0", "https://apps.example.com/demo/index.html"),
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create release status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/publisher/apps/app.reviewed.demo/releases/1.0.0/submit", pubHeaders, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("submit status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/admin/apps/app.reviewed.demo/releases/1.0.0/approve", adminHeaders, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("approve status = %d", res.StatusCode)
	}
	var approved map[string]any
	if err := json.NewDecoder(res.Body).Decode(&approved); err != nil {
		t.Fatalf("decode approve: %v", err)
	}
	_ = res.Body.Close()
	if approved["review_status"] != statusApproved {
		t.Fatalf("expected approved release, got %#v", approved)
	}
}

func TestExpandedReviewWorkflow(t *testing.T) {
	observability.Init()
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "registry.json")
	server := newAppsServer(dataFile)
	ts := httptest.NewServer(makeHandler(server))
	defer ts.Close()

	client := ts.Client()
	pubHeaders := map[string]string{"X-User-ID": "usr_pub"}
	adminHeaders := map[string]string{"X-User-ID": "usr_admin", "X-User-Role": "admin"}

	res := request(t, client, http.MethodPost, ts.URL+"/v1/publisher/apps", pubHeaders, map[string]any{
		"app_id": "app.review.states",
		"name":   "Review States",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create app status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/publisher/apps/app.review.states/releases", pubHeaders, map[string]any{
		"manifest": testManifest("app.review.states", "1.0.0", "https://apps.example.com/review/index.html"),
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create release status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/publisher/apps/app.review.states/releases/1.0.0/submit", pubHeaders, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("submit status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/admin/apps/app.review.states/releases/1.0.0/start-review", adminHeaders, map[string]any{
		"reason": "initial policy review",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start review status = %d", res.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode start review: %v", err)
	}
	_ = res.Body.Close()
	if payload["review_status"] != statusUnderReview {
		t.Fatalf("expected under_review, got %#v", payload)
	}

	res = request(t, client, http.MethodPost, ts.URL+"/v1/admin/apps/app.review.states/releases/1.0.0/needs-changes", adminHeaders, map[string]any{
		"reason": "permission copy is unclear",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("needs changes status = %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode needs changes: %v", err)
	}
	_ = res.Body.Close()
	if payload["review_status"] != statusNeedsChanges {
		t.Fatalf("expected needs_changes, got %#v", payload)
	}

	res = request(t, client, http.MethodPost, ts.URL+"/v1/admin/apps/app.review.states/releases/1.0.0/suspend", adminHeaders, map[string]any{
		"reason": "temporary abuse hold",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("suspend status = %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode suspend: %v", err)
	}
	_ = res.Body.Close()
	if payload["review_status"] != statusSuspended {
		t.Fatalf("expected suspended, got %#v", payload)
	}
}

func TestCatalogHidesUnapprovedReleaseFromNormalUsers(t *testing.T) {
	observability.Init()
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "registry.json")
	server := newAppsServer(dataFile)
	ts := httptest.NewServer(makeHandler(server))
	defer ts.Close()

	client := ts.Client()
	pubHeaders := map[string]string{"X-User-ID": "usr_pub"}
	userHeaders := map[string]string{"X-User-ID": "usr_viewer"}

	res := request(t, client, http.MethodPost, ts.URL+"/v1/publisher/apps", pubHeaders, map[string]any{
		"app_id": "app.pending.demo",
		"name":   "Pending Demo",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create app status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/publisher/apps/app.pending.demo/releases", pubHeaders, map[string]any{
		"manifest": testManifest("app.pending.demo", "1.0.0", "https://apps.example.com/pending/index.html"),
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create release status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodGet, ts.URL+"/v1/apps", userHeaders, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", res.StatusCode)
	}
	var listed map[string]any
	if err := json.NewDecoder(res.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	_ = res.Body.Close()
	items, _ := listed["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("expected no visible apps for normal user, got %#v", listed)
	}
}

func TestDevReleaseRequiresDeveloperModeForNonOwner(t *testing.T) {
	observability.Init()
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "registry.json")
	server := newAppsServer(dataFile)
	ts := httptest.NewServer(makeHandler(server))
	defer ts.Close()

	client := ts.Client()
	devHeaders := map[string]string{"X-User-ID": "usr_dev"}
	userHeaders := map[string]string{"X-User-ID": "usr_viewer"}

	res := request(t, client, http.MethodPost, ts.URL+"/v1/apps/register", devHeaders, map[string]any{
		"manifest": testManifest("app.dev.only", "1.0.0", "http://localhost:5174/miniapps/dev/index.html"),
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodGet, ts.URL+"/v1/apps", userHeaders, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", res.StatusCode)
	}
	var listed map[string]any
	if err := json.NewDecoder(res.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	_ = res.Body.Close()
	items, _ := listed["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("expected dev app to stay hidden without developer mode, got %#v", listed)
	}

	res = request(t, client, http.MethodGet, ts.URL+"/v1/apps?developer_mode=1", userHeaders, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("developer mode list status = %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&listed); err != nil {
		t.Fatalf("decode developer mode list: %v", err)
	}
	_ = res.Body.Close()
	items, _ = listed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected dev app to be visible with developer mode, got %#v", listed)
	}
}

func TestInstallUpdateRequiresConsentWhenPermissionsExpand(t *testing.T) {
	observability.Init()
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "registry.json")
	server := newAppsServer(dataFile)
	ts := httptest.NewServer(makeHandler(server))
	defer ts.Close()

	client := ts.Client()
	headers := map[string]string{"X-User-ID": "usr_123"}

	res := request(t, client, http.MethodPost, ts.URL+"/v1/apps/register", headers, map[string]any{
		"manifest": testManifestWithPermissions("app.test.permissions", "1.0.0", "http://localhost:5174/miniapps/counter/index.html", []string{"conversation.read_context"}),
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register v1 status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/apps/app.test.permissions/install", headers, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("install v1 status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/apps/register", headers, map[string]any{
		"manifest": testManifestWithPermissions("app.test.permissions", "1.1.0", "http://localhost:5174/miniapps/counter/index.html", []string{"conversation.read_context", "conversation.send_message"}),
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register v2 status = %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = request(t, client, http.MethodPost, ts.URL+"/v1/apps/app.test.permissions/install", headers, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("install without consent status = %d", res.StatusCode)
	}
	var installed map[string]any
	if err := json.NewDecoder(res.Body).Decode(&installed); err != nil {
		t.Fatalf("decode install response: %v", err)
	}
	_ = res.Body.Close()
	install, _ := installed["install"].(map[string]any)
	if install["installed_version"] != "1.0.0" {
		t.Fatalf("expected installed version to stay on 1.0.0, got %#v", install)
	}
	if installed["update_requires_consent"] != true {
		t.Fatalf("expected update_requires_consent, got %#v", installed)
	}

	res = request(t, client, http.MethodGet, ts.URL+"/v1/apps/app.test.permissions/updates", headers, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("updates status = %d", res.StatusCode)
	}
	var updates map[string]any
	if err := json.NewDecoder(res.Body).Decode(&updates); err != nil {
		t.Fatalf("decode updates response: %v", err)
	}
	_ = res.Body.Close()
	if updates["update_requires_consent"] != true {
		t.Fatalf("expected update_requires_consent on updates, got %#v", updates)
	}
}
