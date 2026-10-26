package database_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/billing"
	"dncs/backend/internal/encounter"
	"dncs/backend/internal/inventory"
	"dncs/backend/internal/platform/pagination"
	"dncs/backend/internal/prescription"
	"dncs/backend/internal/settings"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestP2MedicineAndBatchPageBoundariesAndProjection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := inventory.NewService(f.pool, authorization.NewStore(f.pool))
	prefix := fmt.Sprintf("P2-%d", f.admin.UserID)
	ids := []int64{}
	price := int64(1000)
	for i := 0; i < 3; i++ {
		m, err := svc.CreateMedicine(ctx, f.admin, inventory.MedicineInput{Code: fmt.Sprintf("MED-%s-%d", prefix, i), Name: fmt.Sprintf("%s %d", prefix, i), InitialPrice: &price, ActiveIngredient: "Test", DosageForm: "Viên", Strength: "5 mg", DispenseUnit: "Viên", Status: "ACTIVE"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	for page := 1; page <= 3; page++ {
		result, err := svc.ListMedicinesPage(ctx, f.admin, true, prefix, "", page, 2)
		if err != nil {
			t.Fatal(err)
		}
		want := 2
		if page == 2 {
			want = 1
		}
		if page == 3 {
			want = 0
		}
		if len(result.Items) != want || result.HasMore != (page == 1) || result.Page != page || result.PageSize != 2 {
			t.Fatalf("medicine page %d: %+v", page, result)
		}
		if page == 2 && result.Items[0].ID != ids[2] {
			t.Fatal("offset used lookahead size")
		}
	}
	result, err := svc.ListMedicinesPage(ctx, f.doctor, false, prefix, "", 1, 1000)
	if err != nil || result.PageSize != 100 || len(result.Items) != 3 || result.HasMore {
		t.Fatalf("clamp: %+v %v", result, err)
	}
	for _, x := range result.Items {
		if x.Description != nil || x.CurrentPrice != nil || x.PhysicalQuantity != 0 || x.SellableQuantity != 0 {
			t.Fatal("inventory projection leaked")
		}
	}
	_, err = svc.CreateReceipt(ctx, f.admin, inventory.ReceiptInput{ReceivedAt: time.Now().Add(-time.Minute).Format(time.RFC3339), IdempotencyKey: "p2-receipt-create-" + prefix, Items: []inventory.ReceiptItem{
		{MedicineID: ids[0], LotNumber: "P2-A", ExpiresOn: time.Now().AddDate(0, 2, 0).Format("2006-01-02"), Quantity: 10, UnitCostVND: 1000},
		{MedicineID: ids[0], LotNumber: "P2-B", ExpiresOn: time.Now().AddDate(0, 2, 0).Format("2006-01-02"), Quantity: 10, UnitCostVND: 1000},
		{MedicineID: ids[0], LotNumber: "P2-C", ExpiresOn: time.Now().AddDate(0, 2, 0).Format("2006-01-02"), Quantity: 10, UnitCostVND: 1000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.ListBatchesPage(ctx, 0, ids[0], "", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	last, err := svc.ListBatchesPage(ctx, 0, ids[0], "", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || !first.HasMore || len(last.Items) != 1 || last.HasMore || first.Items[1]["id"] == last.Items[0]["id"] {
		t.Fatalf("batch pages: %+v %+v", first, last)
	}
	filtered, err := svc.ListBatchesPage(ctx, 0, ids[0], "P2-C", 1, 2)
	if err != nil || len(filtered.Items) != 1 || filtered.HasMore || filtered.Items[0]["lot_number"] != "P2-C" {
		t.Fatalf("server batch search: %+v %v", filtered, err)
	}
	empty, err := svc.ListBatchesPage(ctx, 0, ids[0], "missing", 1, 2)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.HasMore {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	handler := inventory.NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := p2HTTP(t, handler.ListMedicines, f.admin, "/api/v1/medicines?q="+prefix+"&page_size=2", 200)
	if body["has_more"] != true || body["page_size"] != float64(2) || len(body["items"].([]any)) != 2 {
		t.Fatalf("HTTP metadata: %+v", body)
	}
	body = p2HTTP(t, handler.Batches, f.admin, fmt.Sprintf("/api/v1/inventory/batches?medicine_id=%d&page_size=2&page=2", ids[0]), 200)
	if body["has_more"] != false || len(body["items"].([]any)) != 1 {
		t.Fatalf("batch HTTP: %+v", body)
	}
}

func TestP2PrescriptionScopeBeforePageAndTieBreak(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)
	ctx := context.Background()
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	createCatalogService(t, other, "CONSULTATION", &other.specialtyID, "ACTIVE")
	_, eid := draftEncounterWithCatalog(t, f)
	_, foreignEID := draftEncounterWithCatalog(t, other)
	svc := prescription.NewService(f.pool, authorization.NewStore(f.pool))
	med := createMedicine(t, f)
	visible := []int64{}
	for i := 0; i < 3; i++ {
		series, err := p2DraftSeries(t, f, eid)
		if err != nil {
			t.Fatal(err)
		}
		series, err = svc.GetSeries(ctx, f.doctor, series.ID)
		if err != nil {
			t.Fatal(err)
		}
		rx, err := svc.AddItem(ctx, f.doctor, series.Versions[0].ID, med.ID, 1, "Sau ăn", "", 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.Issue(ctx, f.doctor, rx.ID, rx.Version); err != nil {
			t.Fatal(err)
		}
		visible = append(visible, series.ID)
	}
	draft, err := p2DraftSeries(t, f, eid)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err = p2DraftSeries(t, other, foreignEID); err != nil {
			t.Fatal(err)
		}
	}
	for _, actor := range []auth.Principal{f.doctor, f.patientPrincipal} {
		first, err := svc.ListPage(ctx, actor, 0, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		last, err := svc.ListPage(ctx, actor, 0, 2, 2)
		if err != nil {
			t.Fatal(err)
		}
		expected := 3
		if actor.AccountType != auth.AccountTypePatient {
			expected = 4
		}
		if len(first.Items) != 2 || !first.HasMore || len(first.Items)+len(last.Items) != expected || last.HasMore {
			t.Fatalf("scoped pages: %+v %+v", first, last)
		}
		all := append(first.Items, last.Items...)
		for i, x := range all {
			if x.EncounterID != eid {
				t.Fatal("foreign series leaked")
			}
			if i > 0 && all[i-1].ID <= x.ID {
				t.Fatal("unstable tie order")
			}
			if actor.AccountType == auth.AccountTypePatient && x.ID == draft.ID {
				t.Fatal("draft leaked to patient")
			}
		}
		denied, err := svc.ListPage(ctx, actor, foreignEID, 1, 2)
		if err != nil || len(denied.Items) != 0 || denied.HasMore {
			t.Fatalf("foreign encounter: %+v %v", denied, err)
		}
	}
	// A single DB connection must suffice; reading series must release list rows.
	cfg := f.pool.Config().Copy()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	got, err := prescription.NewService(pool, authorization.NewStore(pool)).ListPage(timeout, f.patientPrincipal, eid, 1, 2)
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("single connection: %+v %v", got, err)
	}
	body := p2HTTP(t, prescription.NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))).Me, f.patientPrincipal, "/api/v1/me/prescriptions?page_size=2&page=2", 200)
	if body["has_more"] != false || len(body["items"].([]any)) != 1 {
		t.Fatalf("patient page response: %+v", body)
	}
}

func TestP2InvoicesPageAndOwnScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := billingService(f)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	for i := 0; i < 3; i++ {
		aid := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 70+i))
		_, _, err := encounter.NewService(f.pool, authorization.NewStore(f.pool)).Start(ctx, f.doctor, aid, 1)
		if err != nil {
			t.Fatal(err)
		}
		var eid int64
		if err = f.pool.QueryRow(ctx, `SELECT id FROM medical_encounters WHERE appointment_id=$1`, aid).Scan(&eid); err != nil {
			t.Fatal(err)
		}
		e := finalizeDraftEncounter(t, f, aid, eid)
		if _, err = svc.CreateInvoice(ctx, f.admin, e.ID, "CASH", fmt.Sprintf("p2-invoice-create-%d", eid)); err != nil {
			t.Fatal(err)
		}
	}
	for _, own := range []bool{false, true} {
		actor := f.admin
		if own {
			actor = f.patientPrincipal
		}
		first, err := svc.ListPage(ctx, actor, own, billing.ListFilter{PatientID: f.patientID, Page: 1, PageSize: 2})
		if err != nil {
			t.Fatal(err)
		}
		last, err := svc.ListPage(ctx, actor, own, billing.ListFilter{PatientID: f.patientID, Page: 2, PageSize: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Items) != 2 || !first.HasMore || len(last.Items) != 1 || last.HasMore || first.Items[1].ID <= last.Items[0].ID {
			t.Fatalf("invoice pages: %+v %+v", first, last)
		}
	}
	other := newFixture(t)
	empty, err := svc.ListPage(ctx, other.patientPrincipal, true, billing.ListFilter{PatientID: f.patientID, Page: 1, PageSize: 2})
	if err != nil || len(empty.Items) != 0 || empty.HasMore {
		t.Fatalf("own scope override: %+v %v", empty, err)
	}
	body := p2HTTP(t, billing.NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))).List(true), f.patientPrincipal, "/api/v1/me/invoices?page_size=2&page=2", 200)
	if body["has_more"] != false || len(body["items"].([]any)) != 1 {
		t.Fatalf("invoice HTTP: %+v", body)
	}
}

func TestP2AuditHumanLabelsLookupsAndAppRoleReadOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	name := fmt.Sprintf("Ngô P2 %d", f.doctor.UserID)
	if _, err := f.pool.Exec(ctx, `UPDATE staff_profiles SET full_name=$1,employment_status='INACTIVE',is_deleted=true WHERE user_id IN ($2,$3)`, name, f.doctor.UserID, f.doctor2.UserID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = audit.Insert(ctx, tx, audit.UserEvent(f.doctor.UserID, "P2_AUDIT", "P2_PROBE", "PATIENT", f.patientID)); err != nil {
			t.Fatal(err)
		}
	}
	if err = audit.Insert(ctx, tx, audit.UserEvent(f.doctor2.UserID, "P2_AUDIT", "P2_DUPLICATE_NAME", "PATIENT", f.patientID)); err != nil {
		t.Fatal(err)
	}
	if err = audit.Insert(ctx, tx, audit.SystemEvent("P2_AUDIT", "P2_SYSTEM", "PATIENT", f.patientID)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := f.pool.Config().Copy()
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error { _, err := c.Exec(ctx, "SET ROLE dncs_app"); return err }
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := settings.NewService(pool)
	var before, after int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	logs, cursor, err := svc.AuditLogs(ctx, settings.AuditFilter{Module: "P2_AUDIT", ActorUserID: f.doctor.UserID, PageSize: 1})
	if err != nil || len(logs) != 1 || cursor == "" || logs[0].ActorName != name || !strings.HasPrefix(logs[0].TargetLabel, "PAT-") {
		t.Fatalf("audit projection: %+v %v", logs, err)
	}
	detail, err := svc.AuditLog(ctx, logs[0].ID)
	if err != nil || detail.ActorName != name {
		t.Fatalf("detail: %+v %v", detail, err)
	}
	actors, err := svc.AuditActors(ctx, name)
	if err != nil || len(actors) != 2 || actors[0].Label != name || actors[1].Label != name || actors[0].Code == "" || actors[0].Code == actors[1].Code {
		t.Fatalf("actor lookup: %+v %v", actors, err)
	}
	targets, types, err := svc.AuditTargets(ctx, "PATIENT", logs[0].TargetLabel)
	if err != nil || len(targets) != 1 || targets[0].ID != f.patientID || len(types) == 0 {
		t.Fatalf("target dedup: %+v %v", targets, err)
	}
	system, _, err := svc.AuditLogs(ctx, settings.AuditFilter{Module: "P2_AUDIT", Action: "P2_SYSTEM", TargetType: "PATIENT", TargetID: f.patientID})
	if err != nil || len(system) != 1 || system[0].ActorName != "Hệ thống" {
		t.Fatalf("system fallback: %+v %v", system, err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := settings.NewHandler(svc, logger)
	mw := authorization.NewMiddleware(authorization.NewStore(pool), logger)
	for _, route := range []struct {
		path string
		fn   http.HandlerFunc
	}{{"audit-log-actors?q=" + fmt.Sprintf("P2%%20%d", f.doctor.UserID), h.AuditActors}, {"audit-log-targets?target_type=PATIENT", h.AuditTargets}} {
		guarded := mw.Require("audit_log.view", route.fn)
		for _, a := range []auth.Principal{f.doctor, f.patientPrincipal} {
			p2HTTP(t, guarded.ServeHTTP, a, "/api/v1/"+route.path, 403)
		}
		body := p2HTTP(t, guarded.ServeHTTP, f.admin, "/api/v1/"+route.path, 200)
		raw, _ := json.Marshal(body)
		for _, secret := range []string{"phone", "address", "clinical", "password", "account_number"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("lookup leaked %s", secret)
			}
		}
	}
	p2HTTP(t, h.AuditLogs, f.admin, "/api/v1/audit-logs?actor_user_id=abc", 422)
	p2HTTP(t, h.AuditLogs, f.admin, "/api/v1/audit-logs?target_id=-1", 422)
	p2HTTP(t, h.AuditActors, f.admin, "/api/v1/audit-log-actors?q="+strings.Repeat("x", 151), 422)
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&after); err != nil || after != before {
		t.Fatalf("read mutated audit: %d/%d %v", before, after, err)
	}
	writeP0Fixture(t, "p2_audit.json", map[string]any{"items": logs, "next_cursor": cursor})
}

func p2HTTP(t *testing.T, h http.HandlerFunc, actor auth.Principal, path string, status int) map[string]any {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r = r.WithContext(auth.WithPrincipal(r.Context(), actor))
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != status {
		t.Fatalf("%s: got %d want %d: %s", path, w.Code, status, w.Body.String())
	}
	body := map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestP2PageNormalization(t *testing.T) {
	for _, c := range []struct{ page, size, wantPage, wantSize int }{{0, 0, 1, 20}, {-1, -1, 1, 20}, {2, 1000, 2, 100}} {
		p, s := pagination.Normalize(c.page, c.size)
		if p != c.wantPage || s != c.wantSize {
			t.Fatalf("normalize %v", c)
		}
	}
}

// Seed identities with equal creation timestamps without editing immutable history.
func p2DraftSeries(t *testing.T, f fixture, eid int64) (prescription.Series, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return prescription.Series{}, err
	}
	defer tx.Rollback(ctx)
	var id int64
	var code string
	err = tx.QueryRow(ctx, `INSERT INTO prescription_series(encounter_id,created_by_user_id,created_at) VALUES($1,$2,'2026-10-09T00:00:00Z') RETURNING id,prescription_code`, eid, f.doctor.UserID).Scan(&id, &code)
	if err != nil {
		return prescription.Series{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO prescriptions(series_id,version_number,version_code,created_by_user_id) VALUES($1,1,$2||'-V1',$3)`, id, code, f.doctor.UserID)
	if err != nil {
		return prescription.Series{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return prescription.Series{}, err
	}
	return prescription.NewService(f.pool, authorization.NewStore(f.pool)).GetSeries(ctx, f.doctor, id)
}
