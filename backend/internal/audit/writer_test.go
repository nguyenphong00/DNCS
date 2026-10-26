package audit

import (
	"strings"
	"testing"
)

func TestMarshalJSONRejectsSensitiveAndOversizedPayloads(t *testing.T) {
	for name, payload := range map[string]any{
		"password":        map[string]any{"password": "do-not-store"},
		"nested token":    map[string]any{"safe": []any{map[string]any{"access_token": "do-not-store"}}},
		"clinical detail": map[string]any{"clinical_notes": "do-not-store"},
		"bank identity":   map[string]any{"account_number": "0123456789"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := marshalJSON(payload, false); err == nil || !strings.Contains(err.Error(), "prohibited field") {
				t.Fatalf("sensitive payload was accepted: %v", err)
			}
		})
	}
	if _, err := marshalJSON(map[string]any{"safe": strings.Repeat("x", 17*1024)}, false); err == nil || !strings.Contains(err.Error(), "16 KiB") {
		t.Fatalf("oversized payload was accepted: %v", err)
	}
}

func TestMarshalJSONAcceptsOperationalMetadata(t *testing.T) {
	payload := map[string]any{
		"invoice_id": 42, "payment_method": "CASH", "fields": []string{"status"},
		"from_version": 1, "to_version": 2,
	}
	encoded, err := marshalJSON(payload, false)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("safe operational metadata rejected: %s err=%v", encoded, err)
	}
}

func TestMarshalJSONRejectsNonAllowlistedField(t *testing.T) {
	if _, err := marshalJSON(map[string]any{"unexpected_detail": "value"}, false); err == nil || !strings.Contains(err.Error(), "non-allowlisted") {
		t.Fatalf("non-allowlisted payload was accepted: %v", err)
	}
}
