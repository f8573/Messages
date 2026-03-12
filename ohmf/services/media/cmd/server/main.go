package main

import (
    "encoding/json"
    "log"
    "net/http"
    "time"
)

type initReq struct{
    Items []struct{
        MimeType string `json:"mime_type"`
        SizeBytes int64 `json:"size_bytes"`
        Kind string `json:"kind"`
    } `json:"items"`
}

func handleInit(w http.ResponseWriter, r *http.Request) {
    var req initReq
    _ = json.NewDecoder(r.Body).Decode(&req)
    // Placeholder: generate upload id and a fake upload URL
    uploadID := "upl-"+time.Now().Format("20060102T150405")
    resp := map[string]string{"upload_id": uploadID, "upload_url": "https://storage.example/upload/"+uploadID}
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(resp)
}

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("/v1/media/uploads", handleInit)
    addr := ":18087"
    log.Printf("media service listening %s", addr)
    log.Fatal(http.ListenAndServe(addr, mux))
}
