package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const MaxRequestBodyBytes = 1 << 20

type ErrorBody struct {
	Error     APIError `json:"error"`
	RequestID string   `json:"request_id,omitempty"`
}

func PathInt64(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	value, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || value < 1 {
		WriteError(w, http.StatusBadRequest, "INVALID_PATH_PARAMETER", "Path parameter không hợp lệ.")
		return 0, false
	}
	return value, true
}

func Pagination(r *http.Request) (page, pageSize int) {
	page = 1
	pageSize = 20
	if parsed, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && parsed > 0 {
		page = parsed
	}
	if parsed, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && parsed > 0 {
		pageSize = parsed
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

type APIError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			WriteError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Request body vượt quá giới hạn 1 MiB.")
			return false
		}
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) && typeError.Field != "" {
			WriteFieldError(w, http.StatusBadRequest, "INVALID_REQUEST", "Request body không hợp lệ.", typeError.Field, "INVALID_TYPE")
			return false
		}
		const unknownFieldPrefix = "json: unknown field "
		if strings.HasPrefix(err.Error(), unknownFieldPrefix) {
			field := strings.Trim(err.Error()[len(unknownFieldPrefix):], `"`)
			if len(field) > 200 {
				field = field[:200]
			}
			WriteFieldError(w, http.StatusBadRequest, "INVALID_REQUEST", "Request body chứa field không được hỗ trợ.", field, "UNKNOWN_FIELD")
			return false
		}
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Request body không hợp lệ.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Request body chỉ được chứa một JSON value.")
		return false
	}
	return true
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{
		Error:     APIError{Code: code, Message: message},
		RequestID: w.Header().Get("X-Request-ID"),
	})
}

func WriteFieldError(w http.ResponseWriter, status int, code, message, field, reason string) {
	WriteJSON(w, status, ErrorBody{
		Error: APIError{
			Code: code, Message: message,
			Fields: map[string]string{field: reason},
		},
		RequestID: w.Header().Get("X-Request-ID"),
	})
}
