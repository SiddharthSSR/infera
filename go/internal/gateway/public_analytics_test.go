package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHandlePublicAnalyticsRecordsBoundedEvent(t *testing.T) {
	metrics := NewGatewayMetrics()
	g := &Gateway{metrics: metrics}
	req := httptest.NewRequest(http.MethodPost, "/api/public-analytics/events", strings.NewReader(`{
		"name":"public_resource_opened",
		"properties":{"resource":"quickstart","source":"landing"}
	}`))
	rec := httptest.NewRecorder()

	g.handlePublicAnalytics(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}
	if got := testutil.ToFloat64(metrics.publicFunnelEvents.WithLabelValues(
		"public_resource_opened", "landing", "quickstart",
	)); got != 1 {
		t.Fatalf("expected funnel counter=1, got %v", got)
	}
}

func TestHandlePublicAnalyticsRejectsUnboundedOrSensitivePayloads(t *testing.T) {
	cases := []string{
		`{"name":"public_sign_in_intent","properties":{"source":"user@example.com"}}`,
		`{"name":"public_landing_view","properties":{"surface":"migration_landing","api_key":"inf_secret"}}`,
		`{"name":"unknown_event","properties":{}}`,
		`{"name":"public_landing_view","properties":{"surface":"migration_landing"},"prompt":"secret"}`,
	}

	for _, body := range cases {
		metrics := NewGatewayMetrics()
		g := &Gateway{metrics: metrics}
		rec := httptest.NewRecorder()
		g.handlePublicAnalytics(rec, httptest.NewRequest(http.MethodPost, "/api/public-analytics/events", strings.NewReader(body)))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d", body, rec.Code)
		}
	}
}

func TestHandlePublicAnalyticsRequiresPost(t *testing.T) {
	g := &Gateway{metrics: NewGatewayMetrics()}
	rec := httptest.NewRecorder()
	g.handlePublicAnalytics(rec, httptest.NewRequest(http.MethodGet, "/api/public-analytics/events", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandlePublicAnalyticsRejectsOversizedPayload(t *testing.T) {
	g := &Gateway{metrics: NewGatewayMetrics()}
	rec := httptest.NewRecorder()
	body := `{"name":"public_landing_view","properties":{"surface":"migration_landing"},"padding":"` + strings.Repeat("x", maxPublicAnalyticsBodyBytes) + `"}`
	g.handlePublicAnalytics(rec, httptest.NewRequest(http.MethodPost, "/api/public-analytics/events", strings.NewReader(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
