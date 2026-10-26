package authorization

import (
	"context"
	"log/slog"
	"net/http"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/httpx"
)

type Checker interface {
	Allowed(context.Context, auth.Principal, string) (Decision, error)
}

type ScopeCheck func(*http.Request, auth.Principal) bool

type Middleware struct {
	checker Checker
	logger  *slog.Logger
}

func NewMiddleware(checker Checker, logger *slog.Logger) *Middleware {
	return &Middleware{checker: checker, logger: logger}
}

func (middleware *Middleware) Require(
	permissionCode string,
	next http.Handler,
	scopeChecks ...ScopeCheck,
) http.Handler {
	return middleware.require([]string{permissionCode}, true, next, scopeChecks...)
}

func (middleware *Middleware) RequireAll(permissionCodes []string, next http.Handler, scopeChecks ...ScopeCheck) http.Handler {
	return middleware.require(permissionCodes, true, next, scopeChecks...)
}

func (middleware *Middleware) RequireAny(permissionCodes []string, next http.Handler, scopeChecks ...ScopeCheck) http.Handler {
	return middleware.require(permissionCodes, false, next, scopeChecks...)
}

func (middleware *Middleware) require(
	permissionCodes []string,
	requireAll bool,
	next http.Handler,
	scopeChecks ...ScopeCheck,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Yêu cầu đăng nhập.")
			return
		}
		if principal.MustChangePassword {
			httpx.WriteError(w, auth.ErrMustChangePassword.HTTPStatus, auth.ErrMustChangePassword.Code, auth.ErrMustChangePassword.Message)
			return
		}
		grantedCount := 0
		for _, permissionCode := range permissionCodes {
			decision, err := middleware.checker.Allowed(r.Context(), principal, permissionCode)
			if err != nil {
				middleware.logger.ErrorContext(r.Context(), "authorize request", "permission", permissionCode, "error", err)
				httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Không thể xử lý yêu cầu.")
				return
			}
			if decision.Allowed {
				grantedCount++
			}
		}
		allowed := (!requireAll && grantedCount > 0) || (requireAll && grantedCount == len(permissionCodes))
		if len(permissionCodes) == 0 || !allowed {
			httpx.WriteError(w, http.StatusForbidden, "AUTHZ_PERMISSION_DENIED", "Bạn không có quyền thực hiện chức năng này.")
			return
		}
		for _, check := range scopeChecks {
			if !check(r, principal) {
				httpx.WriteError(w, http.StatusForbidden, "AUTHZ_SCOPE_DENIED", "Dữ liệu nằm ngoài phạm vi được phép.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
