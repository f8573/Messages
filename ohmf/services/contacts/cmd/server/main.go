package main

import (
    "encoding/json"
    "log"
    "net/http"
)

type discoverRequest struct {
    Algorithm string `json:"algorithm"`
    Contacts  []struct {
        Hash  string `json:"hash"`
        Label string `json:"label"`
    } `json:"contacts"`
}

func handleDiscover(w http.ResponseWriter, r *http.Request) {
    var req discoverRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"code":"bad_request","message":"invalid json","request_id":""}` , http.StatusBadRequest)
        return
    }
    // Placeholder: real implementation should perform peppered-hash lookup or PSI.
    resp := map[string]interface{}{"matches": []interface{}{}}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("/v1/contacts/discover", handleDiscover)
    addr := ":18085"
    log.Printf("contacts service listening %s", addr)
    log.Fatal(http.ListenAndServe(addr, mux))
}
