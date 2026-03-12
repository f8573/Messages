package observability

import (
    "crypto/rand"
    "encoding/hex"
    "log"
    "net/http"
    "os"
)

var Logger *log.Logger

func Init() {
    if Logger == nil {
        Logger = log.New(os.Stdout, "observability: ", log.LstdFlags)
    }
}

func generateRequestID() string {
    b := make([]byte, 12)
    _, err := rand.Read(b)
    if err != nil {
        return "req-unknown"
    }
    return hex.EncodeToString(b)
}

func RequestIDMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        reqID := r.Header.Get("X-Request-Id")
        if reqID == "" {
            reqID = generateRequestID()
        }
        w.Header().Set("X-Request-Id", reqID)
        if Logger != nil {
            Logger.Printf("req=%s method=%s path=%s remote=%s", reqID, r.Method, r.URL.Path, r.RemoteAddr)
        }
        next.ServeHTTP(w, r)
    })
}
