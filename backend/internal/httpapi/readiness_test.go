package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadinessHandlerReturns503WhenSchemaIsNotReady(t *testing.T) {
	handler := readinessHandler(func(context.Context) error {
		return errors.New("required migration 202609010018 is missing")
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if body := response.Body.String(); body != "{\"error\":{\"code\":\"NOT_READY\",\"message\":\"Database chưa sẵn sàng.\"}}\n" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestReadinessHandlerReturns200WhenDatabaseAndSchemaAreReady(t *testing.T) {
	handler := readinessHandler(func(context.Context) error { return nil })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected body: %s", body)
	}
}
