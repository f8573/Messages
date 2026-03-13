package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandlerExposesGatewayMetrics(t *testing.T) {
	handler := HTTPMetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	IncWSConnection()
	RecordWSMessage("received", "subscribe")
	RecordWSMessage("sent", "subscribe_ack")
	DecWSConnection()

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRR := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(metricsRR, metricsReq)

	if metricsRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", metricsRR.Code)
	}
	body := metricsRR.Body.String()
	if !strings.Contains(body, "ohmf_gateway_http_requests_total") {
		t.Fatalf("expected http metrics in body")
	}
	if !strings.Contains(body, "ohmf_gateway_ws_messages_total") {
		t.Fatalf("expected ws metrics in body")
	}
}
