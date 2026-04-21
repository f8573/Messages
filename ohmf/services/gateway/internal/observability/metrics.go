package observability

import (
	"bufio"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var metricsOnce sync.Once

var httpRequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ohmf_gateway_http_requests_total",
		Help: "Total number of HTTP requests handled by the gateway.",
	},
	[]string{"method", "route", "status"},
)

var httpRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "ohmf_gateway_http_request_duration_seconds",
		Help:    "HTTP request latency for the gateway.",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"method", "route", "status"},
)

var httpRequestsInflight = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "ohmf_gateway_http_requests_in_flight",
		Help: "Current number of in-flight HTTP requests handled by the gateway.",
	},
	[]string{"method", "route"},
)

var wsConnectionsActive = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "ohmf_gateway_ws_connections_active",
		Help: "Current number of active WebSocket gateway connections.",
	},
)

var wsMessagesTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ohmf_gateway_ws_messages_total",
		Help: "Total number of WebSocket messages handled by the gateway.",
	},
	[]string{"direction", "event"},
)

var dbPoolAcquired = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "ohmf_gateway_db_pool_acquired_connections",
		Help: "Current number of Postgres connections acquired from the gateway pool.",
	},
)

var dbPoolIdle = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "ohmf_gateway_db_pool_idle_connections",
		Help: "Current number of idle Postgres connections in the gateway pool.",
	},
)

var dbPoolTotal = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "ohmf_gateway_db_pool_total_connections",
		Help: "Current total number of Postgres connections tracked by the gateway pool.",
	},
)

var dbPoolConstructing = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "ohmf_gateway_db_pool_constructing_connections",
		Help: "Current number of Postgres connections being constructed by the gateway pool.",
	},
)

var dbPoolMax = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "ohmf_gateway_db_pool_max_connections",
		Help: "Configured maximum size of the gateway Postgres connection pool.",
	},
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if p, ok := r.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func initMetrics() {
	metricsOnce.Do(func() {
		prometheus.MustRegister(
			httpRequestsTotal,
			httpRequestDuration,
			httpRequestsInflight,
			wsConnectionsActive,
			wsMessagesTotal,
			dbPoolAcquired,
			dbPoolIdle,
			dbPoolTotal,
			dbPoolConstructing,
			dbPoolMax,
		)
	})
}

func MetricsHandler() http.Handler {
	initMetrics()
	return promhttp.Handler()
}

func HTTPMetricsMiddleware(next http.Handler) http.Handler {
	initMetrics()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		routeLabel := routePattern(r)
		httpRequestsInflight.WithLabelValues(r.Method, routeLabel).Inc()
		defer func() {
			status := strconv.Itoa(recorder.status)
			httpRequestsInflight.WithLabelValues(r.Method, routeLabel).Dec()
			httpRequestsTotal.WithLabelValues(r.Method, routeLabel, status).Inc()
			httpRequestDuration.WithLabelValues(r.Method, routeLabel, status).Observe(time.Since(start).Seconds())
		}()
		next.ServeHTTP(recorder, r)
	})
}

func IncWSConnection() {
	initMetrics()
	wsConnectionsActive.Inc()
}

func DecWSConnection() {
	initMetrics()
	wsConnectionsActive.Dec()
}

func RecordWSMessage(direction, event string) {
	initMetrics()
	if direction == "" {
		direction = "unknown"
	}
	if event == "" {
		event = "unknown"
	}
	wsMessagesTotal.WithLabelValues(direction, event).Inc()
}

func RecordDBPool(acquired, idle, total, constructing, max int32) {
	initMetrics()
	dbPoolAcquired.Set(float64(acquired))
	dbPoolIdle.Set(float64(idle))
	dbPoolTotal.Set(float64(total))
	dbPoolConstructing.Set(float64(constructing))
	dbPoolMax.Set(float64(max))
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	if r.URL.Path == "" {
		return "/"
	}
	return r.URL.Path
}
