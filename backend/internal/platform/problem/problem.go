package problem

import (
	"errors"
	"log/slog"
	"net/http"

	"dncs/backend/internal/platform/httpx"
)

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string { return e.Code }

func New(status int, code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: status}
}

func As(err error) (*Error, bool) {
	var target *Error
	ok := errors.As(err, &target)
	return target, ok
}

func Write(w http.ResponseWriter, r *http.Request, logger *slog.Logger, operation string, err error) {
	if domainError, ok := As(err); ok {
		httpx.WriteError(w, domainError.HTTPStatus, domainError.Code, domainError.Message)
		return
	}
	logger.ErrorContext(r.Context(), operation, "error", err)
	httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Không thể xử lý yêu cầu.")
}
