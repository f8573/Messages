package main

import (
    "bytes"
    "encoding/json"
    "io/ioutil"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"

    "github.com/f8573/Messages/pkg/observability"
)

func TestRegisterSuccessAndValidation(t *testing.T) {
    observability.Init()
    tmpDir := t.TempDir()
    dataFile := filepath.Join(tmpDir, "registered_apps.json")
    s := newAppsServer(dataFile)
    mux := http.NewServeMux()
    mux.Handle("/v1/apps/register", http.HandlerFunc(s.registerHandler))
    handler := observability.RequestIDMiddleware(mux)
    ts := httptest.NewServer(handler)
    defer ts.Close()

    // valid manifest
    manifest := Manifest{
        AppID:      "com.example.app",
        Entrypoint: "https://example.com/app",
        Permissions: []string{"camera", "storage"},
        Signature:  "sig-abc",
    }
    bs, _ := json.Marshal(manifest)
    res, err := http.Post(ts.URL+"/v1/apps/register", "application/json", bytes.NewReader(bs))
    if err != nil {
        t.Fatalf("post failed: %v", err)
    }
    if res.StatusCode != http.StatusCreated {
        t.Fatalf("expected 201, got %d", res.StatusCode)
    }
    // file should exist and contain manifest
    content, err := ioutil.ReadFile(dataFile)
    if err != nil {
        t.Fatalf("read file failed: %v", err)
    }
    var list []Manifest
    if err := json.Unmarshal(content, &list); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }
    if len(list) != 1 || list[0].AppID != manifest.AppID {
        t.Fatalf("unexpected saved data: %v", list)
    }

    // duplicate registration
    res2, err := http.Post(ts.URL+"/v1/apps/register", "application/json", bytes.NewReader(bs))
    if err != nil {
        t.Fatalf("post dup failed: %v", err)
    }
    if res2.StatusCode != http.StatusConflict {
        t.Fatalf("expected 409, got %d", res2.StatusCode)
    }

    // invalid manifest (missing fields)
    bad := map[string]interface{}{"app_id": ""}
    badBs, _ := json.Marshal(bad)
    res3, err := http.Post(ts.URL+"/v1/apps/register", "application/json", bytes.NewReader(badBs))
    if err != nil {
        t.Fatalf("post bad failed: %v", err)
    }
    if res3.StatusCode != http.StatusBadRequest {
        t.Fatalf("expected 400, got %d", res3.StatusCode)
    }
}

func TestMain(m *testing.M) {
    observability.Init()
    os.Exit(m.Run())
}
