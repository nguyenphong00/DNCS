package database_test

import (
	"context"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/encounter"
	"dncs/backend/internal/inventory"
	"dncs/backend/internal/prescription"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func createMedicine(t *testing.T, f fixture) inventory.Medicine {
	t.Helper()
	n := nextFixtureID(t, f.pool)
	price := int64(25000)
	m, e := inventory.NewService(f.pool).CreateMedicine(context.Background(), f.admin, inventory.MedicineInput{Code: fmt.Sprintf("MED-IT-%d", n), Name: fmt.Sprintf("Integration medicine %d", n), ActiveIngredient: "Paracetamol", DosageForm: "Tablet", Strength: "500 mg", DispenseUnit: "Viên", Status: "ACTIVE", InitialPrice: &price})
	if e != nil {
		t.Fatal(e)
	}
	return m
}

func TestP4ReceiptAdjustmentLedgerAndIdempotency(t *testing.T) {
	f := newFixture(t)
	svc := inventory.NewService(f.pool)
	m := createMedicine(t, f)
	key := fmt.Sprintf("receipt-p4-%d", time.Now().UnixNano())
	in := inventory.ReceiptInput{SupplierName: "Supplier", ReceivedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), IdempotencyKey: key, Items: []inventory.ReceiptItem{{MedicineID: m.ID, LotNumber: "LOT-001", ExpiresOn: time.Now().AddDate(0, 1, 0).Format("2006-01-02"), Quantity: 10.5, UnitCostVND: 10000}}}
	d, e := svc.CreateReceipt(context.Background(), f.admin, in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := svc.CreateReceipt(context.Background(), f.admin, in)
	if e != nil || !again.Replayed || again.ID != d.ID {
		t.Fatalf("receipt replay: %+v %v", again, e)
	}
	var bid int64
	var balance float64
	if e = f.pool.QueryRow(context.Background(), `SELECT id,quantity_on_hand::float8 FROM medicine_batches WHERE medicine_id=$1`, m.ID).Scan(&bid, &balance); e != nil || balance != 10.5 {
		t.Fatalf("batch balance=%v err=%v", balance, e)
	}
	adj, e := svc.CreateAdjustment(context.Background(), f.admin, inventory.AdjustmentInput{ReasonCode: "DAMAGE", ReasonDetail: "Damaged during inspection", IdempotencyKey: fmt.Sprintf("adjustment-p4-%d", time.Now().UnixNano()), Items: []inventory.AdjustmentItem{{BatchID: bid, Direction: "OUT", Quantity: 2.5}}})
	if e != nil || adj.ID == 0 {
		t.Fatalf("adjustment: %+v %v", adj, e)
	}
	var movementCount int
	if e = f.pool.QueryRow(context.Background(), `SELECT b.quantity_on_hand::float8,count(sm.id) FROM medicine_batches b JOIN stock_movements sm ON sm.medicine_batch_id=b.id WHERE b.id=$1 GROUP BY b.id`, bid).Scan(&balance, &movementCount); e != nil || balance != 8 || movementCount != 2 {
		t.Fatalf("ledger mismatch balance=%v movements=%d err=%v", balance, movementCount, e)
	}
	manage, e := svc.CanManage(context.Background(), f.doctor)
	if e != nil || manage {
		t.Fatalf("doctor received medicine.manage projection: manage=%v err=%v", manage, e)
	}
	projected, e := svc.ListMedicines(context.Background(), f.doctor, false, "", "", 1, 20)
	if e != nil || len(projected) == 0 || projected[0].CurrentPrice != nil || projected[0].PhysicalQuantity != 0 || projected[0].SellableQuantity != 0 {
		t.Fatalf("doctor inventory projection leaked exact data: %+v err=%v", projected, e)
	}
	if _, e = f.pool.Exec(context.Background(), `UPDATE stock_movements SET quantity_after=quantity_after WHERE medicine_batch_id=$1`, bid); e == nil {
		t.Fatal("database allowed stock movement update")
	}
	_, e = svc.CreateAdjustment(context.Background(), f.admin, inventory.AdjustmentInput{ReasonCode: "DAMAGE", ReasonDetail: "Too much", IdempotencyKey: fmt.Sprintf("adjustment-over-p4-%d", time.Now().UnixNano()), Items: []inventory.AdjustmentItem{{BatchID: bid, Direction: "OUT", Quantity: 9}}})
	assertProblemCode(t, e, "INVENTORY_ADJUSTMENT_OUT_EXCEEDS_STOCK")
}

func TestP4ConcurrentAdjustmentCannotMakeBatchNegative(t *testing.T) {
	f := newFixture(t)
	svc := inventory.NewService(f.pool)
	m := createMedicine(t, f)
	d, e := svc.CreateReceipt(context.Background(), f.admin, inventory.ReceiptInput{ReceivedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), IdempotencyKey: fmt.Sprintf("receipt-race-p4-%d", time.Now().UnixNano()), Items: []inventory.ReceiptItem{{MedicineID: m.ID, LotNumber: "LOT-RACE", ExpiresOn: time.Now().AddDate(0, 1, 0).Format("2006-01-02"), Quantity: 5, UnitCostVND: 1}}})
	if e != nil || d.ID == 0 {
		t.Fatal(e)
	}
	var bid int64
	f.pool.QueryRow(context.Background(), `SELECT id FROM medicine_batches WHERE medicine_id=$1`, m.ID).Scan(&bid)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, e := svc.CreateAdjustment(context.Background(), f.admin, inventory.AdjustmentInput{ReasonCode: "DAMAGE", ReasonDetail: "Concurrent damage", IdempotencyKey: fmt.Sprintf("adjust-race-%d-%d", time.Now().UnixNano(), i), Items: []inventory.AdjustmentItem{{BatchID: bid, Direction: "OUT", Quantity: 4}}})
			results <- e
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	ok, fail := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("adjust race success=%d fail=%d", ok, fail)
	}
	var q float64
	f.pool.QueryRow(context.Background(), `SELECT quantity_on_hand::float8 FROM medicine_batches WHERE id=$1`, bid).Scan(&q)
	if q != 1 {
		t.Fatalf("race balance=%v", q)
	}
}

func TestP4FEFOAllocationAndFullRollback(t *testing.T) {
	f := newFixture(t)
	svc := inventory.NewService(f.pool)
	m := createMedicine(t, f)
	for i, days := range []int{20, 10} {
		_, err := svc.CreateReceipt(context.Background(), f.admin, inventory.ReceiptInput{
			ReceivedAt:     time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			IdempotencyKey: fmt.Sprintf("receipt-fefo-%d-%d", time.Now().UnixNano(), i),
			Items:          []inventory.ReceiptItem{{MedicineID: m.ID, LotNumber: fmt.Sprintf("FEFO-%d", i), ExpiresOn: time.Now().AddDate(0, 0, days).Format("2006-01-02"), Quantity: 5, UnitCostVND: 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := svc.AllocateStockForPayment(context.Background(), tx, []inventory.StockRequirement{{MedicineID: m.ID, ReferenceID: 900001, Quantity: 6}})
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(allocations) != 2 {
		t.Fatalf("expected two FEFO allocations, got %+v", allocations)
	}
	var first, second float64
	if err = f.pool.QueryRow(context.Background(), `SELECT min(quantity_on_hand)::float8,max(quantity_on_hand)::float8 FROM medicine_batches WHERE medicine_id=$1`, m.ID).Scan(&first, &second); err != nil || first != 0 || second != 4 {
		t.Fatalf("FEFO balances %v/%v err=%v", first, second, err)
	}
	tx, err = f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AllocateStockForPayment(context.Background(), tx, []inventory.StockRequirement{{MedicineID: m.ID, ReferenceID: 900002, Quantity: 5}})
	assertProblemCode(t, err, "INVENTORY_INSUFFICIENT_STOCK")
	_ = tx.Rollback(context.Background())
	var movementCount int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM stock_movements WHERE reference_type='INVOICE_ITEM' AND reference_id=900002`).Scan(&movementCount)
	if movementCount != 0 {
		t.Fatalf("insufficient allocation left %d movements", movementCount)
	}
}

func TestP4PrescriptionIssueReplacementAndPatientProjection(t *testing.T) {
	f := newFixture(t)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	aid := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 40))
	encSvc := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	enc, _, e := encSvc.Start(context.Background(), f.doctor, aid, 1)
	if e != nil {
		t.Fatal(e)
	}
	med := createMedicine(t, f)
	svc := prescription.NewService(f.pool, authorization.NewStore(f.pool))
	series, e := svc.CreateSeries(context.Background(), f.doctor, enc.ID, "Initial draft")
	if e != nil {
		t.Fatal(e)
	}
	draft := series.Versions[0]
	draft, e = svc.AddItem(context.Background(), f.doctor, draft.ID, med.ID, 10, "Uống ngày hai lần", "Sau ăn", 1)
	if e != nil {
		t.Fatal(e)
	}
	issued, e := svc.Issue(context.Background(), f.doctor, draft.ID, draft.Version)
	if e != nil || issued.Status != "ISSUED" {
		t.Fatalf("issue: %+v %v", issued, e)
	}
	if _, e = svc.Get(context.Background(), f.patientPrincipal, issued.ID); e != nil {
		t.Fatalf("patient issued projection: %v", e)
	}
	if _, e = f.pool.Exec(context.Background(), `UPDATE prescriptions SET overall_note='tampered',updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, issued.ID); e == nil {
		t.Fatal("database allowed issued prescription content update")
	}
	replacement, e := svc.Replacement(context.Background(), f.doctor, series.ID)
	if e != nil || len(replacement.Items) != 1 || replacement.Status != "DRAFT" {
		t.Fatalf("replacement: %+v %v", replacement, e)
	}
	replacement, e = svc.Issue(context.Background(), f.doctor, replacement.ID, replacement.Version)
	if e != nil {
		t.Fatal(e)
	}
	old, e := svc.Get(context.Background(), f.doctor, issued.ID)
	if e != nil || old.Status != "SUPERSEDED" || replacement.Status != "ISSUED" {
		t.Fatalf("version states old=%s new=%s err=%v", old.Status, replacement.Status, e)
	}
	var current int64
	if e = f.pool.QueryRow(context.Background(), `SELECT current_issued_prescription_id FROM prescription_series WHERE id=$1`, series.ID).Scan(&current); e != nil || current != replacement.ID {
		t.Fatalf("current=%d err=%v", current, e)
	}
}

func TestP4ReceiptOverLengthTextIsValidationNotServerError(t *testing.T) {
	f := newFixture(t)
	m := createMedicine(t, f)
	in := inventory.ReceiptInput{SupplierName: strings.Repeat("s", 201), ReceivedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), IdempotencyKey: fmt.Sprintf("receipt-long-%d", time.Now().UnixNano()), Items: []inventory.ReceiptItem{{MedicineID: m.ID, LotNumber: "LOT-LONG", ExpiresOn: time.Now().AddDate(0, 1, 0).Format("2006-01-02"), Quantity: 1, UnitCostVND: 1}}}
	_, err := inventory.NewService(f.pool).CreateReceipt(context.Background(), f.admin, in)
	assertProblemCode(t, err, "INVENTORY_RECEIPT_INVALID")
}

func TestP4PrescriptionItemOverLengthTextIsInstructionInvalid(t *testing.T) {
	f := newFixture(t)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	aid := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 41))
	enc, _, e := encounter.NewService(f.pool, authorization.NewStore(f.pool)).Start(context.Background(), f.doctor, aid, 1)
	if e != nil {
		t.Fatal(e)
	}
	med, other := createMedicine(t, f), createMedicine(t, f)
	svc := prescription.NewService(f.pool, authorization.NewStore(f.pool))
	series, e := svc.CreateSeries(context.Background(), f.doctor, enc.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	draft := series.Versions[0]
	longNotes, longInstruction := strings.Repeat("a", 501), strings.Repeat("a", 1001)

	_, e = svc.AddItem(context.Background(), f.doctor, draft.ID, med.ID, 10, "Uống ngày hai lần", longNotes, 1)
	assertProblemCode(t, e, "PRESCRIPTION_INSTRUCTION_INVALID")
	_, e = svc.AddItem(context.Background(), f.doctor, draft.ID, med.ID, 10, longInstruction, "", 1)
	assertProblemCode(t, e, "PRESCRIPTION_INSTRUCTION_INVALID")
	_, e = svc.AddItem(context.Background(), f.doctor, draft.ID, med.ID, 1000, "Uống ngày hai lần", "", 1)
	assertProblemCode(t, e, "PRESCRIPTION_QUANTITY_INVALID")
	var rows int
	if e = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM prescription_items WHERE prescription_id=$1`, draft.ID).Scan(&rows); e != nil || rows != 0 {
		t.Fatalf("rejected items must not be stored: rows=%d err=%v", rows, e)
	}

	maxNotes, maxInstruction := strings.Repeat("á", 500), strings.Repeat("á", 1000)
	draft, e = svc.AddItem(context.Background(), f.doctor, draft.ID, med.ID, 10, maxInstruction, maxNotes, 1)
	if e != nil {
		t.Fatalf("500-character notes and 1000-character instruction must be accepted: %v", e)
	}
	if len(draft.Items) != 1 || draft.Items[0].Notes == nil || *draft.Items[0].Notes != maxNotes || draft.Items[0].UsageInstruction != maxInstruction {
		t.Fatalf("item text not stored verbatim: %+v", draft.Items)
	}
	if draft, e = svc.AddItem(context.Background(), f.doctor, draft.ID, other.ID, 5, "Uống khi sốt", "  ", 2); e != nil {
		t.Fatal(e)
	}
	if len(draft.Items) != 2 || draft.Items[1].Notes != nil {
		t.Fatalf("blank notes must be stored as NULL: %+v", draft.Items)
	}

	item := draft.Items[0]
	_, e = svc.UpdateItem(context.Background(), f.doctor, draft.ID, item.ID, item.Version, 10, maxInstruction, longNotes, 1)
	assertProblemCode(t, e, "PRESCRIPTION_INSTRUCTION_INVALID")
	_, e = svc.UpdateItem(context.Background(), f.doctor, draft.ID, item.ID, item.Version, 10, longInstruction, maxNotes, 1)
	assertProblemCode(t, e, "PRESCRIPTION_INSTRUCTION_INVALID")
	_, e = svc.UpdateItem(context.Background(), f.doctor, draft.ID, item.ID, item.Version, 0, maxInstruction, maxNotes, 1)
	assertProblemCode(t, e, "PRESCRIPTION_QUANTITY_INVALID")
	var stored *string
	if e = f.pool.QueryRow(context.Background(), `SELECT notes FROM prescription_items WHERE id=$1`, item.ID).Scan(&stored); e != nil || stored == nil || *stored != maxNotes {
		t.Fatalf("rejected update must leave notes unchanged: %v err=%v", stored, e)
	}
	updated, e := svc.UpdateItem(context.Background(), f.doctor, draft.ID, item.ID, item.Version, 12, "Uống sau ăn", "Ghi chú mới", 1)
	if e != nil {
		t.Fatal(e)
	}
	if updated.Items[0].Notes == nil || *updated.Items[0].Notes != "Ghi chú mới" || updated.Items[0].Quantity != 12 {
		t.Fatalf("valid update not applied: %+v", updated.Items[0])
	}
}
