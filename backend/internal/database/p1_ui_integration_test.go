package database_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/clinic"
	"dncs/backend/internal/inventory"
	"dncs/backend/internal/patient"
	"dncs/backend/internal/staff"
)

func TestP1UIRolesPermissionAndScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, state := range []struct {
		status  string
		deleted bool
	}{{"ACTIVE", false}, {"INACTIVE", false}, {"ACTIVE", true}} {
		_, err := f.pool.Exec(ctx, `INSERT INTO roles(code,name,role_type,account_scope,status,is_deleted) VALUES($1,'Vai trò thử','CUSTOM','STAFF',$2,$3)`, strings.ToUpper(fmt.Sprintf("P1_TEST_%d_%s_%t", f.admin.UserID, state.status, state.deleted)), state.status, state.deleted)
		if err != nil {
			t.Fatal(err)
		}
	}
	store := authorization.NewStore(f.pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := authorization.NewMiddleware(store, logger).Require("user.role.assign", store.RolesHandler(logger))
	for _, c := range []struct {
		actor  auth.Principal
		query  string
		status int
	}{
		{f.admin, "?account_scope=STAFF", 200}, {f.admin, "?account_scope=PATIENT", 422},
		{f.doctor, "", 403}, {f.patientPrincipal, "", 403},
	} {
		r := httptest.NewRequest("GET", "/api/v1/roles"+c.query, nil)
		r = r.WithContext(auth.WithPrincipal(r.Context(), c.actor))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != c.status {
			t.Fatalf("role %s scope %s: %d", c.actor.RoleCode, c.query, w.Code)
		}
	}
	roles, err := store.AssignableStaffRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, role := range roles {
		if role.ID <= 0 || role.Name == "" || role.Code == "SUPER_ADMIN" || role.Code == "PATIENT" {
			t.Fatalf("invalid assignable role: %+v", role)
		}
		found[role.Code] = true
	}
	if !found[fmt.Sprintf("P1_TEST_%d_ACTIVE_FALSE", f.admin.UserID)] || found[fmt.Sprintf("P1_TEST_%d_INACTIVE_FALSE", f.admin.UserID)] || found[fmt.Sprintf("P1_TEST_%d_ACTIVE_TRUE", f.admin.UserID)] {
		t.Fatal("role lifecycle filter does not match assignable roles")
	}
	for _, code := range []string{"ADMIN", "DOCTOR", "CASHIER"} {
		if !found[code] {
			t.Fatalf("missing %s", code)
		}
	}
	writeP0Fixture(t, "p1_roles.json", map[string]any{"items": roles})
}

func TestP1UIProfileAccountVersionIsIndependent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE users SET version=version+5 WHERE id IN ($1,$2)`, f.doctor.UserID, f.patientPrincipal.UserID); err != nil {
		t.Fatal(err)
	}
	sp, err := staff.NewService(f.pool, auth.NewPasswordManager()).GetByUser(ctx, f.doctor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	pp, err := patient.NewService(f.pool, auth.NewPasswordManager()).GetOwn(ctx, f.patientPrincipal.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if sp.AccountVersion != 6 || sp.Version != 1 || sp.UserID != f.doctor.UserID || pp.AccountVersion != 6 || pp.Version != 1 || pp.UserID != f.patientPrincipal.UserID {
		t.Fatalf("independent versions: staff=%+v patient=%+v", sp, pp)
	}
	writeP0Fixture(t, "p1_staff.json", sp)
	writeP0Fixture(t, "p1_patient.json", pp)
}

func TestP1UIDoctorSpecialtyUsesRelationVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := clinic.NewService(f.pool)
	if _, err := f.pool.Exec(ctx, `UPDATE doctor_specialties SET version=4 WHERE doctor_profile_id=$1 AND specialty_id=$2`, f.doctorID, f.specialtyID); err != nil {
		t.Fatal(err)
	}
	items, err := svc.DoctorSpecialties(ctx, f.doctorID)
	if err != nil || len(items) != 1 {
		t.Fatalf("relations %v %v", items, err)
	}
	item := items[0]
	if item.RelationVersion == nil || *item.RelationVersion != 4 || item.Version != 1 {
		t.Fatalf("relation/specialty versions: %+v", item)
	}
	writeP0Fixture(t, "p1_doctor_specialties.json", map[string]any{"items": items})
	err = svc.RemoveDoctor(ctx, f.admin, f.doctorID, f.specialtyID, item.Version, "Review")
	assertProblemCode(t, err, "CONCURRENT_UPDATE")
	if err = svc.RemoveDoctor(ctx, f.admin, f.doctorID, f.specialtyID, *item.RelationVersion, "Review"); err != nil {
		t.Fatal(err)
	}
	items, err = svc.DoctorSpecialties(ctx, f.doctorID)
	if err != nil || len(items) != 0 {
		t.Fatalf("removed relation visible: %v %v", items, err)
	}
}

func TestP1UIBatchesFilterAndValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := inventory.NewService(f.pool)
	first, second := createMedicine(t, f), createMedicine(t, f)
	for _, m := range []inventory.Medicine{first, second} {
		_, err := svc.CreateReceipt(ctx, f.admin, inventory.ReceiptInput{SupplierName: "P1 test", ReceivedAt: time.Now().Format(time.RFC3339), IdempotencyKey: fmt.Sprintf("p1-ui-test-receipt-%d", m.ID), Items: []inventory.ReceiptItem{{MedicineID: m.ID, LotNumber: "P1-LOT", ExpiresOn: time.Now().AddDate(1, 0, 0).Format("2006-01-02"), Quantity: 10, UnitCostVND: 1000}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := svc.ListBatches(ctx, 0, first.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("filtered batches: %v %v", items, err)
	}
	if items[0]["medicine_id"] != first.ID {
		t.Fatalf("wrong medicine: %v", items)
	}
	writeP0Fixture(t, "p1_batches.json", map[string]any{"items": items})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := inventory.NewHandler(svc, logger)
	w := httptest.NewRecorder()
	h.Batches(w, httptest.NewRequest("GET", fmt.Sprintf("/api/v1/inventory/batches?medicine_id=%d", first.ID), nil))
	var response struct {
		Items []struct {
			MedicineID int64 `json:"medicine_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || len(response.Items) != 1 || response.Items[0].MedicineID != first.ID {
		t.Fatalf("HTTP filter response status=%d: %v", w.Code, err)
	}
	for _, q := range []string{"?medicine_id=bad", "?medicine_id=-1", "?id=0"} {
		w := httptest.NewRecorder()
		h.Batches(w, httptest.NewRequest("GET", "/api/v1/inventory/batches"+q, nil))
		if w.Code != 422 {
			t.Fatalf("query %s: %d", q, w.Code)
		}
	}
}
