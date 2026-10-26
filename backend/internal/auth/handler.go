package auth

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"dncs/backend/internal/platform/httpx"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (handler *Handler) StaffLogin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	handler.login(w, r, AccountTypeStaff, request.Username, request.Password)
}

func (handler *Handler) PatientLogin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PhoneNumber string `json:"phone_number"`
		Password    string `json:"password"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	handler.login(w, r, AccountTypePatient, request.PhoneNumber, request.Password)
}

func (handler *Handler) login(
	w http.ResponseWriter,
	r *http.Request,
	accountType AccountType,
	identifier string,
	password string,
) {
	pair, err := handler.service.Login(r.Context(), accountType, identifier, password, ClientInfoFromRequest(r))
	if err != nil {
		handler.writeError(w, r, "login", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pair)
}

func (handler *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	pair, err := handler.service.Refresh(r.Context(), request.RefreshToken)
	if err != nil {
		handler.writeError(w, r, "refresh token", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pair)
}

func (handler *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	if err := handler.service.Logout(r.Context(), request.RefreshToken); err != nil {
		handler.writeError(w, r, "logout", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Yêu cầu đăng nhập.")
		return
	}
	if err := handler.service.LogoutAll(r.Context(), principal); err != nil {
		handler.writeError(w, r, "logout all", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Yêu cầu đăng nhập.")
		return
	}
	var request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	pair, err := handler.service.ChangePassword(
		r.Context(), principal, request.CurrentPassword, request.NewPassword, ClientInfoFromRequest(r),
	)
	if err != nil {
		handler.writeError(w, r, "change password", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pair)
}

func (handler *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	handler.accountAdminCommand(w, r, "reset password", handler.service.ResetPassword)
}

func (handler *Handler) LockAccount(w http.ResponseWriter, r *http.Request) {
	handler.accountAdminCommand(w, r, "lock account", handler.service.LockAccount)
}

func (handler *Handler) UnlockAccount(w http.ResponseWriter, r *http.Request) {
	handler.accountAdminCommand(w, r, "unlock account", handler.service.UnlockAccount)
}

func (handler *Handler) DeactivateAccount(w http.ResponseWriter, r *http.Request) {
	handler.accountAdminCommand(w, r, "deactivate account", handler.service.DeactivateAccount)
}

func (handler *Handler) accountAdminCommand(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	command func(context.Context, Principal, int64, AccountAdminInput) (AccountAdminResult, error),
) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Yêu cầu đăng nhập.")
		return
	}
	userID, ok := httpx.PathInt64(w, r, "userId")
	if !ok {
		return
	}
	var input AccountAdminInput
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	result, err := command(r.Context(), principal, userID, input)
	if err != nil {
		handler.writeError(w, r, operation, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (handler *Handler) writeError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if serviceError := AsServiceError(err); serviceError != nil {
		writeServiceError(w, serviceError)
		return
	}
	handler.logger.ErrorContext(r.Context(), operation, "error", err)
	httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Không thể xử lý yêu cầu.")
}

func writeServiceError(w http.ResponseWriter, serviceError *ServiceError) {
	httpx.WriteError(w, serviceError.HTTPStatus, serviceError.Code, serviceError.Message)
}

func ClientInfoFromRequest(r *http.Request) ClientInfo {
	ipAddress := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ipAddress = host
	}
	return ClientInfo{IPAddress: ipAddress, UserAgent: safeUserAgent(r.UserAgent())}
}

func safeUserAgent(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		value = value[:512]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}
