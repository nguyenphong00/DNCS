package inventory

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeStrict(t *testing.T, raw string, destination any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		t.Fatalf("decode strict JSON contract: %v", err)
	}
}

func TestInventoryCommandJSONContractsUseSnakeCase(t *testing.T) {
	var update MedicineUpdate
	decodeStrict(t, `{"expected_version":3,"active_ingredient":"Paracetamol","low_stock_threshold":12.5}`, &update)
	if update.ExpectedVersion != 3 || update.ActiveIngredient == nil || *update.ActiveIngredient != "Paracetamol" {
		t.Fatalf("unexpected medicine update: %+v", update)
	}

	var receipt ReceiptInput
	decodeStrict(t, `{"supplier_name":"Supplier","supplier_document_ref":"INV-1","received_at":"2026-09-26T00:00:00Z","note":"ok","items":[{"medicine_id":1,"lot_number":"LOT-1","expires_on":"2027-01-01","received_quantity":2.5,"unit_purchase_cost_vnd":1000}]}`, &receipt)
	if receipt.SupplierName != "Supplier" || len(receipt.Items) != 1 || receipt.Items[0].Quantity != 2.5 {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}

	var adjustment AdjustmentInput
	decodeStrict(t, `{"reason_code":"DAMAGE","reason_detail":"Broken","correction_of_adjustment_id":9,"items":[{"medicine_batch_id":2,"direction":"OUT","quantity":1}]}`, &adjustment)
	if adjustment.ReasonCode != "DAMAGE" || adjustment.CorrectionOf == nil || *adjustment.CorrectionOf != 9 || len(adjustment.Items) != 1 {
		t.Fatalf("unexpected adjustment: %+v", adjustment)
	}
}
