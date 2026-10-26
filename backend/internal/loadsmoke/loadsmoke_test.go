package loadsmoke

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunConcurrentGate(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(http.StatusOK), nil
	})}
	result, err := run(context.Background(), Config{
		URL: "https://example.test/health/live", Requests: 200, Concurrency: 16,
		RequestTimeout: time.Second, MaxP95: time.Second, MaxErrorRate: 0,
	}, client)
	if err != nil || result.Failures != 0 || calls.Load() != 200 {
		t.Fatalf("load smoke result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
}

func TestRunFailsClosedOnThresholdAndUnsafeURL(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusServiceUnavailable), nil
	})}
	result, err := run(context.Background(), Config{
		URL: "https://example.test/health/live", Requests: 4, Concurrency: 2,
		RequestTimeout: time.Second, MaxP95: time.Second, MaxErrorRate: 0,
	}, client)
	if err == nil || result.Failures != 4 {
		t.Fatalf("failure threshold result=%+v err=%v", result, err)
	}
	_, err = Run(context.Background(), Config{
		URL: "https://user:secret@example.test/health?token=secret", Requests: 1, Concurrency: 1,
		RequestTimeout: time.Second, MaxP95: time.Second, MaxErrorRate: 0,
	})
	if err == nil {
		t.Fatal("unsafe target URL was accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
	}
}
