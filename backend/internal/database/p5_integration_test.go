package database_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"dncs/backend/internal/authorization"
	"dncs/backend/internal/billing"
	"dncs/backend/internal/encounter"
	"dncs/backend/internal/inventory"
	"dncs/backend/internal/prescription"
	"dncs/backend/internal/settings"
)

func finalizedEncounter(t *testing.T, f fixture) encounter.Encounter {
	t.Helper()
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	return finalizedEncounterWithCatalog(t, f)
}

func finalizedEncounterWithCatalog(t *testing.T, f fixture) encounter.Encounter {
	t.Helper()
	appointmentID := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 50))
	service := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	started, _, err := service.Start(context.Background(), f.doctor, appointmentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	chief, diagnosis, conclusion := "Kiểm tra P5", "Chẩn đoán P5", "Hoàn tất P5"
	if _, err = service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: 1, ChiefComplaint: &chief, Diagnosis: &diagnosis, Conclusion: &conclusion}); err != nil {
		t.Fatal(err)
	}
	pain := int16(0)
	if _, err = service.CreateVital(context.Background(), f.doctor, started.ID, encounter.VitalInput{PainScore: &pain}); err != nil {
		t.Fatal(err)
	}
	finalized, replayed, err := service.Finalize(context.Background(), f.doctor, appointmentID, 2, 2)
	if err != nil || replayed || finalized.Status != "FINALIZED" {
		t.Fatalf("finalize encounter: %+v replayed=%v err=%v", finalized, replayed, err)
	}
	return finalized
}

func billingService(f fixture) *billing.Service {
	authz := authorization.NewStore(f.pool)
	return billing.NewService(f.pool, authz, inventory.NewService(f.pool, authz), settings.NewService(f.pool))
}

func TestP5SettingsAtomicVersionAndAudit(t *testing.T) {
	f := newFixture(t)
	service := settings.NewService(f.pool)
	current, err := service.Appointment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]int64{}
	input := settings.AppointmentInput{}
	for _, value := range current {
		versions[value.Key] = value.Version
		switch value.Key {
		case "appointment_auto_approval_enabled":
			input.AutoApprovalEnabled = !*value.BooleanValue
		case "appointment_booking_min_lead_minutes":
			input.BookingMinLeadMinutes = *value.IntegerValue
		case "appointment_booking_max_advance_days":
			input.BookingMaxAdvanceDays = *value.IntegerValue
		case "appointment_check_in_early_minutes":
			input.CheckInEarlyMinutes = *value.IntegerValue
		case "appointment_check_in_late_minutes":
			input.CheckInLateMinutes = *value.IntegerValue
		case "appointment_no_show_grace_minutes":
			input.NoShowGraceMinutes = *value.IntegerValue
		case "appointment_max_holding_future_per_patient":
			input.MaxHoldingFuturePerPatient = *value.IntegerValue
		}
	}
	input.ExpectedVersions = versions
	updated, err := service.UpdateAppointment(context.Background(), f.admin, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range updated {
		want := versions[value.Key]
		if value.Key == "appointment_auto_approval_enabled" {
			want++
		}
		if value.Version != want {
			t.Fatalf("unexpected version for %s: got %d want %d", value.Key, value.Version, want)
		}
	}
	_, err = service.UpdateAppointment(context.Background(), f.admin, input)
	assertProblemCode(t, err, "SETTING_CONCURRENT_UPDATE")
	invalid := input
	invalid.ExpectedVersions = map[string]int64{}
	for _, value := range updated {
		invalid.ExpectedVersions[value.Key] = value.Version
	}
	invalid.NoShowGraceMinutes = invalid.CheckInLateMinutes + 1
	_, err = service.UpdateAppointment(context.Background(), f.admin, invalid)
	assertProblemCode(t, err, "SETTING_VALUE_INVALID")
	var audits int
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE actor_user_id=$1 AND action_code='APPOINTMENT_SETTINGS_UPDATED'`, f.admin.UserID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("settings audit count=%d err=%v", audits, err)
	}
	if _, err = f.pool.Exec(context.Background(), `DELETE FROM system_settings WHERE setting_key='inventory_expiry_alert_days'`); err == nil {
		t.Fatal("database allowed system setting deletion")
	}
}

func TestP5BankHandoverRaceAndImmutableIdentity(t *testing.T) {
	f := newFixture(t)
	service := settings.NewService(f.pool)
	n := nextFixtureID(t, f.pool)
	first, err := service.CreateBank(context.Background(), f.admin, "970415", "VietinBank", fmt.Sprintf("10%08d", n), "DNCS CLINIC")
	if err != nil {
		t.Fatal(err)
	}
	first, err = service.ActivateBank(context.Background(), f.admin, first.ID, first.Version)
	if err != nil || first.Status != "ACTIVE" {
		t.Fatalf("activate first bank: %+v err=%v", first, err)
	}
	candidates := make([]settings.BankAccount, 2)
	for index := range candidates {
		candidates[index], err = service.CreateBank(context.Background(), f.admin, "970436", "Vietcombank", fmt.Sprintf("20%07d%d", n, index), "DNCS CLINIC")
		if err != nil {
			t.Fatal(err)
		}
	}
	barrier := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, candidate := range candidates {
		wait.Add(1)
		go func(item settings.BankAccount) {
			defer wait.Done()
			<-barrier
			_, err := service.ActivateBank(context.Background(), f.admin, item.ID, item.Version)
			results <- err
		}(candidate)
	}
	close(barrier)
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			assertProblemCode(t, err, "BANK_ACCOUNT_CONCURRENT_UPDATE")
		}
	}
	var active int
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM bank_accounts WHERE status='ACTIVE' AND NOT is_deleted`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active bank count=%d err=%v", active, err)
	}
	if _, err = f.pool.Exec(context.Background(), `UPDATE bank_accounts SET account_number='999999',version=version+1 WHERE id=$1`, first.ID); err == nil {
		t.Fatal("database allowed bank identity mutation")
	}
}

func TestP5AuditCursorFiltersAndAppendOnlyGuard(t *testing.T) {
	f := newFixture(t)
	service := settings.NewService(f.pool)
	n := nextFixtureID(t, f.pool)
	for index := range 2 {
		if _, err := service.CreateBank(context.Background(), f.admin, "970422", "MB Bank", fmt.Sprintf("60%07d%d", n, index), "DNCS AUDIT"); err != nil {
			t.Fatal(err)
		}
	}
	first, cursor, err := service.AuditLogs(context.Background(), settings.AuditFilter{Module: "audit_settings", Action: "bank_account_created", ActorUserID: f.admin.UserID, PageSize: 1})
	if err != nil || len(first) != 1 || cursor == "" {
		t.Fatalf("first audit page: items=%d cursor=%q err=%v", len(first), cursor, err)
	}
	second, next, err := service.AuditLogs(context.Background(), settings.AuditFilter{Module: "AUDIT_SETTINGS", Action: "BANK_ACCOUNT_CREATED", ActorUserID: f.admin.UserID, Cursor: cursor, PageSize: 1})
	if err != nil || len(second) != 1 || second[0].ID == first[0].ID || next != "" {
		t.Fatalf("second audit page: first=%+v second=%+v next=%q err=%v", first, second, next, err)
	}
	if _, err = service.AuditLog(context.Background(), first[0].ID); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.AuditLogs(context.Background(), settings.AuditFilter{Cursor: "not-a-valid-cursor"})
	assertProblemCode(t, err, "AUDIT_CURSOR_INVALID")
	_, _, err = service.AuditLogs(context.Background(), settings.AuditFilter{From: time.Now().AddDate(-2, 0, 0), To: time.Now()})
	assertProblemCode(t, err, "AUDIT_FILTER_INVALID")
	if _, err = f.pool.Exec(context.Background(), `UPDATE audit_logs SET metadata=metadata WHERE id=$1`, first[0].ID); err == nil {
		t.Fatal("database allowed audit log mutation")
	}
}

func TestP5CashInvoiceIdempotencyAndConfirmation(t *testing.T) {
	f := newFixture(t)
	finalized := finalizedEncounter(t, f)
	service := billingService(f)
	key := fmt.Sprintf("invoice-create-p5-%d", time.Now().UnixNano())
	created, err := service.CreateInvoice(context.Background(), f.admin, finalized.ID, "CASH", key)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "UNPAID" || created.TotalVND != 150000 || len(created.Items) != 1 || created.Payment == nil || created.Payment.Status != "PENDING_CONFIRMATION" {
		t.Fatalf("unexpected invoice aggregate: %+v", created)
	}
	replayed, err := service.CreateInvoice(context.Background(), f.admin, finalized.ID, "CASH", key)
	if err != nil || !replayed.IdempotentlyReplayed || replayed.ID != created.ID {
		t.Fatalf("invoice replay: %+v err=%v", replayed, err)
	}
	confirmKey := fmt.Sprintf("payment-confirm-p5-%d", time.Now().UnixNano())
	paid, err := service.Confirm(context.Background(), f.admin, created.ID, billing.ConfirmInput{ConfirmedAmountVND: created.TotalVND, ExternalReference: "cash receipt", IdempotencyKey: confirmKey})
	if err != nil || paid.Status != "PAID" || paid.Payment == nil || paid.Payment.Status != "CONFIRMED" {
		t.Fatalf("confirm cash: %+v err=%v", paid, err)
	}
	paidReplay, err := service.Confirm(context.Background(), f.admin, created.ID, billing.ConfirmInput{ConfirmedAmountVND: created.TotalVND, ExternalReference: "cash receipt", IdempotencyKey: confirmKey})
	if err != nil || !paidReplay.IdempotentlyReplayed {
		t.Fatalf("confirm replay: %+v err=%v", paidReplay, err)
	}
	_, err = service.Confirm(context.Background(), f.admin, created.ID, billing.ConfirmInput{ConfirmedAmountVND: created.TotalVND, IdempotencyKey: fmt.Sprintf("payment-confirm-new-p5-%d", time.Now().UnixNano())})
	assertProblemCode(t, err, "PAYMENT_ALREADY_CONFIRMED")
	_, err = service.CreateInvoice(context.Background(), f.admin, finalized.ID, "CASH", fmt.Sprintf("invoice-create-next-p5-%d", time.Now().UnixNano()))
	assertProblemCode(t, err, "INVOICE_NO_BILLABLE_ITEMS")
	if _, err = f.pool.Exec(context.Background(), `UPDATE invoice_items SET line_total_vnd=line_total_vnd WHERE invoice_id=$1`, created.ID); err == nil {
		t.Fatal("database allowed invoice item mutation")
	}
}

func TestP5ConcurrentInvoiceAndPaymentCommandsHaveOneWinner(t *testing.T) {
	f := newFixture(t)
	finalized := finalizedEncounter(t, f)
	service := billingService(f)
	barrier := make(chan struct{})
	type createResult struct {
		invoice billing.Invoice
		err     error
	}
	creates := make(chan createResult, 2)
	var wait sync.WaitGroup
	for index := range 2 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-barrier
			invoice, err := service.CreateInvoice(context.Background(), f.admin, finalized.ID, "CASH", fmt.Sprintf("invoice-race-p5-%d-%d", time.Now().UnixNano(), index))
			creates <- createResult{invoice: invoice, err: err}
		}(index)
	}
	close(barrier)
	wait.Wait()
	close(creates)
	var created billing.Invoice
	createSuccess, createConflict := 0, 0
	for result := range creates {
		if result.err == nil {
			createSuccess++
			created = result.invoice
		} else {
			assertProblemCode(t, result.err, "INVOICE_UNPAID_ALREADY_EXISTS")
			createConflict++
		}
	}
	if createSuccess != 1 || createConflict != 1 {
		t.Fatalf("invoice race success=%d conflict=%d", createSuccess, createConflict)
	}

	barrier = make(chan struct{})
	confirms := make(chan error, 2)
	for index := range 2 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-barrier
			_, err := service.Confirm(context.Background(), f.admin, created.ID, billing.ConfirmInput{ConfirmedAmountVND: created.TotalVND, IdempotencyKey: fmt.Sprintf("confirm-race-p5-%d-%d", time.Now().UnixNano(), index)})
			confirms <- err
		}(index)
	}
	close(barrier)
	wait.Wait()
	close(confirms)
	confirmSuccess, confirmConflict := 0, 0
	for err := range confirms {
		if err == nil {
			confirmSuccess++
		} else {
			assertProblemCode(t, err, "PAYMENT_ALREADY_CONFIRMED")
			confirmConflict++
		}
	}
	if confirmSuccess != 1 || confirmConflict != 1 {
		t.Fatalf("payment race success=%d conflict=%d", confirmSuccess, confirmConflict)
	}
	var invoiceCount, itemCount, paymentCount, auditCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM invoices WHERE encounter_id=$1`, finalized.ID).Scan(&invoiceCount); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM invoice_items WHERE invoice_id=$1`, created.ID).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE invoice_id=$1`, created.ID).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_code='PAYMENT_CONFIRMED' AND target_id=(SELECT id FROM payments WHERE invoice_id=$1)`, created.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if invoiceCount != 1 || itemCount != 1 || paymentCount != 1 || auditCount != 1 {
		t.Fatalf("race duplicates invoice=%d item=%d payment=%d audit=%d", invoiceCount, itemCount, paymentCount, auditCount)
	}
}

func addIssuedPrescription(t *testing.T, f fixture, encounterID int64, medicine inventory.Medicine, quantity int16) {
	t.Helper()
	service := prescription.NewService(f.pool, authorization.NewStore(f.pool))
	series, err := service.CreateSeries(context.Background(), f.doctor, encounterID, "Billing P5")
	if err != nil {
		t.Fatal(err)
	}
	draft := series.Versions[0]
	draft, err = service.AddItem(context.Background(), f.doctor, draft.ID, medicine.ID, quantity, "Uống theo chỉ định", "Billing P5", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Issue(context.Background(), f.doctor, draft.ID, draft.Version); err != nil {
		t.Fatal(err)
	}
}

func draftEncounterWithCatalog(t *testing.T, f fixture) (int64, int64) {
	t.Helper()
	appointmentID := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 51))
	started, _, err := encounter.NewService(f.pool, authorization.NewStore(f.pool)).Start(context.Background(), f.doctor, appointmentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return appointmentID, started.ID
}

func finalizeDraftEncounter(t *testing.T, f fixture, appointmentID, encounterID int64) encounter.Encounter {
	t.Helper()
	service := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	chief, diagnosis, conclusion := "Có thuốc P5", "Điều trị P5", "Hoàn tất P5"
	if _, err := service.Update(context.Background(), f.doctor, encounterID, encounter.UpdateInput{ExpectedVersion: 1, ChiefComplaint: &chief, Diagnosis: &diagnosis, Conclusion: &conclusion}); err != nil {
		t.Fatal(err)
	}
	pain := int16(0)
	if _, err := service.CreateVital(context.Background(), f.doctor, encounterID, encounter.VitalInput{PainScore: &pain}); err != nil {
		t.Fatal(err)
	}
	finalized, _, err := service.Finalize(context.Background(), f.doctor, appointmentID, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	return finalized
}

func TestP5MedicineDeductsOnlyAtConfirmWithFEFOAndRollback(t *testing.T) {
	f := newFixture(t)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	medicine := createMedicine(t, f)
	inventoryService := inventory.NewService(f.pool)
	for index, days := range []int{20, 10} {
		_, err := inventoryService.CreateReceipt(context.Background(), f.admin, inventory.ReceiptInput{ReceivedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), IdempotencyKey: fmt.Sprintf("receipt-billing-p5-%d-%d", time.Now().UnixNano(), index), Items: []inventory.ReceiptItem{{MedicineID: medicine.ID, LotNumber: fmt.Sprintf("BILL-P5-%d", index), ExpiresOn: time.Now().AddDate(0, 0, days).Format("2006-01-02"), Quantity: 5, UnitCostVND: 1}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	appointmentID, encounterID := draftEncounterWithCatalog(t, f)
	addIssuedPrescription(t, f, encounterID, medicine, 6)
	finalized := finalizeDraftEncounter(t, f, appointmentID, encounterID)
	service := billingService(f)
	created, err := service.CreateInvoice(context.Background(), f.admin, finalized.ID, "CASH", fmt.Sprintf("invoice-medicine-p5-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	var before float64
	if err = f.pool.QueryRow(context.Background(), `SELECT sum(quantity_on_hand)::float8 FROM medicine_batches WHERE medicine_id=$1`, medicine.ID).Scan(&before); err != nil || before != 10 {
		t.Fatalf("invoice creation reserved stock: quantity=%v err=%v", before, err)
	}
	paid, err := service.Confirm(context.Background(), f.admin, created.ID, billing.ConfirmInput{ConfirmedAmountVND: created.TotalVND, IdempotencyKey: fmt.Sprintf("confirm-medicine-p5-%d", time.Now().UnixNano())})
	if err != nil || paid.Status != "PAID" {
		t.Fatalf("confirm medicine invoice: %+v err=%v", paid, err)
	}
	var minimum, maximum float64
	if err = f.pool.QueryRow(context.Background(), `SELECT min(quantity_on_hand)::float8,max(quantity_on_hand)::float8 FROM medicine_batches WHERE medicine_id=$1`, medicine.ID).Scan(&minimum, &maximum); err != nil || minimum != 0 || maximum != 4 {
		t.Fatalf("FEFO balances=%v/%v err=%v", minimum, maximum, err)
	}

	appointmentID, encounterID = draftEncounterWithCatalog(t, f)
	addIssuedPrescription(t, f, encounterID, medicine, 5)
	finalized = finalizeDraftEncounter(t, f, appointmentID, encounterID)
	shortage, err := service.CreateInvoice(context.Background(), f.admin, finalized.ID, "CASH", fmt.Sprintf("invoice-shortage-p5-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Confirm(context.Background(), f.admin, shortage.ID, billing.ConfirmInput{ConfirmedAmountVND: shortage.TotalVND, IdempotencyKey: fmt.Sprintf("confirm-shortage-p5-%d", time.Now().UnixNano())})
	assertProblemCode(t, err, "INVENTORY_INSUFFICIENT_STOCK")
	var invoiceStatus, paymentStatus string
	if err = f.pool.QueryRow(context.Background(), `SELECT i.status::text,p.status::text FROM invoices i JOIN payments p ON p.invoice_id=i.id WHERE i.id=$1`, shortage.ID).Scan(&invoiceStatus, &paymentStatus); err != nil || invoiceStatus != "UNPAID" || paymentStatus != "PENDING_CONFIRMATION" {
		t.Fatalf("shortage did not rollback financial state: %s/%s err=%v", invoiceStatus, paymentStatus, err)
	}
	var remaining float64
	if err = f.pool.QueryRow(context.Background(), `SELECT sum(quantity_on_hand)::float8 FROM medicine_batches WHERE medicine_id=$1`, medicine.ID).Scan(&remaining); err != nil || remaining != 4 {
		t.Fatalf("shortage did not rollback stock: %v err=%v", remaining, err)
	}
}

func TestP5BankInvoiceKeepsSnapshotAcrossHandover(t *testing.T) {
	f := newFixture(t)
	finalized := finalizedEncounter(t, f)
	config := settings.NewService(f.pool)
	n := nextFixtureID(t, f.pool)
	first, err := config.CreateBank(context.Background(), f.admin, "970418", "BIDV", fmt.Sprintf("30%08d", n), "DNCS RECEIVER ONE")
	if err != nil {
		t.Fatal(err)
	}
	first, err = config.ActivateBank(context.Background(), f.admin, first.ID, first.Version)
	if err != nil {
		t.Fatal(err)
	}
	service := billingService(f)
	created, err := service.CreateInvoice(context.Background(), f.admin, finalized.ID, "BANK_TRANSFER", fmt.Sprintf("invoice-bank-p5-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	if created.Payment == nil || created.Payment.BankAccountIDAtCreation == nil || *created.Payment.BankAccountIDAtCreation != first.ID || created.Payment.QRPayload == nil {
		t.Fatalf("missing bank snapshot: %+v", created.Payment)
	}
	second, err := config.CreateBank(context.Background(), f.admin, "970407", "Techcombank", fmt.Sprintf("40%08d", n), "DNCS RECEIVER TWO")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.ActivateBank(context.Background(), f.admin, second.ID, second.Version); err != nil {
		t.Fatal(err)
	}
	after, err := service.Invoice(context.Background(), f.admin, created.ID, false)
	if err != nil || after.Payment == nil || after.Payment.BankAccountIDAtCreation == nil || *after.Payment.BankAccountIDAtCreation != first.ID || *after.Payment.AccountNumberAtCreation != first.AccountNumber {
		t.Fatalf("bank snapshot changed after handover: %+v err=%v", after.Payment, err)
	}
	qr, err := service.PaymentQR(context.Background(), f.patientPrincipal, created.ID, true)
	if err != nil || qr["bank_bin"] == nil {
		t.Fatalf("patient own QR: %+v err=%v", qr, err)
	}
}

func TestP5BankReferenceIsNormalizedAndUniquePerSnapshotAccount(t *testing.T) {
	f := newFixture(t)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	firstEncounter := finalizedEncounterWithCatalog(t, f)
	secondEncounter := finalizedEncounterWithCatalog(t, f)
	config := settings.NewService(f.pool)
	n := nextFixtureID(t, f.pool)
	bank, err := config.CreateBank(context.Background(), f.admin, "970432", "VPBank", fmt.Sprintf("50%08d", n), "DNCS PAYMENT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.ActivateBank(context.Background(), f.admin, bank.ID, bank.Version); err != nil {
		t.Fatal(err)
	}
	service := billingService(f)
	first, err := service.CreateInvoice(context.Background(), f.admin, firstEncounter.ID, "BANK_TRANSFER", fmt.Sprintf("invoice-bank-ref-a-p5-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateInvoice(context.Background(), f.admin, secondEncounter.ID, "BANK_TRANSFER", fmt.Sprintf("invoice-bank-ref-b-p5-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Confirm(context.Background(), f.admin, first.ID, billing.ConfirmInput{ConfirmedAmountVND: first.TotalVND, BankTransactionReference: "  Tx-P5-Unique-001  ", IdempotencyKey: fmt.Sprintf("confirm-bank-ref-a-p5-%d", time.Now().UnixNano())}); err != nil {
		t.Fatal(err)
	}
	_, err = service.Confirm(context.Background(), f.admin, second.ID, billing.ConfirmInput{ConfirmedAmountVND: second.TotalVND, BankTransactionReference: "tx-p5-unique-001", IdempotencyKey: fmt.Sprintf("confirm-bank-ref-b-p5-%d", time.Now().UnixNano())})
	assertProblemCode(t, err, "PAYMENT_BANK_REFERENCE_EXISTS")
	var invoiceStatus, paymentStatus string
	if err = f.pool.QueryRow(context.Background(), `SELECT i.status::text,p.status::text FROM invoices i JOIN payments p ON p.invoice_id=i.id WHERE i.id=$1`, second.ID).Scan(&invoiceStatus, &paymentStatus); err != nil || invoiceStatus != "UNPAID" || paymentStatus != "PENDING_CONFIRMATION" {
		t.Fatalf("duplicate bank reference changed state: %s/%s err=%v", invoiceStatus, paymentStatus, err)
	}
}
