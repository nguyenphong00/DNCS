package database_test

import (
	"context"
	"dncs/backend/internal/inventory"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/billing"
	"dncs/backend/internal/encounter"
	"dncs/backend/internal/prescription"
	"dncs/backend/internal/settings"
)

func TestP0PendingBillingPermissionSnapshotAndPaidDisappears(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	e := finalizedEncounter(t, f)
	svc := billingService(f)
	role := roleID(t, f.pool, "CASHIER")
	uid := insertStaffUser(t, f.pool, role, fmt.Sprintf("pending_cashier_%d", e.ID))
	cashier := auth.Principal{UserID: uid, RoleID: role, RoleCode: "CASHIER", AccountType: auth.AccountTypeStaff}
	for _, actor := range []auth.Principal{f.doctor, f.patientPrincipal} {
		_, err := svc.Pending(ctx, actor, billing.PendingFilter{})
		assertProblemCode(t, err, "FORBIDDEN")
	}
	items, err := svc.Pending(ctx, cashier, billing.PendingFilter{Query: e.EncounterCode, InvoiceState: "none", Page: 1, PageSize: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("pending: %+v %v", items, err)
	}
	preview := items[0]
	writeP0Fixture(t, "pending.json", map[string]any{"items": items, "page": 1, "page_size": 30})
	if preview.PatientName == "" || preview.PatientPhone == "" || preview.AppointmentCode == "" || preview.DoctorName == "" || preview.ExpectedTotalVND <= 0 {
		t.Fatalf("identity/totals: %+v", preview)
	}
	raw, _ := json.Marshal(preview)
	for _, field := range []string{"diagnosis", "chief_complaint", "clinical_notes", "conclusion", "vital"} {
		if strings.Contains(string(raw), field) {
			t.Fatalf("clinical field leaked: %s", field)
		}
	}
	inv, err := svc.CreateInvoice(ctx, cashier, e.ID, "CASH", fmt.Sprintf("pending-create-%d", e.ID))
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExpectedTotalVND != inv.TotalVND {
		t.Fatalf("preview=%d actual=%d", preview.ExpectedTotalVND, inv.TotalVND)
	}
	items, err = svc.Pending(ctx, cashier, billing.PendingFilter{Query: e.EncounterCode, InvoiceState: "unpaid"})
	if err != nil || len(items) != 1 || items[0].ExistingInvoice == nil || items[0].ExistingInvoice.ID != inv.ID || items[0].ExpectedTotalVND != inv.TotalVND {
		t.Fatalf("unpaid snapshot: %+v %v", items, err)
	}
	_, err = svc.Confirm(ctx, cashier, inv.ID, billing.ConfirmInput{ConfirmedAmountVND: inv.TotalVND, IdempotencyKey: fmt.Sprintf("pending-confirm-%d", inv.ID)})
	if err != nil {
		t.Fatal(err)
	}
	items, err = svc.Pending(ctx, cashier, billing.PendingFilter{Query: e.EncounterCode})
	if err != nil || len(items) != 0 {
		t.Fatalf("paid visible: %+v %v", items, err)
	}
}

func TestP0PendingMedicineBankTotalAndPrescriptionIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	aid := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 60))
	enc := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	e, _, err := enc.Start(ctx, f.doctor, aid, 1)
	if err != nil {
		t.Fatal(err)
	}
	rx := prescription.NewService(f.pool, authorization.NewStore(f.pool))
	series, err := rx.CreateSeries(ctx, f.doctor, e.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	series, err = rx.GetSeries(ctx, f.doctor, series.ID)
	if err != nil || series.EncounterCode != e.EncounterCode || series.PatientName != e.PatientNameAtStart || series.PatientCode != e.PatientCodeAtStart {
		t.Fatalf("identity: %+v %v", series, err)
	}
	writeP0Fixture(t, "encounter.json", e)
	writeP0Fixture(t, "series.json", series)
	med := createMedicine(t, f)
	writeP0Fixture(t, "medicines.json", map[string]any{"items": []any{med}})
	draft := series.Versions[0]
	draft, err = rx.AddItem(ctx, f.doctor, draft.ID, med.ID, 3, "Uống sau ăn", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rx.Issue(ctx, f.doctor, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	chief, diagnosis, conclusion := "Khám", "Chẩn đoán", "Kết luận"
	e, err = enc.Update(ctx, f.doctor, e.ID, encounter.UpdateInput{ExpectedVersion: e.Version, ChiefComplaint: &chief, Diagnosis: &diagnosis, Conclusion: &conclusion})
	if err != nil {
		t.Fatal(err)
	}
	pain := int16(0)
	_, err = enc.CreateVital(ctx, f.doctor, e.ID, encounter.VitalInput{PainScore: &pain})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = enc.Finalize(ctx, f.doctor, aid, 2, e.Version)
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.NewService(f.pool)
	bank, err := cfg.CreateBank(ctx, f.admin, "970415", "VietinBank", fmt.Sprintf("99%09d", e.ID), "PHÒNG KHÁM ĐẶNG ĐỨC")
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.ActivateBank(ctx, f.admin, bank.ID, bank.Version)
	if err != nil {
		t.Fatal(err)
	}
	svc := billingService(f)
	items, err := svc.Pending(ctx, f.admin, billing.PendingFilter{Query: e.EncounterCode})
	if err != nil || len(items) != 1 {
		t.Fatalf("pending: %+v %v", items, err)
	}
	if items[0].ExpectedMedicineTotalVND <= 0 {
		t.Fatal("missing medicine total")
	}
	inv, err := svc.CreateInvoice(ctx, f.admin, e.ID, "BANK_TRANSFER", fmt.Sprintf("pending-bank-create-%d", e.ID))
	if err != nil {
		t.Fatal(err)
	}
	if inv.TotalVND != items[0].ExpectedTotalVND {
		t.Fatalf("bank preview=%d actual=%d", items[0].ExpectedTotalVND, inv.TotalVND)
	}
	if inv.Payment.TransferContent == nil || len(*inv.Payment.TransferContent) > 25 {
		t.Fatal("transfer content too long")
	}
	writeP0Fixture(t, "bank_invoice.json", inv)
	qr, err := svc.PaymentQR(ctx, f.admin, inv.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	writeP0Fixture(t, "bank_qr.json", qr)
	filtered, err := svc.Pending(ctx, f.admin, billing.PendingFilter{Query: e.EncounterCode, DateFrom: time.Now().Add(24 * time.Hour)})
	if err != nil || len(filtered) != 0 {
		t.Fatalf("date filter: %v %v", filtered, err)
	}
}

func writeP0Fixture(t *testing.T, name string, value any) {
	t.Helper()
	dir := os.Getenv("DNCS_FIXTURE_OUTPUT")
	if dir == "" {
		return
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestP0PendingHTTPFiltersAndAppRoleReadOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	e := finalizedEncounter(t, f)
	cfg, err := pgxpool.ParseConfig(f.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE dncs_app")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	az := authorization.NewStore(pool)
	svc := billing.NewService(pool, az, inventory.NewService(pool, az), settings.NewService(pool))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := authorization.NewMiddleware(az, logger).Require("billing.pending_encounter.view", http.HandlerFunc(billing.NewHandler(svc, logger).Pending))
	cases := []struct {
		actor  auth.Principal
		query  string
		status int
	}{
		{f.doctor, "", 403}, {f.patientPrincipal, "", 403}, {f.admin, "?q=" + e.EncounterCode, 200},
		{f.admin, "?from=bad", 422}, {f.admin, "?from=2026-10-10&to=2026-10-09", 422}, {f.admin, "?doctor_id=-1", 422}, {f.admin, "?invoice_state=paid", 422},
		{f.admin, "?page=9223372036854775807&page_size=100", 422},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/api/v1/billing/pending-encounters"+c.query, nil)
		r = r.WithContext(auth.WithPrincipal(ctx, c.actor))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != c.status {
			t.Fatalf("query=%s status=%d expected=%d body=%s", c.query, w.Code, c.status, w.Body.String())
		}
	}
	before := 0
	after := 0
	f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&before)
	items, err := svc.Pending(ctx, f.admin, billing.PendingFilter{Query: e.EncounterCode, DoctorID: f.doctorID, Page: 1, PageSize: 1})
	if err != nil || len(items) != 1 {
		t.Fatalf("app projection: %+v %v", items, err)
	}
	items, err = svc.Pending(ctx, f.admin, billing.PendingFilter{Query: e.EncounterCode, Page: 2, PageSize: 1})
	if err != nil || len(items) != 0 {
		t.Fatalf("pagination: %+v %v", items, err)
	}
	f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&after)
	if before != after {
		t.Fatal("preview wrote audit")
	}
	var invoices int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE encounter_id=$1`, e.ID).Scan(&invoices); err != nil || invoices != 0 {
		t.Fatalf("preview wrote invoice: %d %v", invoices, err)
	}
}
