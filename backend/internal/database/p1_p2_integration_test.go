package database_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"dncs/backend/internal/appointment"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/bootstrap"
	"dncs/backend/internal/database"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"dncs/backend/internal/scheduling"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

type fixture struct {
	pool                    *pgxpool.Pool
	admin, patientPrincipal auth.Principal
	doctor, doctor2         auth.Principal
	doctorID, doctor2ID     int64
	roomID, room2ID         int64
	specialtyID             int64
	patientID, patient2ID   int64
	scheduleID              int64
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; PostgreSQL integration test skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	migrateOnce.Do(func() {
		migrationDir, err := filepath.Abs("../../../database/migrations")
		if err != nil {
			migrateErr = err
			return
		}
		migrateErr = database.Migrate(ctx, pool, migrationDir)
	})
	if migrateErr != nil {
		t.Fatalf("migrate test database: %v", migrateErr)
	}
	return pool
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pool := integrationPool(t)
	ctx := context.Background()
	n := nextFixtureID(t, pool)
	adminRole := roleID(t, pool, "ADMIN")
	patientRole := roleID(t, pool, "PATIENT")
	adminUser := insertStaffUser(t, pool, adminRole, fmt.Sprintf("it_admin_%d", n))
	doctorUser, _, doctorID := insertDoctor(t, pool, fmt.Sprintf("it_doctor_%d", n), adminUser)
	doctor2User, _, doctor2ID := insertDoctor(t, pool, fmt.Sprintf("it_doctor2_%d", n), adminUser)
	patientUser, patientID := insertPatient(t, pool, patientRole, n, adminUser)
	_, patient2ID := insertPatient(t, pool, patientRole, n+100000, adminUser)
	roomID := insertRoom(t, pool, fmt.Sprintf("RM-IT-%d", n), fmt.Sprintf("Integration room %d", n), adminUser)
	room2ID := insertRoom(t, pool, fmt.Sprintf("RM-IT2-%d", n), fmt.Sprintf("Integration room 2 %d", n), adminUser)
	specialtyID := insertSpecialty(t, pool, fmt.Sprintf("SPC_IT_%d", n), fmt.Sprintf("Integration specialty %d", n), adminUser)
	for _, doctor := range []int64{doctorID, doctor2ID} {
		if _, err := pool.Exec(ctx, `INSERT INTO doctor_specialties(doctor_profile_id,specialty_id,assigned_by) VALUES($1,$2,$3)`, doctor, specialtyID, adminUser); err != nil {
			t.Fatalf("assign specialty: %v", err)
		}
	}
	start := nextFutureStart()
	scheduleID := insertSchedule(t, pool, doctorID, roomID, start, start.Add(2*time.Hour), "ACTIVE", adminUser)
	return fixture{
		pool:             pool,
		admin:            auth.Principal{UserID: adminUser, RoleID: adminRole, RoleCode: "ADMIN", AccountType: auth.AccountTypeStaff},
		patientPrincipal: auth.Principal{UserID: patientUser, RoleID: patientRole, RoleCode: "PATIENT", AccountType: auth.AccountTypePatient},
		doctor:           auth.Principal{UserID: doctorUser, RoleID: roleID(t, pool, "DOCTOR"), RoleCode: "DOCTOR", AccountType: auth.AccountTypeStaff},
		doctor2:          auth.Principal{UserID: doctor2User, RoleID: roleID(t, pool, "DOCTOR"), RoleCode: "DOCTOR", AccountType: auth.AccountTypeStaff},
		doctorID:         doctorID, doctor2ID: doctor2ID, roomID: roomID, room2ID: room2ID,
		specialtyID: specialtyID, patientID: patientID, patient2ID: patient2ID, scheduleID: scheduleID,
	}
}

func TestDoctorNoShowIsLimitedToOwnAppointments(t *testing.T) {
	f := newFixture(t)
	start := nextFutureStart().AddDate(0, 0, 1)
	scheduleID := insertSchedule(t, f.pool, f.doctor2ID, f.room2ID, start, start.Add(time.Hour), "DRAFT", f.admin.UserID)
	key := fmt.Sprintf("doctor-scope-no-show-%d", time.Now().UnixNano())
	if err := insertAppointment(f.pool, scheduleID, f.patientID, f.doctor2ID, f.specialtyID, f.room2ID, start, key, f.admin.UserID); err != nil {
		t.Fatalf("insert scoped appointment: %v", err)
	}
	var appointmentID, version int64
	if err := f.pool.QueryRow(context.Background(), `SELECT id,version FROM appointments WHERE idempotency_key=$1`, key).Scan(&appointmentID, &version); err != nil {
		t.Fatalf("load scoped appointment: %v", err)
	}
	service := appointment.NewService(f.pool, authorization.NewStore(f.pool))
	_, err := service.Transition(context.Background(), f.doctor, appointmentID, "no-show", appointment.TransitionInput{ExpectedVersion: version, Reason: "wrong doctor"})
	domain, ok := problem.As(err)
	if !ok || domain.Code != "APPOINTMENT_SCOPE_FORBIDDEN" {
		t.Fatalf("expected cross-doctor no-show denial, got %v", err)
	}
	_, err = service.Transition(context.Background(), f.doctor2, appointmentID, "no-show", appointment.TransitionInput{ExpectedVersion: version, Reason: "own appointment"})
	domain, ok = problem.As(err)
	if !ok || domain.Code != "APPOINTMENT_NO_SHOW_TOO_EARLY" {
		t.Fatalf("expected own-doctor request to pass scope check, got %v", err)
	}
}

func TestSuperAdminBootstrapIsConcurrentSafeAndIdempotent(t *testing.T) {
	pool := integrationPool(t)
	input := bootstrap.Input{Username: "bootstrap.gate.admin", FullName: "Bootstrap Gate Admin", Password: "bootstrap-test-password"}
	results := make(chan struct {
		result bootstrap.Result
		err    error
	}, 2)
	barrier := make(chan struct{})
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-barrier
			result, err := bootstrap.SuperAdmin(context.Background(), pool, auth.NewPasswordManager(), input)
			results <- struct {
				result bootstrap.Result
				err    error
			}{result: result, err: err}
		}()
	}
	close(barrier)
	wait.Wait()
	close(results)
	created, existing := 0, 0
	var userID int64
	for item := range results {
		if item.err != nil {
			t.Fatalf("concurrent bootstrap failed: %v", item.err)
		}
		if userID != 0 && item.result.UserID != userID {
			t.Fatalf("bootstrap returned different users: %d and %d", userID, item.result.UserID)
		}
		userID = item.result.UserID
		if item.result.Created {
			created++
		} else {
			existing++
		}
	}
	if created > 1 || created+existing != 2 {
		t.Fatalf("expected at most one create and two results for the same identity, created=%d existing=%d", created, existing)
	}

	var mustChange bool
	var historyCount, auditCount int
	var encodedPassword string
	if err := pool.QueryRow(context.Background(), `SELECT password_hash,must_change_password FROM users WHERE id=$1`, userID).Scan(&encodedPassword, &mustChange); err != nil {
		t.Fatal(err)
	}
	if !mustChange || !auth.NewPasswordManager().Matches(encodedPassword, input.Password) {
		t.Fatal("bootstrap password or must-change state is invalid")
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM user_role_histories WHERE user_id=$1`, userID).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_code='SUPER_ADMIN_BOOTSTRAPPED' AND target_id=$1`, userID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if historyCount != 1 || auditCount != 1 {
		t.Fatalf("bootstrap history/audit mismatch: history=%d audit=%d", historyCount, auditCount)
	}
	_, err := bootstrap.SuperAdmin(context.Background(), pool, auth.NewPasswordManager(), bootstrap.Input{Username: "another.super.admin", FullName: "Another Admin", Password: input.Password})
	if !errors.Is(err, bootstrap.ErrAlreadyBootstrapped) {
		t.Fatalf("expected second bootstrap identity to be rejected, got %v", err)
	}
}

func TestRefreshRotationRejectsConcurrentDoubleSubmit(t *testing.T) {
	pool := integrationPool(t)
	adminRole := roleID(t, pool, "ADMIN")
	userID := insertStaffUser(t, pool, adminRole, fmt.Sprintf("it_refresh_%d", nextFixtureID(t, pool)))
	service, err := auth.NewService(
		auth.NewStore(pool),
		auth.NewTokenManager("integration-test-jwt-secret-at-least-32-bytes", "dncs-integration", 15*time.Minute),
		auth.NewPasswordManager(),
		auth.ServiceConfig{StaffRefreshTTL: 8 * time.Hour, PatientRefreshTTL: 30 * 24 * time.Hour, MaxLoginFailures: 5, LoginLockDuration: 15 * time.Minute},
	)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.StartSessionForUser(context.Background(), userID, auth.ClientInfo{})
	if err != nil {
		t.Fatal(err)
	}
	type refreshResult struct {
		pair auth.TokenPair
		err  error
	}
	results := make(chan refreshResult, 2)
	barrier := make(chan struct{})
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-barrier
			pair, err := service.Refresh(context.Background(), initial.RefreshToken)
			results <- refreshResult{pair: pair, err: err}
		}()
	}
	close(barrier)
	wait.Wait()
	close(results)
	successes, rejected := 0, 0
	var rotated auth.TokenPair
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			rotated = result.pair
		case errors.Is(result.err, auth.ErrSessionInvalid):
			rejected++
		default:
			t.Fatalf("unexpected refresh result: %v", result.err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("expected one rotation and one rejection, successes=%d rejected=%d", successes, rejected)
	}
	if rotated.RefreshToken == "" || rotated.RefreshToken == initial.RefreshToken {
		t.Fatal("successful refresh did not rotate the token")
	}
	if _, err := service.Refresh(context.Background(), initial.RefreshToken); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("original refresh token remained valid: %v", err)
	}
}

func TestEffectivePermissionChangesOnNextCheck(t *testing.T) {
	pool := integrationPool(t)
	adminRole := roleID(t, pool, "ADMIN")
	userID := insertStaffUser(t, pool, adminRole, fmt.Sprintf("it_permission_%d", nextFixtureID(t, pool)))
	principal := auth.Principal{UserID: userID, RoleID: adminRole, RoleCode: "ADMIN", AccountType: auth.AccountTypeStaff}
	store := authorization.NewStore(pool)
	decision, err := store.Allowed(context.Background(), principal, "staff.view.all")
	if err != nil || !decision.Allowed || decision.Source != "ROLE" {
		t.Fatalf("expected initial role grant, decision=%+v err=%v", decision, err)
	}
	var overrideID int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO user_permission_overrides(user_id,permission_id,effect,reason,granted_by)
		SELECT $1,id,'DENY','integration immediate permission test',$1
		FROM permissions WHERE code='staff.view.all'
		RETURNING id`, userID).Scan(&overrideID); err != nil {
		t.Fatalf("insert permission override: %v", err)
	}
	decision, err = store.Allowed(context.Background(), principal, "staff.view.all")
	if err != nil || decision.Allowed || decision.Source != "USER_OVERRIDE" {
		t.Fatalf("expected immediate override denial, decision=%+v err=%v", decision, err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE user_permission_overrides SET is_deleted=true,updated_at=now(),version=version+1 WHERE id=$1`, overrideID); err != nil {
		t.Fatalf("remove permission override: %v", err)
	}
	decision, err = store.Allowed(context.Background(), principal, "staff.view.all")
	if err != nil || !decision.Allowed || decision.Source != "ROLE" {
		t.Fatalf("expected role grant immediately after override removal, decision=%+v err=%v", decision, err)
	}
}

func TestScheduleExclusionConstraintsAreConcurrentSafe(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	start := nextFutureStart().AddDate(0, 0, 2)
	// Use a fresh doctor/room combination so both concurrent transactions initially see an empty range.
	n := nextFixtureID(t, f.pool)
	_, _, doctorID := insertDoctor(t, f.pool, fmt.Sprintf("it_sched_race_%d", n), f.admin.UserID)
	roomID := insertRoom(t, f.pool, fmt.Sprintf("RM-SR-%d", n), fmt.Sprintf("Schedule race %d", n), f.admin.UserID)
	errorsSeen := concurrentInserts(2, func(index int) error {
		offset := time.Duration(index*15) * time.Minute
		_, err := f.pool.Exec(ctx, `INSERT INTO doctor_schedules(doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status,created_by,updated_by) VALUES($1,$2,$3,$4,30,'ACTIVE',$5,$5)`, doctorID, roomID, start.Add(offset), start.Add(90*time.Minute+offset), f.admin.UserID)
		return err
	})
	assertOneExclusionConflict(t, errorsSeen, "ex_doctor_schedules_doctor_overlap", "ex_doctor_schedules_room_overlap")

	// Same doctor, different rooms: only doctor overlap can fire.
	n = nextFixtureID(t, f.pool)
	_, _, doctorOnly := insertDoctor(t, f.pool, fmt.Sprintf("it_doctor_only_race_%d", n), f.admin.UserID)
	doctorRoomA := insertRoom(t, f.pool, fmt.Sprintf("RM-DR-A-%d", n), fmt.Sprintf("Doctor race A %d", n), f.admin.UserID)
	doctorRoomB := insertRoom(t, f.pool, fmt.Sprintf("RM-DR-B-%d", n), fmt.Sprintf("Doctor race B %d", n), f.admin.UserID)
	errorsSeen = concurrentInserts(2, func(index int) error {
		room := doctorRoomA
		if index == 1 {
			room = doctorRoomB
		}
		_, err := f.pool.Exec(ctx, `INSERT INTO doctor_schedules(doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status,created_by,updated_by) VALUES($1,$2,$3,$4,30,'ACTIVE',$5,$5)`, doctorOnly, room, start.AddDate(0, 0, 1), start.AddDate(0, 0, 1).Add(time.Hour), f.admin.UserID)
		return err
	})
	assertOneExclusionConflict(t, errorsSeen, "ex_doctor_schedules_doctor_overlap")

	// Isolate room exclusion with two different doctors.
	n = nextFixtureID(t, f.pool)
	_, _, d1 := insertDoctor(t, f.pool, fmt.Sprintf("it_room_race_a_%d", n), f.admin.UserID)
	_, _, d2 := insertDoctor(t, f.pool, fmt.Sprintf("it_room_race_b_%d", n), f.admin.UserID)
	room2 := insertRoom(t, f.pool, fmt.Sprintf("RM-RR-%d", n), fmt.Sprintf("Room race %d", n), f.admin.UserID)
	errorsSeen = concurrentInserts(2, func(index int) error {
		doctor := d1
		if index == 1 {
			doctor = d2
		}
		_, err := f.pool.Exec(ctx, `INSERT INTO doctor_schedules(doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status,created_by,updated_by) VALUES($1,$2,$3,$4,30,'ACTIVE',$5,$5)`, doctor, room2, start, start.Add(time.Hour), f.admin.UserID)
		return err
	})
	assertOneExclusionConflict(t, errorsSeen, "ex_doctor_schedules_room_overlap")
}

func TestAppointmentPatientAndRoomExclusionsAreConcurrentSafe(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	start := nextFutureStart().AddDate(0, 0, 4)
	// Two draft schedules allow isolating the patient constraint without schedule exclusions.
	s1 := insertSchedule(t, f.pool, f.doctorID, f.roomID, start, start.Add(time.Hour), "DRAFT", f.admin.UserID)
	s2 := insertSchedule(t, f.pool, f.doctor2ID, f.room2ID, start, start.Add(time.Hour), "DRAFT", f.admin.UserID)
	errorsSeen := concurrentInserts(2, func(index int) error {
		schedule, doctor, room := s1, f.doctorID, f.roomID
		if index == 1 {
			schedule, doctor, room = s2, f.doctor2ID, f.room2ID
		}
		return insertAppointment(f.pool, schedule, f.patientID, doctor, f.specialtyID, room, start, fmt.Sprintf("patient-race-key-%02d-%d", index, time.Now().UnixNano()), f.admin.UserID)
	})
	assertOneExclusionConflict(t, errorsSeen, "ex_appointments_patient_overlap")

	// Same doctor, different patients and rooms: only doctor overlap can fire.
	start = start.AddDate(0, 0, 1)
	s1 = insertSchedule(t, f.pool, f.doctorID, f.roomID, start, start.Add(time.Hour), "DRAFT", f.admin.UserID)
	s2 = insertSchedule(t, f.pool, f.doctorID, f.room2ID, start, start.Add(time.Hour), "DRAFT", f.admin.UserID)
	errorsSeen = concurrentInserts(2, func(index int) error {
		schedule, patient, room := s1, f.patientID, f.roomID
		if index == 1 {
			schedule, patient, room = s2, f.patient2ID, f.room2ID
		}
		return insertAppointment(f.pool, schedule, patient, f.doctorID, f.specialtyID, room, start, fmt.Sprintf("doctor-race-key-%02d-%d", index, time.Now().UnixNano()), f.admin.UserID)
	})
	assertOneExclusionConflict(t, errorsSeen, "ex_appointments_doctor_overlap")

	// Different doctors and patients, same room: only room overlap can fire.
	start = start.AddDate(0, 0, 1)
	s1 = insertSchedule(t, f.pool, f.doctorID, f.roomID, start, start.Add(time.Hour), "DRAFT", f.admin.UserID)
	s2 = insertSchedule(t, f.pool, f.doctor2ID, f.roomID, start, start.Add(time.Hour), "DRAFT", f.admin.UserID)
	errorsSeen = concurrentInserts(2, func(index int) error {
		schedule, doctor, patient := s1, f.doctorID, f.patientID
		if index == 1 {
			schedule, doctor, patient = s2, f.doctor2ID, f.patient2ID
		}
		return insertAppointment(f.pool, schedule, patient, doctor, f.specialtyID, f.roomID, start, fmt.Sprintf("room-race-key-%02d-%d", index, time.Now().UnixNano()), f.admin.UserID)
	})
	assertOneExclusionConflict(t, errorsSeen, "ex_appointments_room_overlap")
	_ = ctx
}

func TestBookingIdempotencyConcurrentReplayAndHashMismatch(t *testing.T) {
	f := newFixture(t)
	service := appointment.NewService(f.pool, authorization.NewStore(f.pool))
	var start time.Time
	if err := f.pool.QueryRow(context.Background(), `SELECT start_at FROM doctor_schedules WHERE id=$1`, f.scheduleID).Scan(&start); err != nil {
		t.Fatal(err)
	}
	input := appointment.BookInput{DoctorScheduleID: f.scheduleID, SpecialtyID: f.specialtyID, StartAt: start.Format(time.RFC3339), IdempotencyKey: fmt.Sprintf("booking-idempotency-%d", time.Now().UnixNano())}
	type result struct {
		item     appointment.Appointment
		replayed bool
		err      error
	}
	results := make(chan result, 2)
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			item, replayed, err := service.Book(context.Background(), f.patientPrincipal, input)
			results <- result{item, replayed, err}
		}()
	}
	close(barrier)
	wg.Wait()
	close(results)
	ids := map[int64]bool{}
	replays := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("same-key booking failed: %v", result.err)
		}
		ids[result.item.ID] = true
		if result.replayed {
			replays++
		}
	}
	if len(ids) != 1 || replays != 1 {
		t.Fatalf("expected one resource and one replay, ids=%v replays=%d", ids, replays)
	}
	input.StartAt = start.Add(30 * time.Minute).Format(time.RFC3339)
	_, _, err := service.Book(context.Background(), f.patientPrincipal, input)
	domain, ok := problem.As(err)
	if !ok || domain.Code != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("expected IDEMPOTENCY_KEY_REUSED, got %v", err)
	}
}

func TestBookingAndScheduleCancelRespectLockOrder(t *testing.T) {
	for iteration := 0; iteration < 4; iteration++ {
		f := newFixture(t)
		appointmentService := appointment.NewService(f.pool, authorization.NewStore(f.pool))
		scheduleService := scheduling.NewService(f.pool, authorization.NewStore(f.pool))
		var start time.Time
		var version int64
		if err := f.pool.QueryRow(context.Background(), `SELECT start_at,version FROM doctor_schedules WHERE id=$1`, f.scheduleID).Scan(&start, &version); err != nil {
			t.Fatal(err)
		}
		barrier := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-barrier
			_, _, err := appointmentService.Book(context.Background(), f.patientPrincipal, appointment.BookInput{DoctorScheduleID: f.scheduleID, SpecialtyID: f.specialtyID, StartAt: start.Format(time.RFC3339), IdempotencyKey: fmt.Sprintf("lock-order-book-%d-%d", iteration, time.Now().UnixNano())})
			results <- err
		}()
		go func() {
			defer wg.Done()
			<-barrier
			_, err := scheduleService.Cancel(context.Background(), f.admin, f.scheduleID, version, "integration lock-order race")
			results <- err
		}()
		close(barrier)
		wg.Wait()
		close(results)
		for err := range results {
			if pgutil.SQLState(err) == "40P01" {
				t.Fatalf("deadlock detected on iteration %d: %v", iteration, err)
			}
			if err == nil {
				continue
			}
			if domain, ok := problem.As(err); ok && (domain.Code == "SLOT_NOT_AVAILABLE" || domain.Code == "SCHEDULE_HAS_HOLDING_APPOINTMENTS" || domain.Code == "CONCURRENT_UPDATE") {
				continue
			}
			t.Fatalf("unexpected lock-order race error: %v", err)
		}
	}
}

func concurrentInserts(count int, insert func(int) error) []error {
	barrier := make(chan struct{})
	result := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) { defer wg.Done(); <-barrier; result <- insert(index) }(i)
	}
	close(barrier)
	wg.Wait()
	close(result)
	errorsSeen := make([]error, 0, count)
	for err := range result {
		errorsSeen = append(errorsSeen, err)
	}
	return errorsSeen
}
func assertOneExclusionConflict(t *testing.T, results []error, constraints ...string) {
	t.Helper()
	success, conflict := 0, 0
	allowed := map[string]bool{}
	for _, name := range constraints {
		allowed[name] = true
	}
	for _, err := range results {
		if err == nil {
			success++
			continue
		}
		if pgutil.SQLState(err) != "23P01" || !allowed[pgutil.Constraint(err)] {
			t.Fatalf("unexpected insert error state=%s constraint=%s: %v", pgutil.SQLState(err), pgutil.Constraint(err), err)
		}
		conflict++
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("expected one success/one exclusion conflict, got success=%d conflict=%d", success, conflict)
	}
}

func roleID(t *testing.T, pool *pgxpool.Pool, code string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM roles WHERE code=$1`, code).Scan(&id); err != nil {
		t.Fatalf("load role %s: %v", code, err)
	}
	return id
}

func nextFixtureID(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT nextval('appointment_code_seq')`).Scan(&id); err != nil {
		t.Fatalf("allocate integration fixture id: %v", err)
	}
	return id
}
func insertStaffUser(t *testing.T, pool *pgxpool.Pool, roleID int64, username string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `INSERT INTO users(role_id,account_type,username,normalized_username,password_hash,status) VALUES($1,'STAFF',$2,$2,$3,'ACTIVE') RETURNING id`, roleID, username, "integration-test-password-hash").Scan(&id)
	if err != nil {
		t.Fatalf("insert staff user: %v", err)
	}
	return id
}
func insertDoctor(t *testing.T, pool *pgxpool.Pool, username string, actor int64) (int64, int64, int64) {
	t.Helper()
	user := insertStaffUser(t, pool, roleID(t, pool, "DOCTOR"), username)
	var staff, doctor int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO staff_profiles(user_id,full_name,created_by,updated_by) VALUES($1,$2,$3,$3) RETURNING id`, user, "Integration Doctor "+username, actor).Scan(&staff); err != nil {
		t.Fatalf("insert doctor staff: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `INSERT INTO doctor_profiles(staff_profile_id,created_by) VALUES($1,$2) RETURNING id`, staff, actor).Scan(&doctor); err != nil {
		t.Fatalf("insert doctor profile: %v", err)
	}
	return user, staff, doctor
}
func insertPatient(t *testing.T, pool *pgxpool.Pool, roleID, n, actor int64) (int64, int64) {
	t.Helper()
	phone := fmt.Sprintf("+849%08d", n%100000000)
	var user, patient int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO users(role_id,account_type,phone_number,normalized_phone_number,password_hash,status) VALUES($1,'PATIENT',$2,$2,$3,'ACTIVE') RETURNING id`, roleID, phone, "integration-test-password-hash").Scan(&user); err != nil {
		t.Fatalf("insert patient user: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `INSERT INTO patients(user_id,full_name,normalized_full_name,created_source,created_by_user_id,updated_by_user_id) VALUES($1,$2,$3,'COUNTER_ADMIN',$4,$4) RETURNING id`, user, "Integration Patient "+phone, "integration patient "+phone, actor).Scan(&patient); err != nil {
		t.Fatalf("insert patient: %v", err)
	}
	return user, patient
}
func insertRoom(t *testing.T, pool *pgxpool.Pool, code, name string, actor int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO rooms(code,name,normalized_name,room_type,created_by,updated_by) VALUES($1,$2,$3,'CONSULTATION',$4,$4) RETURNING id`, code, name, stringsLower(name), actor).Scan(&id); err != nil {
		t.Fatalf("insert room: %v", err)
	}
	return id
}
func insertSpecialty(t *testing.T, pool *pgxpool.Pool, code, name string, actor int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO specialties(code,name,normalized_name,created_by,updated_by) VALUES($1,$2,$3,$4,$4) RETURNING id`, code, name, stringsLower(name), actor).Scan(&id); err != nil {
		t.Fatalf("insert specialty: %v", err)
	}
	return id
}
func insertSchedule(t *testing.T, pool *pgxpool.Pool, doctor, room int64, start, end time.Time, status string, actor int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO doctor_schedules(doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status,created_by,updated_by) VALUES($1,$2,$3,$4,30,$5,$6,$6) RETURNING id`, doctor, room, start, end, status, actor).Scan(&id); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}
	return id
}
func insertAppointment(pool *pgxpool.Pool, schedule, patient, doctor, specialty, room int64, start time.Time, key string, actor int64) error {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err := pool.Exec(context.Background(), `INSERT INTO appointments(doctor_schedule_id,patient_id,doctor_profile_id,specialty_id,room_id,doctor_code_at_booking,doctor_name_at_booking,specialty_code_at_booking,specialty_name_at_booking,room_code_at_booking,room_name_at_booking,start_at,end_at,status,source,approval_mode_at_booking,check_in_early_minutes_at_booking,check_in_late_minutes_at_booking,no_show_grace_minutes_at_booking,booked_by_user_id,idempotency_key,idempotency_request_hash) VALUES($1,$2,$3,$4,$5,'DOC-SNAPSHOT','Doctor snapshot','SPC_SNAPSHOT','Specialty snapshot','RM-SNAPSHOT','Room snapshot',$6,$7,'CONFIRMED','COUNTER','MANUAL',30,15,10,$8,$9,$10)`, schedule, patient, doctor, specialty, room, start, start.Add(30*time.Minute), actor, key, hash)
	return err
}
func nextFutureStart() time.Time {
	now := time.Now().UTC().AddDate(0, 0, 10)
	return time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, time.UTC)
}
func stringsLower(value string) string {
	result := make([]rune, 0, len(value))
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 32
		}
		result = append(result, r)
	}
	return string(result)
}
