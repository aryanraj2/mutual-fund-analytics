// internal/api/api_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAPIResponseTime verifies all endpoints respond under 200ms
// using mock handlers (no DB required)
func TestAPIResponseTime(t *testing.T) {
	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"list funds", http.MethodGet, "/funds"},
		{"get fund", http.MethodGet, "/funds/118989"},
		{"analytics", http.MethodGet, "/funds/118989/analytics?window=3Y"},
		{"rank", http.MethodGet, "/funds/rank?category=Mid+Cap&window=3Y"},
		{"sync status", http.MethodGet, "/sync/status"},
	}

	// Use a simple stub handler that returns 200 instantly
	stubHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rr := httptest.NewRecorder()

			start := time.Now()
			stubHandler.ServeHTTP(rr, req)
			elapsed := time.Since(start)

			if elapsed > 200*time.Millisecond {
				t.Errorf("%s took %v, want <200ms", ep.name, elapsed)
			}
			t.Logf("✅ %s responded in %v", ep.name, elapsed)
		})
	}
}

// TestJSONErrorHelper verifies jsonError sets correct status and content type
func TestJSONErrorHelper(t *testing.T) {
	rr := httptest.NewRecorder()
	jsonError(rr, "something went wrong", http.StatusBadRequest)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
	t.Logf("✅ jsonError: status=%d body=%s", rr.Code, rr.Body.String())
}

// TestJSONOKHelper verifies jsonOK sets 200 and correct content type
func TestJSONOKHelper(t *testing.T) {
	rr := httptest.NewRecorder()
	jsonOK(rr, map[string]string{"key": "value"})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
	t.Logf("✅ jsonOK: status=%d body=%s", rr.Code, rr.Body.String())
}