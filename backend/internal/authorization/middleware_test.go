package authorization

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"dncs/backend/internal/auth"
)

type fakeChecker struct {
	decision Decision
	err      error
	calls    int
}

func (checker *fakeChecker) Allowed(context.Context, auth.Principal, string) (Decision, error) {
	checker.calls++
	return checker.decision, checker.err
}

func TestRequirePermissionAllowsEffectiveGrant(t *testing.T) {
	checker := &fakeChecker{decision: Decision{Allowed: true, Source: "ROLE"}}
	middleware := NewMiddleware(checker, slog.New(slog.NewTextHandler(io.Discard, nil)))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := middleware.Require("patient.view.all", next)

	request := httptest.NewRequest(http.MethodGet, "/patients", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: 1, RoleID: 2}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || checker.calls != 1 {
		t.Fatalf("status=%d calls=%d", response.Code, checker.calls)
	}
}

func TestRequirePermissionDeniesByDefaultAndScope(t *testing.T) {
	checker := &fakeChecker{decision: Decision{Allowed: false, Source: "DEFAULT_DENY"}}
	middleware := NewMiddleware(checker, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := middleware.Require("patient.view.all", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("denied request reached handler")
	}))
	request := httptest.NewRequest(http.MethodGet, "/patients", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: 1}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("permission denial status=%d", response.Code)
	}

	checker.decision.Allowed = true
	handler = middleware.Require("patient.view.all", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("out-of-scope request reached handler")
	}), func(*http.Request, auth.Principal) bool { return false })
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("scope denial status=%d", response.Code)
	}
}

func TestRequirePermissionBlocksTemporaryPassword(t *testing.T) {
	checker := &fakeChecker{decision: Decision{Allowed: true}}
	middleware := NewMiddleware(checker, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := middleware.Require("role.view", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("temporary-password request reached handler")
	}))
	request := httptest.NewRequest(http.MethodGet, "/roles", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{
		UserID: 1, MustChangePassword: true,
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || checker.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, checker.calls)
	}
}
