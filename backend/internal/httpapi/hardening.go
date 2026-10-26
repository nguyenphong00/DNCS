package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/platform/requestmeta"
)

const (
	requestIDHeader     = "X-Request-ID"
	correlationIDHeader = "X-Correlation-ID"
)

type Options struct {
	RequestTimeout      time.Duration
	AuthRateLimitMax    int
	AuthRateLimitWindow time.Duration
	AuditIPHashKey      string
	CORSAllowedOrigins  []string
}

func DefaultOptions() Options {
	return Options{
		RequestTimeout:      25 * time.Second,
		AuthRateLimitMax:    20,
		AuthRateLimitWindow: time.Minute,
	}
}

func resolveOptions(options []Options) Options {
	if len(options) == 0 {
		return DefaultOptions()
	}
	resolved := options[0]
	defaults := DefaultOptions()
	if resolved.RequestTimeout <= 0 {
		resolved.RequestTimeout = defaults.RequestTimeout
	}
	if resolved.AuthRateLimitMax <= 0 {
		resolved.AuthRateLimitMax = defaults.AuthRateLimitMax
	}
	if resolved.AuthRateLimitWindow <= 0 {
		resolved.AuthRateLimitWindow = defaults.AuthRateLimitWindow
	}
	return resolved
}

func wrapHardening(next http.Handler, logger *slog.Logger, options Options) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	limiter := newFixedWindowLimiter(options.AuthRateLimitMax, options.AuthRateLimitWindow, 10_000)
	handler := next
	handler = recoverPanics(logger, handler)
	handler = authRateLimit(limiter, handler)
	handler = requestTimeout(options.RequestTimeout, handler)
	handler = developmentCORS(options.CORSAllowedOrigins, handler)
	handler = requestLog(logger, handler)
	handler = securityHeaders(handler)
	handler = routePattern(next, handler)
	handler = requestMetadata(options.AuditIPHashKey, handler)
	return handler
}

var (
	corsAllowedMethods = map[string]struct{}{
		http.MethodGet: {}, http.MethodPost: {}, http.MethodPut: {}, http.MethodPatch: {}, http.MethodDelete: {},
	}
	corsAllowedHeaders = map[string]struct{}{
		"authorization": {}, "content-type": {}, "idempotency-key": {}, "x-request-id": {}, "x-correlation-id": {},
	}
)

func developmentCORS(origins []string, next http.Handler) http.Handler {
	allowedOrigins := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowedOrigins[origin] = struct{}{}
	}
	if len(allowedOrigins) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, allowed := allowedOrigins[origin]; !allowed {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID, X-Correlation-ID")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-Correlation-ID, Retry-After, Idempotent-Replayed")
		w.Header().Add("Vary", "Origin")
		if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !validCORSPreflight(r) {
			httpx.WriteError(w, http.StatusForbidden, "CORS_PREFLIGHT_FORBIDDEN", "CORS preflight không được cho phép.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func validCORSPreflight(r *http.Request) bool {
	if _, allowed := corsAllowedMethods[r.Header.Get("Access-Control-Request-Method")]; !allowed {
		return false
	}
	for _, value := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
		header := strings.ToLower(strings.TrimSpace(value))
		if header == "" {
			continue
		}
		if _, allowed := corsAllowedHeaders[header]; !allowed {
			return false
		}
	}
	return true
}

type routeContextKey struct{}

type routeMatcher interface {
	Handler(*http.Request) (http.Handler, string)
}

func routePattern(application, next http.Handler) http.Handler {
	matcher, ok := application.(routeMatcher)
	if !ok {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := matcher.Handler(r)
		if pattern == "" {
			pattern = "UNMATCHED"
		}
		ctx := context.WithValue(r.Context(), routeContextKey{}, pattern)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestMetadata(ipHashKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := normalizedUUID(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = newUUID()
		}
		correlationID := normalizedUUID(r.Header.Get(correlationIDHeader))
		if correlationID == "" {
			correlationID = requestID
		}
		metadata := requestmeta.Metadata{
			RequestID:     requestID,
			CorrelationID: correlationID,
			IPHash:        hashRemoteIP(ipHashKey, r.RemoteAddr),
		}
		w.Header().Set(requestIDHeader, requestID)
		w.Header().Set(correlationIDHeader, correlationID)
		next.ServeHTTP(w, r.WithContext(requestmeta.WithContext(r.Context(), metadata)))
	})
}

var fallbackUUIDCounter atomic.Uint64

func newUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		fallback := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", time.Now().UnixNano(), fallbackUUIDCounter.Add(1))))
		copy(value[:], fallback[:16])
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func normalizedUUID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return ""
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return ""
		}
	}
	return value
}

func hashRemoteIP(key, remoteAddress string) string {
	if len(key) < 32 {
		return ""
	}
	host := remoteAddress
	if parsedHost, _, err := net.SplitHostPort(remoteAddress); err == nil {
		host = parsedHost
	}
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		host = ip.String()
	}
	if strings.TrimSpace(host) == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(host))
	return hex.EncodeToString(mac.Sum(nil))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

type responseCapture struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (writer *responseCapture) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *responseCapture) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	written, err := writer.ResponseWriter.Write(value)
	writer.bytes += written
	return written, err
}

func (writer *responseCapture) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
func (writer *responseCapture) Written() bool               { return writer.status != 0 }

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		capture := &responseCapture{ResponseWriter: w}
		next.ServeHTTP(capture, r)
		status := capture.status
		if status == 0 {
			status = http.StatusOK
		}
		route, ok := r.Context().Value(routeContextKey{}).(string)
		if !ok || route == "" {
			route = "UNMATCHED"
		}
		metadata, _ := requestmeta.FromContext(r.Context())
		logger.InfoContext(r.Context(), "http request",
			"request_id", metadata.RequestID,
			"correlation_id", metadata.CorrelationID,
			"method", r.Method,
			"route", route,
			"status", status,
			"response_bytes", capture.bytes,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	})
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				metadata, _ := requestmeta.FromContext(r.Context())
				logger.ErrorContext(r.Context(), "http panic recovered",
					"request_id", metadata.RequestID,
					"correlation_id", metadata.CorrelationID,
					"panic_type", fmt.Sprintf("%T", recovered),
				)
				tracker, tracked := w.(interface{ Written() bool })
				if !tracked || !tracker.Written() {
					httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Không thể xử lý yêu cầu.")
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type responseState struct {
	http.ResponseWriter
	wrote bool
}

func (writer *responseState) WriteHeader(status int) {
	writer.wrote = true
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *responseState) Write(value []byte) (int, error) {
	writer.wrote = true
	return writer.ResponseWriter.Write(value)
}

func (writer *responseState) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
func (writer *responseState) Written() bool               { return writer.wrote }

func requestTimeout(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		state := &responseState{ResponseWriter: w}
		next.ServeHTTP(state, r.WithContext(ctx))
		if !state.wrote && ctx.Err() == context.DeadlineExceeded {
			httpx.WriteError(w, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "Yêu cầu đã quá thời gian xử lý.")
		}
	})
}

type rateLimitEntry struct {
	count int
	reset time.Time
}

type fixedWindowLimiter struct {
	mu          sync.Mutex
	entries     map[string]rateLimitEntry
	limit       int
	window      time.Duration
	maxEntries  int
	lastCleanup time.Time
}

func newFixedWindowLimiter(limit int, window time.Duration, maxEntries int) *fixedWindowLimiter {
	return &fixedWindowLimiter{entries: make(map[string]rateLimitEntry), limit: limit, window: window, maxEntries: maxEntries}
}

func (limiter *fixedWindowLimiter) allow(key string, now time.Time) (bool, int, time.Time) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.lastCleanup.IsZero() || now.Sub(limiter.lastCleanup) >= limiter.window {
		for candidate, entry := range limiter.entries {
			if !now.Before(entry.reset) {
				delete(limiter.entries, candidate)
			}
		}
		limiter.lastCleanup = now
	}
	entry, exists := limiter.entries[key]
	if !exists || !now.Before(entry.reset) {
		if !exists && len(limiter.entries) >= limiter.maxEntries {
			key = "overflow"
			entry, exists = limiter.entries[key]
		}
		if !exists || !now.Before(entry.reset) {
			entry = rateLimitEntry{reset: now.Add(limiter.window)}
		}
	}
	if entry.count >= limiter.limit {
		limiter.entries[key] = entry
		return false, 0, entry.reset
	}
	entry.count++
	limiter.entries[key] = entry
	return true, limiter.limit - entry.count, entry.reset
}

func authRateLimit(limiter *fixedWindowLimiter, next http.Handler) http.Handler {
	protected := map[string]struct{}{
		"/api/v1/auth/staff/login":      {},
		"/api/v1/auth/patient/login":    {},
		"/api/v1/auth/patient/register": {},
		"/api/v1/auth/refresh":          {},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := protected[r.URL.Path]; !ok {
			next.ServeHTTP(w, r)
			return
		}
		client := r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			client = host
		}
		now := time.Now()
		allowed, remaining, reset := limiter.allow("ip|"+client+"|"+r.URL.Path, now)
		identity, bodyOK := rateLimitIdentity(w, r)
		if !bodyOK {
			return
		}
		if allowed && identity != "" {
			identityAllowed, identityRemaining, identityReset := limiter.allow("identity|"+identity+"|"+r.URL.Path, now)
			if identityRemaining < remaining {
				remaining = identityRemaining
			}
			if !identityAllowed {
				allowed = false
				reset = identityReset
			}
		}
		w.Header().Set("RateLimit-Limit", strconv.Itoa(limiter.limit))
		w.Header().Set("RateLimit-Remaining", strconv.Itoa(remaining))
		w.Header().Set("RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		if !allowed {
			retryAfter := int(math.Ceil(time.Until(reset).Seconds()))
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Quá nhiều yêu cầu. Vui lòng thử lại sau.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func rateLimitIdentity(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Body == nil {
		return "", true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, httpx.MaxRequestBodyBytes+1))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Không thể đọc request body.")
		return "", false
	}
	if len(body) > httpx.MaxRequestBodyBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Request body vượt quá giới hạn 1 MiB.")
		return "", false
	}
	var payload struct {
		Username     string `json:"username"`
		PhoneNumber  string `json:"phone_number"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", true
	}
	identity := ""
	switch {
	case payload.Username != "":
		identity, _ = auth.NormalizeStaffUsername(payload.Username)
	case payload.PhoneNumber != "":
		identity, _ = auth.NormalizeVietnamPhone(payload.PhoneNumber)
	case payload.RefreshToken != "":
		identity = strings.TrimSpace(payload.RefreshToken)
	}
	if identity == "" {
		return "", true
	}
	hash := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(hash[:]), true
}
