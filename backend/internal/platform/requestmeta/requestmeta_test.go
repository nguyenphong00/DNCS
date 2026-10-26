package requestmeta

import (
	"context"
	"testing"
)

func TestContextRoundTrip(t *testing.T) {
	want := Metadata{RequestID: "request", CorrelationID: "correlation", IPHash: "hash"}
	ctx := WithContext(context.Background(), want)
	got, ok := FromContext(ctx)
	if !ok || got != want {
		t.Fatalf("metadata round trip: got=%+v ok=%v", got, ok)
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context unexpectedly contained request metadata")
	}
}
