package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_PreflightReturns204WithHeaders(t *testing.T) {
	h := CORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("preflight must not reach the next handler")
	}))

	r := httptest.NewRequest(http.MethodOptions, "/auctions", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("origin not reflected, got %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("missing Allow-Headers")
	}
}

func TestCORS_PassesThroughWithOriginHeader(t *testing.T) {
	called := false
	h := CORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/auctions", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if !called {
		t.Fatal("next handler was not called")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatal("Allow-Origin not set on actual request")
	}
}
