package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONReturnsFieldSafeValidationErrors(t *testing.T) {
	for name, testCase := range map[string][3]string{
		"unknown": {`{"known":1,"secret":2}`, "secret", "UNKNOWN_FIELD"},
		"type":    {`{"known":"wrong"}`, "known", "INVALID_TYPE"},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(testCase[0]))
			response := httptest.NewRecorder()
			var destination struct {
				Known int `json:"known"`
			}
			if DecodeJSON(response, request, &destination) {
				t.Fatal("invalid JSON request was accepted")
			}
			var envelope ErrorBody
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Fields[testCase[1]] != testCase[2] {
				t.Fatalf("field errors=%v", envelope.Error.Fields)
			}
		})
	}
}

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"`+strings.Repeat("x", MaxRequestBodyBytes)+`"}`))
	response := httptest.NewRecorder()
	var destination struct {
		Value string `json:"value"`
	}
	if DecodeJSON(response, request, &destination) || response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized response status=%d body=%s", response.Code, response.Body.String())
	}
}
