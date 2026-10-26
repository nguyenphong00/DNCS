package auth

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type AccountType string

const (
	AccountTypeStaff   AccountType = "STAFF"
	AccountTypePatient AccountType = "PATIENT"
)

type Principal struct {
	UserID             int64       `json:"user_id"`
	SessionID          int64       `json:"session_id"`
	RoleID             int64       `json:"role_id"`
	RoleCode           string      `json:"role_code"`
	AccountType        AccountType `json:"account_type"`
	TokenVersion       int64       `json:"-"`
	MustChangePassword bool        `json:"must_change_password"`
}

type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	TokenType        string    `json:"token_type"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	MustChange       bool      `json:"must_change_password"`
}

type ClientInfo struct {
	IPAddress string
	UserAgent string
}

type ServiceError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *ServiceError) Error() string { return e.Code }

var (
	ErrInvalidCredentials = &ServiceError{
		Code: "AUTH_INVALID_CREDENTIALS", Message: "Thông tin đăng nhập không hợp lệ.", HTTPStatus: http.StatusUnauthorized,
	}
	ErrAccountInactive = &ServiceError{
		Code: "AUTH_ACCOUNT_INACTIVE", Message: "Tài khoản đã bị vô hiệu hóa.", HTTPStatus: http.StatusForbidden,
	}
	ErrAccountLocked = &ServiceError{
		Code: "AUTH_ACCOUNT_LOCKED", Message: "Tài khoản đang bị khóa.", HTTPStatus: http.StatusForbidden,
	}
	ErrSessionInvalid = &ServiceError{
		Code: "AUTH_SESSION_INVALID", Message: "Phiên đăng nhập không hợp lệ.", HTTPStatus: http.StatusUnauthorized,
	}
	ErrCurrentPasswordInvalid = &ServiceError{
		Code: "AUTH_CURRENT_PASSWORD_INVALID", Message: "Mật khẩu hiện tại không đúng.", HTTPStatus: http.StatusBadRequest,
	}
	ErrPasswordPolicy = &ServiceError{
		Code: "AUTH_PASSWORD_POLICY_VIOLATION", Message: "Mật khẩu phải dài từ 8 đến 128 ký tự và không được trùng mật khẩu hiện tại.", HTTPStatus: http.StatusBadRequest,
	}
	ErrMustChangePassword = &ServiceError{
		Code: "AUTH_MUST_CHANGE_PASSWORD", Message: "Bạn phải đổi mật khẩu trước khi dùng chức năng này.", HTTPStatus: http.StatusForbidden,
	}
	ErrAccountNotFound = &ServiceError{
		Code: "AUTH_ACCOUNT_NOT_FOUND", Message: "Không tìm thấy tài khoản.", HTTPStatus: http.StatusNotFound,
	}
	ErrAccountProtected = &ServiceError{
		Code: "AUTH_ACCOUNT_PROTECTED", Message: "Không được thao tác lên tài khoản được bảo vệ.", HTTPStatus: http.StatusForbidden,
	}
	ErrAccountSelfAdmin = &ServiceError{
		Code: "AUTH_ACCOUNT_SELF_ADMIN_FORBIDDEN", Message: "Không được dùng chức năng quản trị tài khoản cho chính mình.", HTTPStatus: http.StatusForbidden,
	}
	ErrAccountConcurrentUpdate = &ServiceError{
		Code: "AUTH_ACCOUNT_CONCURRENT_UPDATE", Message: "Tài khoản đã được cập nhật bởi request khác.", HTTPStatus: http.StatusConflict,
	}
	ErrAccountInvalidTransition = &ServiceError{
		Code: "AUTH_ACCOUNT_INVALID_STATUS_TRANSITION", Message: "Không thể chuyển tài khoản sang trạng thái yêu cầu.", HTTPStatus: http.StatusConflict,
	}
	ErrAccountAdminInput = &ServiceError{
		Code: "AUTH_ACCOUNT_ADMIN_INPUT_INVALID", Message: "expected_version và lý do là bắt buộc.", HTTPStatus: http.StatusUnprocessableEntity,
	}
)

func AsServiceError(err error) *ServiceError {
	var serviceError *ServiceError
	if errors.As(err, &serviceError) {
		return serviceError
	}
	return nil
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
