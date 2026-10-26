package database_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"dncs/backend/internal/appointment"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/encounter"
	"dncs/backend/internal/platform/problem"
	"dncs/backend/internal/servicecatalog"
)

func TestP3StartRequiresConsultationAndCommitsAggregateAtomically(t *testing.T) {
	f := newFixture(t)
	appointmentID := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 20))
	encounters := encounter.NewService(f.pool, authorization.NewStore(f.pool))

	_, _, err := encounters.Start(context.Background(), f.doctor, appointmentID, 1)
	assertProblemCode(t, err, "ENCOUNTER_CONSULTATION_SERVICE_NOT_CONFIGURED")
	var status string
	var encounterCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT status::text FROM appointments WHERE id=$1`, appointmentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM medical_encounters WHERE appointment_id=$1`, appointmentID).Scan(&encounterCount); err != nil {
		t.Fatal(err)
	}
	if status != "CHECKED_IN" || encounterCount != 0 {
		t.Fatalf("failed start was not atomic: status=%s encounters=%d", status, encounterCount)
	}

	consultation := createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	started, replayed, err := encounters.Start(context.Background(), f.doctor, appointmentID, 1)
	if err != nil || replayed {
		t.Fatalf("start encounter: replayed=%v err=%v", replayed, err)
	}
	if started.Status != "DRAFT" || started.AppointmentID != appointmentID {
		t.Fatalf("unexpected encounter: %+v", started)
	}
	var orderCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM encounter_services WHERE encounter_id=$1 AND service_id=$2 AND source='SYSTEM_CONSULTATION' AND status='ACTIVE' AND quantity=1`, started.ID, consultation.ID).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT status::text FROM appointments WHERE id=$1`, appointmentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "IN_PROGRESS" || orderCount != 1 {
		t.Fatalf("start aggregate mismatch: appointment=%s consultation_rows=%d", status, orderCount)
	}
	if _, replayed, err := encounters.Start(context.Background(), f.doctor, appointmentID, 1); err != nil || !replayed {
		t.Fatalf("start retry was not idempotent: replayed=%v err=%v", replayed, err)
	}
}

func TestP3ServiceCatalogVersioningProjectionAndConsultationRace(t *testing.T) {
	f := newFixture(t)
	catalog := servicecatalog.NewService(f.pool, authorization.NewStore(f.pool))
	n := nextFixtureID(t, f.pool)
	amount := int64(90000)
	description := "Mô tả vận hành nội bộ"
	procedure, err := catalog.Create(context.Background(), f.admin, servicecatalog.CreateInput{
		Code: fmt.Sprintf("SVC-PROJ-%d", n), Name: fmt.Sprintf("Projection service %d", n), ServiceType: "PROCEDURE",
		Description: description, Status: "ACTIVE", InitialUnitPriceVND: &amount,
	})
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	futurePrice, err := catalog.CreatePrice(context.Background(), f.admin, procedure.ID, 120000, future.Format(time.RFC3339))
	if err != nil || futurePrice.Phase != "SCHEDULED" {
		t.Fatalf("create scheduled price: price=%+v err=%v", futurePrice, err)
	}
	patientProjection, err := catalog.Get(context.Background(), f.patientPrincipal, procedure.ID)
	if err != nil {
		t.Fatal(err)
	}
	if patientProjection.Code != "" || patientProjection.Description != nil || patientProjection.NextPriceAt != nil || patientProjection.CurrentPrice == nil || patientProjection.CurrentPrice.UnitPriceVND != amount {
		t.Fatalf("patient service projection leaked internal/future data: %+v", patientProjection)
	}
	cancelled, err := catalog.CancelPrice(context.Background(), f.admin, procedure.ID, futurePrice.ID, "Thay đổi kế hoạch giá")
	if err != nil || cancelled.Phase != "CANCELLED" {
		t.Fatalf("cancel scheduled price: price=%+v err=%v", cancelled, err)
	}
	var currentPriceID int64
	if err := f.pool.QueryRow(context.Background(), `SELECT id FROM service_price_versions WHERE service_id=$1 AND effective_from<=now() AND cancelled_at IS NULL ORDER BY effective_from DESC LIMIT 1`, procedure.ID).Scan(&currentPriceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE service_price_versions SET unit_price_vnd=1 WHERE id=$1`, currentPriceID); err == nil {
		t.Fatal("database allowed immutable price mutation")
	}

	results := make(chan error, 2)
	barrier := make(chan struct{})
	var wait sync.WaitGroup
	for index := range 2 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-barrier
			price := int64(100000 + index)
			_, err := catalog.Create(context.Background(), f.admin, servicecatalog.CreateInput{
				Code: fmt.Sprintf("SVC-CONS-RACE-%d-%d", n, index), Name: fmt.Sprintf("Consultation race %d %d", n, index),
				ServiceType: "CONSULTATION", SpecialtyID: &f.specialtyID, Status: "ACTIVE", InitialUnitPriceVND: &price,
			})
			results <- err
		}(index)
	}
	close(barrier)
	wait.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if domain, ok := problem.As(err); ok && domain.Code == "SERVICE_CONSULTATION_ALREADY_EXISTS" {
			conflicts++
			continue
		}
		t.Fatalf("unexpected consultation race error: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("consultation race mismatch: successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestP3ClinicalLifecycleScopesServicesAndAmendments(t *testing.T) {
	f := newFixture(t)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	procedure := createCatalogService(t, f, "PROCEDURE", nil, "ACTIVE")
	appointmentID := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 21))
	service := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	started, _, err := service.Start(context.Background(), f.doctor, appointmentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	draftList, err := service.List(context.Background(), f.patientPrincipal, true, encounter.ListFilter{})
	if err != nil || len(draftList.Items) != 0 {
		t.Fatalf("patient could list draft encounter: %+v err=%v", draftList, err)
	}
	clinical := "Ghi chú nội bộ không dành cho patient"
	chief, diagnosis, conclusion := "Đau đầu", "Đau đầu căng cơ", "Theo dõi ngoại trú"
	updated, err := service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: 1, ChiefComplaint: &chief, Diagnosis: &diagnosis, Conclusion: &conclusion, ClinicalNotes: &clinical})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatalf("expected encounter version 2, got %d", updated.Version)
	}
	temperature := 37.2
	vital, err := service.CreateVital(context.Background(), f.doctor, started.ID, encounter.VitalInput{TemperatureC: &temperature})
	if err != nil {
		t.Fatal(err)
	}
	if vital.Version != 1 {
		t.Fatalf("unexpected vital version %d", vital.Version)
	}
	order, err := service.AddService(context.Background(), f.doctor, started.ID, procedure.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if order.UnitPriceVND != 150000 || order.Source != "DOCTOR_ORDER" {
		t.Fatalf("unexpected service order: %+v", order)
	}
	_, err = service.RemoveService(context.Background(), f.doctor, started.ID, order.ID, order.Version, "Không còn chỉ định")
	if err != nil {
		t.Fatal(err)
	}
	order, err = service.AddService(context.Background(), f.doctor, started.ID, procedure.ID, 1)
	if err != nil {
		t.Fatalf("reassign removed service: %v", err)
	}

	finalized, replayed, err := service.Finalize(context.Background(), f.doctor, appointmentID, 2, 2)
	if err != nil || replayed {
		t.Fatalf("finalize: replayed=%v err=%v", replayed, err)
	}
	if finalized.Status != "FINALIZED" || finalized.Version != 3 {
		t.Fatalf("unexpected finalized encounter: %+v", finalized)
	}
	if _, _, err := service.Finalize(context.Background(), f.doctor, appointmentID, 2, 2); err != nil {
		t.Fatalf("finalize retry should return current resource: %v", err)
	}
	if _, err := service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: 3, Symptoms: &chief}); err == nil {
		t.Fatal("finalized encounter remained mutable")
	}

	patientView, err := service.Get(context.Background(), f.patientPrincipal, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if patientView.ClinicalNotes != nil {
		t.Fatal("patient projection leaked clinical_notes")
	}
	adminView, err := service.Get(context.Background(), f.admin, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if adminView.ClinicalNotes == nil || *adminView.ClinicalNotes != clinical {
		t.Fatal("admin clinical projection unexpectedly redacted")
	}
	if _, err := service.Get(context.Background(), f.doctor2, started.ID); err == nil {
		t.Fatal("unrelated doctor could view finalized encounter")
	}

	if _, err := service.AddAmendment(context.Background(), f.doctor, started.ID, "CLINICAL_ADDENDUM", "Bổ sung", "Nội dung bổ sung"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddAmendment(context.Background(), f.admin, started.ID, "ADMINISTRATIVE_NOTE", "Hành chính", "Đã kiểm tra thông tin hành chính"); err != nil {
		t.Fatal(err)
	}
	amendments, err := service.ListAmendments(context.Background(), f.patientPrincipal, started.ID)
	if err != nil || len(amendments) != 2 || amendments[0].SequenceNumber != 1 || amendments[1].SequenceNumber != 2 {
		t.Fatalf("unexpected amendment timeline: %+v err=%v", amendments, err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE medical_encounter_amendments SET content='tampered' WHERE id=$1`, amendments[0].ID); err == nil {
		t.Fatal("append-only amendment database guard did not reject update")
	}
}

func TestP3FinalizeValidationAndConcurrentSafety(t *testing.T) {
	f := newFixture(t)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	appointmentID := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 22))
	service := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	started, _, err := service.Start(context.Background(), f.doctor, appointmentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Finalize(context.Background(), f.doctor, appointmentID, 2, 1)
	assertProblemCode(t, err, "ENCOUNTER_REQUIRED_FIELD_MISSING")
	chief, diagnosis, conclusion := "Ho", "Viêm họng", "Điều trị ngoại trú"
	if _, err := service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: 1, ChiefComplaint: &chief, Diagnosis: &diagnosis, Conclusion: &conclusion}); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Finalize(context.Background(), f.doctor, appointmentID, 2, 2)
	assertProblemCode(t, err, "ENCOUNTER_VITAL_SIGNS_REQUIRED")
	heartRate := int16(78)
	if _, err := service.CreateVital(context.Background(), f.doctor, started.ID, encounter.VitalInput{HeartRateBPM: &heartRate}); err != nil {
		t.Fatal(err)
	}

	barrier := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-barrier
			_, _, err := service.Finalize(context.Background(), f.doctor, appointmentID, 2, 2)
			results <- err
		}()
	}
	close(barrier)
	wait.Wait()
	close(results)
	for err := range results {
		if err == nil {
			continue
		}
		if domain, ok := problem.As(err); ok && domain.Code == "ENCOUNTER_CONCURRENT_UPDATE" {
			continue
		}
		t.Fatalf("unexpected concurrent finalize error: %v", err)
	}
	var encounterHistory, appointmentHistory int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM medical_encounter_status_histories WHERE encounter_id=$1 AND to_status='FINALIZED'`, started.ID).Scan(&encounterHistory); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM appointment_status_histories WHERE appointment_id=$1 AND to_status='COMPLETED'`, appointmentID).Scan(&appointmentHistory); err != nil {
		t.Fatal(err)
	}
	if encounterHistory != 1 || appointmentHistory != 1 {
		t.Fatalf("duplicate finalize history: encounter=%d appointment=%d", encounterHistory, appointmentHistory)
	}
}

func TestP3FollowUpIsSeparateIdempotentCommand(t *testing.T) {
	f := newFixture(t)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	appointmentID := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 23))
	encounters := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	started, _, err := encounters.Start(context.Background(), f.doctor, appointmentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	chief, diagnosis, conclusion := "Tái khám", "Ổn định", "Tái khám sau 1 tuần"
	if _, err := encounters.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: 1, ChiefComplaint: &chief, Diagnosis: &diagnosis, Conclusion: &conclusion}); err != nil {
		t.Fatal(err)
	}
	pain := int16(1)
	if _, err := encounters.CreateVital(context.Background(), f.doctor, started.ID, encounter.VitalInput{PainScore: &pain}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := encounters.Finalize(context.Background(), f.doctor, appointmentID, 2, 2); err != nil {
		t.Fatal(err)
	}

	start := nextFutureStart().AddDate(0, 0, 30)
	scheduleID := insertSchedule(t, f.pool, f.doctorID, f.roomID, start, start.Add(2*time.Hour), "ACTIVE", f.admin.UserID)
	appointments := appointment.NewService(f.pool, authorization.NewStore(f.pool))
	input := appointment.FollowUpInput{DoctorScheduleID: scheduleID, StartAt: start.Format(time.RFC3339), IdempotencyKey: fmt.Sprintf("follow-up-integration-%d", time.Now().UnixNano())}
	created, replayed, err := appointments.FollowUp(context.Background(), f.doctor, started.ID, input)
	if err != nil || replayed {
		t.Fatalf("create follow-up: replayed=%v err=%v", replayed, err)
	}
	if created.Source != "FOLLOW_UP" || created.Status != "CONFIRMED" || created.ApprovalMode != "DIRECT_FOLLOW_UP" {
		t.Fatalf("unexpected follow-up: %+v", created)
	}
	replayedItem, replayed, err := appointments.FollowUp(context.Background(), f.doctor, started.ID, input)
	if err != nil || !replayed || replayedItem.ID != created.ID {
		t.Fatalf("follow-up replay mismatch: item=%+v replayed=%v err=%v", replayedItem, replayed, err)
	}
	other := input
	other.IdempotencyKey = fmt.Sprintf("follow-up-integration-other-%d", time.Now().UnixNano())
	_, _, err = appointments.FollowUp(context.Background(), f.doctor, started.ID, other)
	assertProblemCode(t, err, "APPOINTMENT_FOLLOW_UP_ACTIVE_EXISTS")
}

func createCatalogService(t *testing.T, f fixture, serviceType string, specialtyID *int64, status string) servicecatalog.CatalogService {
	t.Helper()
	n := nextFixtureID(t, f.pool)
	amount := int64(150000)
	item, err := servicecatalog.NewService(f.pool, authorization.NewStore(f.pool)).Create(context.Background(), f.admin, servicecatalog.CreateInput{
		Code: fmt.Sprintf("SVC-IT-%d", n), Name: fmt.Sprintf("Integration service %d", n), ServiceType: serviceType, SpecialtyID: specialtyID, Status: status, InitialUnitPriceVND: &amount,
	})
	if err != nil {
		t.Fatalf("create catalog service: %v", err)
	}
	return item
}

func checkedInAppointment(t *testing.T, f fixture, start time.Time) int64 {
	t.Helper()
	scheduleID := insertSchedule(t, f.pool, f.doctorID, f.roomID, start, start.Add(time.Hour), "DRAFT", f.admin.UserID)
	key := fmt.Sprintf("p3-appointment-%d", time.Now().UnixNano())
	if err := insertAppointment(f.pool, scheduleID, f.patientID, f.doctorID, f.specialtyID, f.roomID, start, key, f.admin.UserID); err != nil {
		t.Fatalf("insert appointment: %v", err)
	}
	var id int64
	if err := f.pool.QueryRow(context.Background(), `SELECT id FROM appointments WHERE booked_by_user_id=$1 AND idempotency_key=$2`, f.admin.UserID, key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE appointments SET status='CHECKED_IN' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertProblemCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got nil", code)
	}
	var domain *problem.Error
	if !errors.As(err, &domain) || domain.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

// UI forms send "" for blank clinical fields. Spec (medical-encounter.md, constraint 11):
// trimmed-empty clinical text is stored as NULL, never "" (which used to violate
// ck_medical_encounters_text_length and surface as HTTP 500). Over-length is 422.
func TestP3UpdateBlankTextClearsToNullAndOverLengthIsValidation(t *testing.T) {
	f := newFixture(t)
	createCatalogService(t, f, "CONSULTATION", &f.specialtyID, "ACTIVE")
	appointmentID := checkedInAppointment(t, f, nextFutureStart().AddDate(0, 0, 24))
	service := encounter.NewService(f.pool, authorization.NewStore(f.pool))
	started, _, err := service.Start(context.Background(), f.doctor, appointmentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	chief, symptoms, diagnosis, conclusion, notes, followUp := "Đau họng", "Sốt 38,5", "Viêm họng cấp", "Điều trị ngoại trú", "Ghi chú", "Tái khám nếu sốt"
	updated, err := service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: 1, ChiefComplaint: &chief, Symptoms: &symptoms, Diagnosis: &diagnosis, Conclusion: &conclusion, ClinicalNotes: &notes, FollowUpRecommendation: &followUp})
	if err != nil {
		t.Fatal(err)
	}
	empty, blank, emptyNotes, emptyFollowUp := "", "  \n\t ", "", ""
	updated, err = service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: updated.Version, Symptoms: &empty, ClinicalNotes: &emptyNotes, FollowUpRecommendation: &emptyFollowUp, Conclusion: &blank})
	if err != nil {
		t.Fatalf("blank clinical text must clear the field, got %v", err)
	}
	if updated.Symptoms != nil || updated.ClinicalNotes != nil || updated.FollowUpRecommendation != nil || updated.Conclusion != nil {
		t.Fatalf("blank fields were not cleared to NULL: %+v", updated)
	}
	if updated.ChiefComplaint == nil || *updated.ChiefComplaint != chief || updated.Diagnosis == nil || *updated.Diagnosis != diagnosis {
		t.Fatalf("fields absent from the patch must be preserved: %+v", updated)
	}
	var storedEmpty int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM medical_encounters WHERE id=$1 AND '' IN (chief_complaint,symptoms,diagnosis,conclusion,clinical_notes,follow_up_recommendation)`, started.ID).Scan(&storedEmpty); err != nil {
		t.Fatal(err)
	}
	if storedEmpty != 0 {
		t.Fatal("empty string persisted in clinical text")
	}

	tooLong := strings.Repeat("a", 1001)
	_, err = service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: updated.Version, ChiefComplaint: &tooLong})
	assertProblemCode(t, err, "VALIDATION_ERROR")
	maxLen := strings.Repeat("á", 1000)
	if updated, err = service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: updated.Version, ChiefComplaint: &maxLen}); err != nil {
		t.Fatalf("1000-character chief complaint must be accepted: %v", err)
	}

	heartRate := int16(80)
	if _, err := service.CreateVital(context.Background(), f.doctor, started.ID, encounter.VitalInput{HeartRateBPM: &heartRate}); err != nil {
		t.Fatal(err)
	}
	// Conclusion was cleared, so finalize must still refuse.
	_, _, err = service.Finalize(context.Background(), f.doctor, appointmentID, 2, updated.Version)
	assertProblemCode(t, err, "ENCOUNTER_REQUIRED_FIELD_MISSING")
	if updated, err = service.Update(context.Background(), f.doctor, started.ID, encounter.UpdateInput{ExpectedVersion: updated.Version, Conclusion: &conclusion}); err != nil {
		t.Fatal(err)
	}
	// Only the three required fields are mandatory; the other three may stay NULL.
	if _, _, err = service.Finalize(context.Background(), f.doctor, appointmentID, 2, updated.Version); err != nil {
		t.Fatalf("finalize with the three required fields must succeed: %v", err)
	}
}
