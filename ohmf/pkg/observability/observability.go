package observability

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Logger *log.Logger
var initOnce sync.Once

var httpRequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ohmf_http_requests_total",
		Help: "Total number of HTTP requests handled by lightweight OHMF services.",
	},
	[]string{"method", "status"},
)

var httpRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "ohmf_http_request_duration_seconds",
		Help:    "HTTP request latency for lightweight OHMF services.",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"method", "status"},
)

var httpRequestsInflight = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "ohmf_http_requests_in_flight",
		Help: "Current number of in-flight HTTP requests handled by lightweight OHMF services.",
	},
	[]string{"method"},
)

func Init() {
	initOnce.Do(func() {
		Logger = log.New(os.Stdout, "observability: ", log.LstdFlags)
		prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, httpRequestsInflight)
	})
}

func generateRequestID() string {
	b := make([]byte, 12)
	_, err := rand.Read(b)
	if err != nil {
		return "req-unknown"
	}
	return hex.EncodeToString(b)
}

func generateTraceparent() string {
	traceID := randomHex(16)
	if traceID == "" {
		traceID = strings.Repeat("0", 32)
	}
	parentID := randomHex(8)
	if parentID == "" {
		parentID = strings.Repeat("0", 16)
	}
	return fmt.Sprintf("00-%s-%s-01", traceID, parentID)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := r.Header.Get("X-Request-Id")
		if reqID == "" {
			reqID = generateRequestID()
		}
		traceparent := r.Header.Get("Traceparent")
		if traceparent == "" {
			traceparent = generateTraceparent()
		}
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		recorder.Header().Set("X-Request-Id", reqID)
		recorder.Header().Set("Traceparent", traceparent)
		httpRequestsInflight.WithLabelValues(r.Method).Inc()
		defer func() {
			status := strconv.Itoa(recorder.status)
			httpRequestsInflight.WithLabelValues(r.Method).Dec()
			httpRequestsTotal.WithLabelValues(r.Method, status).Inc()
			httpRequestDuration.WithLabelValues(r.Method, status).Observe(time.Since(start).Seconds())
		}()
		if Logger != nil {
			Logger.Printf("req=%s traceparent=%s method=%s path=%s remote=%s", reqID, traceparent, r.Method, r.URL.Path, r.RemoteAddr)
		}
		next.ServeHTTP(recorder, r)
	})
}

func MetricsHandler() http.Handler {
	Init()
	return promhttp.Handler()
}
