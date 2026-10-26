package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/platform/requestmeta"
)

const testAuditHashKey = "p6-test-audit-ip-hash-key-at-least-32-bytes"

func testOptions() Options {
	return Options{
		RequestTimeout:      100 * time.Millisecond,
		AuthRateLimitMax:    2,
		AuthRateLimitWindow: time.Minute,
		AuditIPHashKey:      testAuditHashKey,
	}
}

func TestHardeningAddsTraceSecurityAndRedactedStructuredLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /probe", func(w http.ResponseWriter, r *http.Request) {
		metadata, ok := requestmeta.FromContext(r.Context())
		if !ok || metadata.IPHash == "" {
			t.Fatal("request metadata or keyed IP hash missing")
		}
		httpx.WriteError(w, http.StatusBadRequest, "PROBE", "probe")
	})
	handler := wrapHardening(mux, logger, testOptions())
	request := httptest.NewRequest(http.MethodGet, "https://example.test/probe?token=secret-query", nil)
	request.RemoteAddr = "203.0.113.7:4321"
	request.Header.Set("Authorization", "Bearer secret-access-token")
	request.Header.Set(requestIDHeader, "invalid\nidentifier")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	requestID := response.Header().Get(requestIDHeader)
	if normalizedUUID(requestID) == "" || response.Header().Get(correlationIDHeader) != requestID {
		t.Fatalf("invalid generated trace headers: request=%q correlation=%q", requestID, response.Header().Get(correlationIDHeader))
	}
	for key, want := range map[string]string{
		"Cache-Control":                "no-store",
		"Content-Security-Policy":      "default-src 'none'; base-uri 'none'; frame-ancestors 'none'",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Permissions-Policy":           "camera=(), geolocation=(), microphone=()",
		"Referrer-Policy":              "no-referrer",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	} {
		if got := response.Header().Get(key); got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
	var body httpx.ErrorBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RequestID != requestID || body.Error.Code != "PROBE" {
		t.Fatalf("error trace envelope=%+v", body)
	}
	logged := logs.String()
	for _, secret := range []string{"secret-query", "secret-access-token", "203.0.113.7"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("structured request log leaked %q: %s", secret, logged)
		}
	}
	for _, expected := range []string{requestID, "GET /probe", `"status":400`} {
		if !strings.Contains(logged, expected) {
			t.Errorf("structured request log missing %q: %s", expected, logged)
		}
	}
}

func TestHardeningPreservesOnlyCanonicalTraceIDs(t *testing.T) {
	requestID := "550e8400-e29b-41d4-a716-446655440000"
	correlationID := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	handler := wrapHardening(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), nil, testOptions())
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(requestIDHeader, strings.ToUpper(requestID))
	request.Header.Set(correlationIDHeader, correlationID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get(requestIDHeader) != requestID || response.Header().Get(correlationIDHeader) != correlationID {
		t.Fatalf("trace propagation request=%q correlation=%q", response.Header().Get(requestIDHeader), response.Header().Get(correlationIDHeader))
	}
}

func TestDevelopmentCORSUsesExplicitAllowlistAndShortCircuitsPreflight(t *testing.T) {
	var calls atomic.Int32
	options := testOptions()
	options.CORSAllowedOrigins = []string{"http://127.0.0.1:5173"}
	handler := wrapHardening(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}), nil, options)

	preflight := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/staff/login", nil)
	preflight.Header.Set("Origin", "http://127.0.0.1:5173")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "authorization, content-type, x-request-id")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, preflight)
	if response.Code != http.StatusNoContent || calls.Load() != 0 {
		t.Fatalf("preflight status=%d calls=%d", response.Code, calls.Load())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
		t.Fatalf("allow origin=%q", got)
	}
	if response.Header().Get("X-Request-ID") == "" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("preflight must retain hardening headers: %v", response.Header())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/staff/login", nil)
	request.Header.Set("Origin", "http://127.0.0.1:5173")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || calls.Load() != 1 {
		t.Fatalf("allowed request status=%d calls=%d", response.Code, calls.Load())
	}

	disallowed := httptest.NewRequest(http.MethodGet, "/probe", nil)
	disallowed.Header.Set("Origin", "http://malicious.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, disallowed)
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin unexpectedly received CORS header: %v", response.Header())
	}

	invalidHeader := httptest.NewRequest(http.MethodOptions, "/probe", nil)
	invalidHeader.Header.Set("Origin", "http://127.0.0.1:5173")
	invalidHeader.Header.Set("Access-Control-Request-Method", http.MethodPost)
	invalidHeader.Header.Set("Access-Control-Request-Headers", "x-untrusted-header")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, invalidHeader)
	if response.Code != http.StatusForbidden || calls.Load() != 2 {
		t.Fatalf("invalid preflight status=%d calls=%d", response.Code, calls.Load())
	}
}

func TestAuthRateLimitReturnsStableJSONError(t *testing.T) {
	var calls atomic.Int32
	handler := wrapHardening(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}), nil, testOptions())
	for attempt := 1; attempt <= 3; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/staff/login", strings.NewReader(`{}`))
		request.RemoteAddr = "192.0.2.10:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if attempt < 3 && response.Code != http.StatusNoContent {
			t.Fatalf("attempt %d status=%d", attempt, response.Code)
		}
		if attempt == 3 {
			if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
				t.Fatalf("rate-limit response status=%d headers=%v", response.Code, response.Header())
			}
			var body httpx.ErrorBody
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "RATE_LIMIT_EXCEEDED" || body.RequestID == "" {
				t.Fatalf("rate-limit body=%s err=%v", response.Body.String(), err)
			}
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("protected handler calls=%d want 2", calls.Load())
	}
}

func TestAuthRateLimitAlsoKeysByIdentifierAndRestoresBody(t *testing.T) {
	var decoded atomic.Int32
	handler := wrapHardening(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
		}
		if httpx.DecodeJSON(w, r, &body) && body.Username == "same.user" {
			decoded.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}
	}), nil, testOptions())
	for attempt := 1; attempt <= 3; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/staff/login", strings.NewReader(`{"username":"same.user"}`))
		request.RemoteAddr = "192.0.2." + strconv.Itoa(attempt) + ":1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if attempt == 3 && response.Code != http.StatusTooManyRequests {
			t.Fatalf("identifier limit status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if decoded.Load() != 2 {
		t.Fatalf("decoded bodies=%d want 2", decoded.Load())
	}
}

func TestRequestTimeoutAndPanicRecoveryUseSafeErrors(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		options := testOptions()
		options.RequestTimeout = 5 * time.Millisecond
		handler := wrapHardening(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}), nil, options)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/slow", nil))
		if response.Code != http.StatusGatewayTimeout || !strings.Contains(response.Body.String(), "REQUEST_TIMEOUT") {
			t.Fatalf("timeout response status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("panic", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		handler := wrapHardening(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("secret panic payload")
		}), logger, testOptions())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "INTERNAL_ERROR") {
			t.Fatalf("panic response status=%d body=%s", response.Code, response.Body.String())
		}
		if strings.Contains(logs.String(), "secret panic payload") {
			t.Fatalf("panic value leaked to log: %s", logs.String())
		}
	})
}

func TestP6ConcurrentHealthSmoke(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	handler := wrapHardening(mux, logger, testOptions())
	const requests = 256
	var successes atomic.Int32
	var wait sync.WaitGroup
	for range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
			if response.Code == http.StatusOK && normalizedUUID(response.Header().Get(requestIDHeader)) != "" {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != requests {
		t.Fatalf("concurrent health successes=%d want %d", successes.Load(), requests)
	}
}
