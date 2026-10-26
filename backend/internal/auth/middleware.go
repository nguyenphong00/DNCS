package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"dncs/backend/internal/platform/httpx"
)

type Authenticator struct {
	tokens TokenManager
	store  *Store
	logger *slog.Logger
}

func NewAuthenticator(tokens TokenManager, store *Store, logger *slog.Logger) *Authenticator {
	return &Authenticator{tokens: tokens, store: store, logger: logger}
}

func (authenticator *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Yêu cầu đăng nhập.")
			return
		}
		claims, err := authenticator.tokens.VerifyAccess(token)
		if err != nil {
			writeServiceError(w, ErrSessionInvalid)
			return
		}
		principal, err := authenticator.store.LoadPrincipal(r.Context(), claims)
		if err != nil {
			if serviceError := AsServiceError(err); serviceError != nil {
				writeServiceError(w, serviceError)
				return
			}
			authenticator.logger.ErrorContext(r.Context(), "authenticate request", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Không thể xử lý yêu cầu.")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
}

func RequirePasswordChanged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Yêu cầu đăng nhập.")
			return
		}
		if principal.MustChangePassword {
			writeServiceError(w, ErrMustChangePassword)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(value string) (string, bool) {
	parts := strings.Fields(value)
	returnValue := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		returnValue = parts[1]
	}
	return returnValue, returnValue != ""
}
