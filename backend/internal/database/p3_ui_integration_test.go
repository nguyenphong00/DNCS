package database_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dncs/backend/internal/appointment"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/billing"
	"dncs/backend/internal/clinic"
	"dncs/backend/internal/encounter"
	"dncs/backend/internal/patient"
	"dncs/backend/internal/platform/pagination"
	"dncs/backend/internal/scheduling"
	"dncs/backend/internal/servicecatalog"
	"dncs/backend/internal/staff"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise both sides of each page boundary and compare IDs, including a page
// beyond the end. This catches offset calculations accidentally using size+1.
func p3CheckPages[T any](t *testing.T, total, size int, query func(int, int) (pagination.Page[T], error)) {
	t.Helper()
	seen := map[float64]bool{}
	for page := 1; page <= (total+size-1)/size+1; page++ {
		result, err := query(page, size)
		if err != nil {
			t.Fatal(err)
		}
		remaining := max(0, total-(page-1)*size)
		want := min(size, remaining)
		if len(result.Items) != want || result.Items == nil || result.HasMore != (remaining > size) || result.Page != page || result.PageSize != size {
			t.Fatalf("page=%d size=%d expected=%d got=%+v", page, size, want, result)
		}
		for _, item := range result.Items {
			raw, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]any
			if err = json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			id, ok := record["id"].(float64)
			if !ok {
				id = record["encounter_id"].(float64)
			}
			if seen[id] {
				t.Fatalf("duplicate ID %v across pages", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != total {
		t.Fatalf("read %d distinct IDs, expected %d", len(seen), total)
	}
}

func TestP3UIPatientsAndStaffBeyondFirst50(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("p3ui%d", f.admin.UserID)
	tied := time.Now().Add(-time.Hour).UTC()
	patientIDs := []int64{}
	staffIDs := []int64{}
	for i := 0; i < 52; i++ {
		_, id := insertPatient(t, f.pool, roleID(t, f.pool, "PATIENT"), nextFixtureID(t, f.pool), f.admin.UserID)
		// Names are mutable; created_at remains the INSERT timestamp.
		if _, err := f.pool.Exec(ctx, `UPDATE patients SET full_name=$2,normalized_full_name=$2 WHERE id=$1`, id, fmt.Sprintf("%s patient %02d", prefix, i)); err != nil {
			t.Fatal(err)
		}
		patientIDs = append(patientIDs, id)
	}
	for i := 0; i < 51; i++ {
		uid := insertStaffUser(t, f.pool, roleID(t, f.pool, "ADMIN"), fmt.Sprintf("%s.staff.%02d", prefix, i))
		var id int64
		if err := f.pool.QueryRow(ctx, `INSERT INTO staff_profiles(user_id,full_name,created_by,updated_by,created_at) VALUES($1,$2,$3,$3,$4) RETURNING id`, uid, prefix+" staff", f.admin.UserID, tied).Scan(&id); err != nil {
			t.Fatal(err)
		}
		staffIDs = append(staffIDs, id)
	}
	p := patient.NewService(f.pool, auth.NewPasswordManager())
	s := staff.NewService(f.pool, auth.NewPasswordManager())
	for _, size := range []int{1, 2, 50, 100} {
		t.Run(fmt.Sprintf("patient-size-%d", size), func(t *testing.T) {
			p3CheckPages(t, 52, size, func(page, size int) (pagination.Page[patient.Profile], error) {
				return p.ListPage(ctx, f.admin, prefix, page, size)
			})
		})
		t.Run(fmt.Sprintf("staff-size-%d", size), func(t *testing.T) {
			p3CheckPages(t, 51, size, func(page, size int) (pagination.Page[staff.Profile], error) {
				return s.ListPage(ctx, prefix, "ACTIVE", false, page, size)
			})
		})
	}
	exact, err := p.ListPage(ctx, f.admin, prefix+" patient 51", 1, 1)
	if err != nil || len(exact.Items) != 1 || exact.HasMore || exact.Items[0].ID != patientIDs[51] {
		t.Fatalf("search boundary: %+v %v", exact, err)
	}
	tail, err := s.ListPage(ctx, prefix, "ACTIVE", false, 2, 50)
	if err != nil || len(tail.Items) != 1 || tail.Items[0].ID != staffIDs[0] {
		t.Fatalf("staff tied timestamps did not use descending ID: %+v %v", tail, err)
	}
	empty, err := p.ListPage(ctx, f.doctor, prefix, 1, 50)
	if err != nil || len(empty.Items) != 0 || empty.HasMore {
		t.Fatalf("doctor could read unrelated patients: %+v %v", empty, err)
	}
	// Only the oldest matching patient is assigned; scope must precede LIMIT.
	if err := insertAppointment(f.pool, f.scheduleID, patientIDs[0], f.doctorID, f.specialtyID, f.roomID, nextFutureStart(), prefix+"-assigned-appointment", f.admin.UserID); err != nil {
		t.Fatal(err)
	}
	allowed, err := p.ListPage(ctx, f.doctor, prefix, 1, 1)
	if err != nil || len(allowed.Items) != 1 || allowed.Items[0].ID != patientIDs[0] || allowed.HasMore {
		t.Fatalf("scope applied after paging: %+v %v", allowed, err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	patients := p2HTTP(t, patient.NewHandler(p, nil, logger).List, f.admin, "/api/v1/patients?q="+prefix+"&page=2&page_size=50", 200)
	if patients["has_more"] != false || len(patients["items"].([]any)) != 2 {
		t.Fatalf("patient HTTP metadata: %+v", patients)
	}
	body := p2HTTP(t, staff.NewHandler(s, logger).List, f.admin, "/api/v1/staff?keyword="+prefix+"&page=2&page_size=50", 200)
	if body["has_more"] != false || len(body["items"].([]any)) != 1 {
		t.Fatalf("staff HTTP metadata: %+v", body)
	}
}

func TestP3UIClinicAndServicesBeyondFirst100(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prefix := fmt.Sprintf("P3UI%d", f.admin.UserID)
	az := authorization.NewStore(f.pool)
	c := clinic.NewService(f.pool)
	s := servicecatalog.NewService(f.pool, az)
	price := int64(1000)
	for i := 0; i < 101; i++ {
		insertSpecialty(t, f.pool, fmt.Sprintf("SPC_%s_S%03d", prefix, i), fmt.Sprintf("%s specialty %03d", prefix, i), f.admin.UserID)
		insertRoom(t, f.pool, fmt.Sprintf("RM-%s-R%03d", prefix, i), fmt.Sprintf("%s room %03d", prefix, i), f.admin.UserID)
		_, err := s.Create(ctx, f.admin, servicecatalog.CreateInput{Code: fmt.Sprintf("SVC-%s-V%03d", prefix, i), Name: fmt.Sprintf("%s service %03d", prefix, i), ServiceType: "PROCEDURE", Description: "Internal test description", Status: "ACTIVE", InitialUnitPriceVND: &price})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Run("specialties", func(t *testing.T) {
		p3CheckPages(t, 101, 100, func(page, size int) (pagination.Page[clinic.Specialty], error) {
			return c.ListSpecialtiesPage(ctx, prefix, true, page, size)
		})
	})
	t.Run("rooms", func(t *testing.T) {
		p3CheckPages(t, 101, 100, func(page, size int) (pagination.Page[clinic.Room], error) {
			return c.ListRoomsPage(ctx, prefix, "CONSULTATION", true, page, size)
		})
	})
	t.Run("services", func(t *testing.T) {
		p3CheckPages(t, 101, 100, func(page, size int) (pagination.Page[servicecatalog.CatalogService], error) {
			return s.ListPage(ctx, f.admin, servicecatalog.ListFilter{Query: prefix, Page: page, PageSize: size})
		})
	})
	visible, err := s.ListPage(ctx, f.patientPrincipal, servicecatalog.ListFilter{Query: prefix, Page: 2, PageSize: 100})
	if err != nil || len(visible.Items) != 1 || visible.HasMore || visible.Items[0].Code != "" || visible.Items[0].Description != nil || visible.Items[0].NextPriceAt != nil {
		t.Fatalf("patient projection: %+v %v", visible, err)
	}
	empty, err := c.ListSpecialtiesPage(ctx, prefix+"missing", true, 1, 100)
	if err != nil || len(empty.Items) != 0 || empty.Items == nil || empty.HasMore {
		t.Fatalf("empty specialty page: %+v %v", empty, err)
	}
	exact, err := c.ListRoomsPage(ctx, prefix+" room 000", "CONSULTATION", true, 1, 1)
	if err != nil || len(exact.Items) != 1 || exact.HasMore {
		t.Fatalf("room search/exact final page: %+v %v", exact, err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, v := range []struct {
		handler http.HandlerFunc
		path    string
	}{
		{cHandler(c, logger).ListSpecialties, "/api/v1/specialties?q=" + prefix},
		{cHandler(c, logger).ListRooms, "/api/v1/rooms?q=" + prefix},
		{servicecatalog.NewHandler(s, logger).List, "/api/v1/services?q=" + prefix},
	} {
		body := p2HTTP(t, v.handler, f.admin, v.path+"&page=2&page_size=100", 200)
		if body["has_more"] != false || len(body["items"].([]any)) != 1 {
			t.Fatalf("HTTP metadata: %s %+v", v.path, body)
		}
	}
	// This INACTIVE row sorts before all visible rows. Visibility must
	// be applied in SQL before the page is fetched, not by filtering afterwards.
	if _, err := s.Create(ctx, f.admin, servicecatalog.CreateInput{Code: "SVC-" + prefix + "-A-HIDDEN", Name: prefix + " inactive", ServiceType: "PROCEDURE", Status: "INACTIVE", InitialUnitPriceVND: &price}); err != nil {
		t.Fatal(err)
	}
	p3CheckPages(t, 101, 100, func(page, size int) (pagination.Page[servicecatalog.CatalogService], error) {
		return s.ListPage(ctx, f.patientPrincipal, servicecatalog.ListFilter{Query: prefix, Page: page, PageSize: size})
	})
}

func cHandler(c *clinic.Service, logger *slog.Logger) *clinic.Handler {
	return clinic.NewHandler(c, logger)
}

func TestP3UIAppointmentAndScheduleScopeBeforePagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	start := nextFutureStart().AddDate(0, 0, 25)
	for i := 0; i < 6; i++ {
		doctor, room, pid := f.doctorID, f.roomID, f.patientID
		if i >= 3 {
			doctor, room, pid = f.doctor2ID, f.room2ID, f.patient2ID
		}
		slot := start.AddDate(0, 0, i)
		sid := insertSchedule(t, f.pool, doctor, room, slot, slot.Add(time.Hour), "DRAFT", f.admin.UserID)
		if err := insertAppointment(f.pool, sid, pid, doctor, f.specialtyID, room, slot, fmt.Sprintf("p3ui-appointment-key-%d-%d", f.admin.UserID, i), f.admin.UserID); err != nil {
			t.Fatal(err)
		}
	}
	az := authorization.NewStore(f.pool)
	a := appointment.NewService(f.pool, az)
	s := scheduling.NewService(f.pool, az)
	from, to := start.Format(time.RFC3339), start.AddDate(0, 0, 7).Format(time.RFC3339)
	for _, actor := range []auth.Principal{f.doctor, f.patientPrincipal} {
		t.Run(actor.RoleCode, func(t *testing.T) {
			p3CheckPages(t, 3, 2, func(page, size int) (pagination.Page[appointment.Appointment], error) {
				doctorFilter := f.doctor2ID
				if actor.AccountType == auth.AccountTypePatient {
					doctorFilter = 0
				}
				return a.ListPage(ctx, actor, actor.AccountType == auth.AccountTypePatient, from, to, "CONFIRMED", 0, doctorFilter, 0, 0, page, size)
			})
		})
	}
	p3CheckPages(t, 3, 2, func(page, size int) (pagination.Page[scheduling.Schedule], error) {
		return s.ListPage(ctx, f.doctor, from, to, "DRAFT", f.doctor2ID, 0, page, size)
	})
	// A patient-supplied foreign ID cannot widen or redirect own scope.
	own, err := a.ListPage(ctx, f.patientPrincipal, true, from, to, "CONFIRMED", f.patient2ID, 0, 0, 0, 1, 2)
	if err != nil || len(own.Items) != 2 || !own.HasMore || own.Items[0].PatientID != f.patientID {
		t.Fatalf("own scope: %+v %v", own, err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p2HTTP(t, appointment.NewHandler(a, logger).List(false), f.doctor, "/api/v1/appointments?page=2&page_size=2&from="+from+"&to="+to, 200)
	p2HTTP(t, scheduling.NewHandler(s, logger).List, f.doctor, "/api/v1/doctor-schedules?page=2&page_size=2&from="+from+"&to="+to, 200)
}

func TestP3UIPendingMetadataLegacyReadOnlyAndPaidRemoval(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	enc := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	for i := 0; i < 3; i++ {
		aid := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 40+i))
		e, _, err := enc.Start(ctx, f.doctor, aid, 1)
		if err != nil {
			t.Fatal(err)
		}
		text := "Synthetic P3"
		e, err = enc.Update(ctx, f.doctor, e.ID, encounter.UpdateInput{ExpectedVersion: e.Version, ChiefComplaint: &text, Diagnosis: &text, Conclusion: &text})
		if err != nil {
			t.Fatal(err)
		}
		pain := int16(0)
		if _, err = enc.CreateVital(ctx, f.doctor, e.ID, encounter.VitalInput{PainScore: &pain}); err != nil {
			t.Fatal(err)
		}
		if _, _, err = enc.Finalize(ctx, f.doctor, aid, 2, e.Version); err != nil {
			t.Fatal(err)
		}
	}
	appConfig, err := pgxpool.ParseConfig(f.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	appConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE dncs_app")
		return err
	}
	appPool, err := pgxpool.NewWithConfig(ctx, appConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer appPool.Close()
	appFixture := f
	appFixture.pool = appPool
	s := billingService(appFixture)
	filter := billing.PendingFilter{DoctorID: f.doctorID}
	var before, after int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{1, 2, 3, 30} {
		p3CheckPages(t, 3, size, func(page, size int) (pagination.Page[billing.PendingEncounter], error) {
			v := filter
			v.Page = page
			v.PageSize = size
			return s.PendingPage(ctx, f.admin, v)
		})
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&after); err != nil || before != after {
		t.Fatal("preview changed audit", err)
	}
	for _, actor := range []auth.Principal{f.doctor, f.patientPrincipal} {
		_, err := s.PendingPage(ctx, actor, filter)
		assertProblemCode(t, err, "FORBIDDEN")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := billing.NewHandler(s, logger)
	path := fmt.Sprintf("/api/v1/billing/pending-encounters?doctor_id=%d&page_size=3", f.doctorID)
	r := httptest.NewRequest("GET", path, nil).WithContext(auth.WithPrincipal(ctx, f.admin))
	w := httptest.NewRecorder()
	h.Pending(w, r)
	var legacy []billing.PendingEncounter
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &legacy) != nil || len(legacy) != 3 {
		t.Fatal("legacy array response changed")
	}
	page := p2HTTP(t, h.Pending, f.admin, path+"&with_page=true", 200)
	if page["has_more"] != false || len(page["items"].([]any)) != 3 {
		t.Fatalf("exact final page: %+v", page)
	}
	first, err := s.PendingPage(ctx, f.admin, billing.PendingFilter{DoctorID: f.doctorID, Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := s.CreateInvoice(ctx, f.admin, first.Items[0].EncounterID, "CASH", fmt.Sprintf("p3ui-invoice-create-%d", f.admin.UserID))
	if err != nil || inv.TotalVND != first.Items[0].ExpectedTotalVND {
		t.Fatal("preview mismatch", err)
	}
	if _, err = s.Confirm(ctx, f.admin, inv.ID, billing.ConfirmInput{ConfirmedAmountVND: inv.TotalVND, IdempotencyKey: fmt.Sprintf("p3ui-payment-confirm-%d", inv.ID)}); err != nil {
		t.Fatal(err)
	}
	p3CheckPages(t, 2, 2, func(page, size int) (pagination.Page[billing.PendingEncounter], error) {
		v := filter
		v.Page = page
		v.PageSize = size
		return s.PendingPage(ctx, f.admin, v)
	})
}
