package appointment

import (
	"testing"
	"time"
)

func TestBookingHashCanonicalizesInstant(t *testing.T) {
	utc := time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)
	local := time.Date(2026, 9, 10, 9, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	if bookingHash(1, 2, 3, utc) != bookingHash(1, 2, 3, local) {
		t.Fatal("same instant must produce the same idempotency request hash")
	}
	if bookingHash(1, 2, 3, utc) == bookingHash(1, 2, 4, utc) {
		t.Fatal("different payload must produce a different idempotency request hash")
	}
}

func TestTransitionMatrix(t *testing.T) {
	tests := []struct{ from, action, want string }{
		{"PENDING_APPROVAL", "approve", "CONFIRMED"},
		{"PENDING_APPROVAL", "reject", "REJECTED"},
		{"PENDING_APPROVAL", "cancel", "CANCELLED"},
		{"CONFIRMED", "cancel", "CANCELLED"},
		{"CONFIRMED", "check-in", "CHECKED_IN"},
		{"CONFIRMED", "no-show", "NO_SHOW"},
	}
	for _, test := range tests {
		got, err := transitionTarget(test.from, test.action)
		if err != nil || got != test.want {
			t.Fatalf("%s/%s: got %q, err=%v", test.from, test.action, got, err)
		}
	}
	if _, err := transitionTarget("CHECKED_IN", "cancel"); err == nil {
		t.Fatal("checked-in appointment must not be cancellable")
	}
}

func TestPrintableASCIIIdempotencyKey(t *testing.T) {
	if !printableASCII("booking-key-123456") {
		t.Fatal("expected printable ASCII key")
	}
	if printableASCII("booking-key-đ") || printableASCII("booking\nkey") {
		t.Fatal("non-ASCII/control key must be rejected")
	}
}
