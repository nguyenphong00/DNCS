package encounter

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool  *pgxpool.Pool
	authz *authorization.Store
	now   func() time.Time
}

func NewService(pool *pgxpool.Pool, authz *authorization.Store) *Service {
	return &Service{pool: pool, authz: authz, now: time.Now}
}

type Encounter struct {
	ID                        int64       `json:"id"`
	EncounterCode             string      `json:"encounter_code"`
	AppointmentID             int64       `json:"appointment_id"`
	PatientID                 int64       `json:"patient_id"`
	DoctorProfileID           int64       `json:"doctor_profile_id"`
	SpecialtyID               int64       `json:"specialty_id"`
	PatientCodeAtStart        string      `json:"patient_code_at_start"`
	PatientNameAtStart        string      `json:"patient_name_at_start"`
	PatientDateOfBirthAtStart *time.Time  `json:"patient_date_of_birth_at_start,omitempty"`
	PatientGenderAtStart      *string     `json:"patient_gender_at_start,omitempty"`
	DoctorCodeAtStart         string      `json:"doctor_code_at_start"`
	DoctorNameAtStart         string      `json:"doctor_name_at_start"`
	SpecialtyCodeAtStart      string      `json:"specialty_code_at_start"`
	SpecialtyNameAtStart      string      `json:"specialty_name_at_start"`
	Status                    string      `json:"status"`
	ChiefComplaint            *string     `json:"chief_complaint,omitempty"`
	Symptoms                  *string     `json:"symptoms,omitempty"`
	Diagnosis                 *string     `json:"diagnosis,omitempty"`
	Conclusion                *string     `json:"conclusion,omitempty"`
	ClinicalNotes             *string     `json:"clinical_notes,omitempty"`
	ClinicalNotesRedacted     bool        `json:"clinical_notes_redacted,omitempty"`
	FollowUpRecommendation    *string     `json:"follow_up_recommendation,omitempty"`
	StartedAt                 time.Time   `json:"started_at"`
	FinalizedAt               *time.Time  `json:"finalized_at,omitempty"`
	FinalizedByUserID         *int64      `json:"finalized_by_user_id,omitempty"`
	Version                   int64       `json:"version"`
	CreatedAt                 time.Time   `json:"created_at"`
	UpdatedAt                 time.Time   `json:"updated_at"`
	VitalSigns                []VitalSign `json:"vital_signs,omitempty"`
	Amendments                []Amendment `json:"amendments,omitempty"`
}

type UpdateInput struct {
	ExpectedVersion        int64
	ChiefComplaint         *string
	Symptoms               *string
	Diagnosis              *string
	Conclusion             *string
	ClinicalNotes          *string
	FollowUpRecommendation *string
}

type VitalInput struct {
	ExpectedVersion    int64
	TemperatureC       *float64
	SystolicBP         *int16
	DiastolicBP        *int16
	HeartRateBPM       *int16
	RespiratoryRateBPM *int16
	SpO2Percent        *float64
	WeightKG           *float64
	HeightCM           *float64
	PainScore          *int16
	Notes              *string
	MeasuredAt         string
}

type VitalSign struct {
	ID                 int64     `json:"id"`
	EncounterID        int64     `json:"encounter_id"`
	TemperatureC       *float64  `json:"temperature_c,omitempty"`
	SystolicBP         *int16    `json:"systolic_bp,omitempty"`
	DiastolicBP        *int16    `json:"diastolic_bp,omitempty"`
	HeartRateBPM       *int16    `json:"heart_rate_bpm,omitempty"`
	RespiratoryRateBPM *int16    `json:"respiratory_rate_bpm,omitempty"`
	SpO2Percent        *float64  `json:"spo2_percent,omitempty"`
	WeightKG           *float64  `json:"weight_kg,omitempty"`
	HeightCM           *float64  `json:"height_cm,omitempty"`
	PainScore          *int16    `json:"pain_score,omitempty"`
	Notes              *string   `json:"notes,omitempty"`
	MeasuredAt         time.Time `json:"measured_at"`
	RecordedByUserID   int64     `json:"recorded_by_user_id"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ServiceOrder struct {
	ID                           int64      `json:"id"`
	EncounterID                  int64      `json:"encounter_id"`
	ServiceID                    int64      `json:"service_id"`
	ServicePriceVersionIDAtOrder int64      `json:"service_price_version_id_at_order"`
	ServiceCodeAtOrder           string     `json:"service_code_at_order"`
	ServiceNameAtOrder           string     `json:"service_name_at_order"`
	UnitLabelAtOrder             string     `json:"unit_label_at_order"`
	Source                       string     `json:"source"`
	Quantity                     int16      `json:"quantity"`
	Status                       string     `json:"status"`
	AssignedByUserID             *int64     `json:"assigned_by_user_id,omitempty"`
	AssignedAt                   time.Time  `json:"assigned_at"`
	RemovedByUserID              *int64     `json:"removed_by_user_id,omitempty"`
	RemovedAt                    *time.Time `json:"removed_at,omitempty"`
	RemovalReason                *string    `json:"removal_reason,omitempty"`
	UnitPriceVND                 int64      `json:"unit_price_vnd"`
	Version                      int64      `json:"version"`
	CreatedAt                    time.Time  `json:"created_at"`
	UpdatedAt                    time.Time  `json:"updated_at"`
}

type Amendment struct {
	ID              int64     `json:"id"`
	EncounterID     int64     `json:"encounter_id"`
	SequenceNumber  int       `json:"sequence_number"`
	AmendmentType   string    `json:"amendment_type"`
	Reason          string    `json:"reason"`
	Content         string    `json:"content"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
}

type ListFilter struct {
	From, To, Status string
	DoctorProfileID  int64
	PatientID        int64
	SpecialtyID      int64
	AppointmentID    int64
	EncounterCode    string
	Cursor           string
	Limit            int
}

type ListResult struct {
	Items      []Encounter `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

type appointmentStart struct {
	ID, PatientID, DoctorID, SpecialtyID int64
	Status                               string
	Version                              int64
	DoctorCode, DoctorName               string
	SpecialtyCode, SpecialtyName         string
}

func (s *Service) Start(ctx context.Context, actor auth.Principal, appointmentID, expectedVersion int64) (Encounter, bool, error) {
	var prelim appointmentStart
	var doctorUserID, staffID, patientUserID int64
	err := s.pool.QueryRow(ctx, `
		SELECT a.id,a.patient_id,a.doctor_profile_id,a.specialty_id,a.status::text,a.version,
		       a.doctor_code_at_booking,a.doctor_name_at_booking,a.specialty_code_at_booking,a.specialty_name_at_booking,
		       sp.user_id,sp.id,p.user_id
		FROM appointments a JOIN doctor_profiles d ON d.id=a.doctor_profile_id
		JOIN staff_profiles sp ON sp.id=d.staff_profile_id JOIN patients p ON p.id=a.patient_id
		WHERE a.id=$1 AND NOT a.is_deleted`, appointmentID).Scan(&prelim.ID, &prelim.PatientID, &prelim.DoctorID, &prelim.SpecialtyID,
		&prelim.Status, &prelim.Version, &prelim.DoctorCode, &prelim.DoctorName, &prelim.SpecialtyCode, &prelim.SpecialtyName,
		&doctorUserID, &staffID, &patientUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Encounter{}, false, appointmentNotFound()
	}
	if err != nil {
		return Encounter{}, false, err
	}
	if doctorUserID != actor.UserID {
		return Encounter{}, false, scopeDenied()
	}
	if prelim.Status == "IN_PROGRESS" {
		item, err := s.getByAppointmentRaw(ctx, appointmentID)
		if err == nil && item.DoctorProfileID == prelim.DoctorID {
			return item, true, nil
		}
	}
	if prelim.Status != "CHECKED_IN" {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_APPOINTMENT_NOT_CHECKED_IN", "Lịch hẹn chưa ở trạng thái đã check-in.")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Encounter{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var specialtyActive bool
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM specialties WHERE id=$1 FOR SHARE`, prelim.SpecialtyID).Scan(&specialtyActive); err != nil {
		return Encounter{}, false, err
	}
	var accountActive, staffActive, doctorActive bool
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM users WHERE id=$1 FOR SHARE`, doctorUserID).Scan(&accountActive); err != nil {
		return Encounter{}, false, err
	}
	if err := tx.QueryRow(ctx, `SELECT employment_status='ACTIVE' AND NOT is_deleted FROM staff_profiles WHERE id=$1 FOR SHARE`, staffID).Scan(&staffActive); err != nil {
		return Encounter{}, false, err
	}
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM doctor_profiles WHERE id=$1 FOR SHARE`, prelim.DoctorID).Scan(&doctorActive); err != nil {
		return Encounter{}, false, err
	}
	if !specialtyActive || !accountActive || !staffActive || !doctorActive {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_DOCTOR_OR_SPECIALTY_INACTIVE", "Bác sĩ hoặc chuyên khoa không còn hoạt động.")
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR SHARE`, patientUserID); err != nil {
		return Encounter{}, false, err
	}
	var patientCode, patientName string
	var dob *time.Time
	var gender *string
	if err := tx.QueryRow(ctx, `SELECT patient_code,full_name,date_of_birth,gender_code FROM patients WHERE id=$1 FOR SHARE`, prelim.PatientID).Scan(&patientCode, &patientName, &dob, &gender); err != nil {
		return Encounter{}, false, err
	}
	var locked appointmentStart
	if err := tx.QueryRow(ctx, `SELECT id,patient_id,doctor_profile_id,specialty_id,status::text,version,doctor_code_at_booking,doctor_name_at_booking,specialty_code_at_booking,specialty_name_at_booking FROM appointments WHERE id=$1 AND NOT is_deleted FOR UPDATE`, appointmentID).Scan(&locked.ID, &locked.PatientID, &locked.DoctorID, &locked.SpecialtyID, &locked.Status, &locked.Version, &locked.DoctorCode, &locked.DoctorName, &locked.SpecialtyCode, &locked.SpecialtyName); err != nil {
		return Encounter{}, false, err
	}
	if locked.Version != expectedVersion {
		return Encounter{}, false, concurrent()
	}
	if locked.Status != "CHECKED_IN" {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_APPOINTMENT_NOT_CHECKED_IN", "Lịch hẹn chưa ở trạng thái đã check-in.")
	}
	if locked.PatientID != prelim.PatientID || locked.DoctorID != prelim.DoctorID || locked.SpecialtyID != prelim.SpecialtyID {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_APPOINTMENT_STATE_INCONSISTENT", "Dữ liệu lịch hẹn đã thay đổi không nhất quán.")
	}
	var encounterID int64
	if err := tx.QueryRow(ctx, `INSERT INTO medical_encounters(appointment_id,patient_id,doctor_profile_id,specialty_id,patient_code_at_start,patient_name_at_start,patient_date_of_birth_at_start,patient_gender_at_start,doctor_code_at_start,doctor_name_at_start,specialty_code_at_start,specialty_name_at_start) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`, appointmentID, locked.PatientID, locked.DoctorID, locked.SpecialtyID, patientCode, patientName, dob, gender, locked.DoctorCode, locked.DoctorName, locked.SpecialtyCode, locked.SpecialtyName).Scan(&encounterID); err != nil {
		return Encounter{}, false, mapConstraint(err)
	}
	var serviceID int64
	var serviceCode, serviceName, unitLabel string
	err = tx.QueryRow(ctx, `SELECT id,code,name,unit_label FROM services WHERE service_type='CONSULTATION' AND specialty_id=$1 AND status='ACTIVE' AND NOT is_deleted FOR SHARE`, locked.SpecialtyID).Scan(&serviceID, &serviceCode, &serviceName, &unitLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return Encounter{}, false, consultationMissing()
	}
	if err != nil {
		return Encounter{}, false, err
	}
	var priceID int64
	err = tx.QueryRow(ctx, `SELECT id FROM service_price_versions WHERE service_id=$1 AND effective_from<=now() AND cancelled_at IS NULL ORDER BY effective_from DESC,id DESC LIMIT 1 FOR SHARE`, serviceID).Scan(&priceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Encounter{}, false, consultationMissing()
	}
	if err != nil {
		return Encounter{}, false, err
	}
	var orderID int64
	if err := tx.QueryRow(ctx, `INSERT INTO encounter_services(encounter_id,service_id,service_price_version_id_at_order,service_code_at_order,service_name_at_order,unit_label_at_order,source,quantity) VALUES($1,$2,$3,$4,$5,$6,'SYSTEM_CONSULTATION',1) RETURNING id`, encounterID, serviceID, priceID, serviceCode, serviceName, unitLabel).Scan(&orderID); err != nil {
		return Encounter{}, false, mapConstraint(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO medical_encounter_status_histories(encounter_id,to_status,actor_user_id,metadata) VALUES($1,'DRAFT',$2,jsonb_build_object('appointment_id',$3::bigint))`, encounterID, actor.UserID, appointmentID); err != nil {
		return Encounter{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE appointments SET status='IN_PROGRESS',version=version+1,updated_at=now() WHERE id=$1`, appointmentID); err != nil {
		return Encounter{}, false, mapConstraint(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO appointment_status_histories(appointment_id,from_status,to_status,actor_user_id,actor_type,metadata) VALUES($1,'CHECKED_IN','IN_PROGRESS',$2,'USER',jsonb_build_object('encounter_id',$3::bigint))`, appointmentID, actor.UserID, encounterID); err != nil {
		return Encounter{}, false, err
	}
	for _, event := range []audit.Event{
		audit.UserEvent(actor.UserID, "ENCOUNTER", "ENCOUNTER_STARTED", "ENCOUNTER", encounterID),
		audit.UserEvent(actor.UserID, "ENCOUNTER", "ENCOUNTER_CONSULTATION_SERVICE_CREATED", "ENCOUNTER_SERVICE", orderID),
		audit.UserEvent(actor.UserID, "APPOINTMENT", "APPOINTMENT_STARTED", "APPOINTMENT", appointmentID),
	} {
		if err := audit.Insert(ctx, tx, event); err != nil {
			return Encounter{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Encounter{}, false, mapConstraint(err)
	}
	item, err := s.getRaw(ctx, encounterID)
	return item, false, err
}

func (s *Service) Update(ctx context.Context, actor auth.Principal, id int64, input UpdateInput) (Encounter, error) {
	appointmentID, doctorID, err := s.preliminaryOwnership(ctx, id)
	if err != nil {
		return Encounter{}, err
	}
	if err := s.requireAssignedDoctor(ctx, actor, doctorID); err != nil {
		return Encounter{}, err
	}
	values := []*string{input.ChiefComplaint, input.Symptoms, input.Diagnosis, input.Conclusion, input.ClinicalNotes, input.FollowUpRecommendation}
	limits := []int{1000, 4000, 4000, 4000, 8000, 2000}
	for i, value := range values {
		if value != nil {
			cleaned, err := clinicalText(*value, limits[i])
			if err != nil {
				return Encounter{}, err
			}
			*value = cleaned
		}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Encounter{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var appointmentStatus string
	if err := tx.QueryRow(ctx, `SELECT status::text FROM appointments WHERE id=$1 FOR UPDATE`, appointmentID).Scan(&appointmentStatus); err != nil {
		return Encounter{}, err
	}
	if appointmentStatus != "IN_PROGRESS" {
		return Encounter{}, problem.New(409, "ENCOUNTER_APPOINTMENT_NOT_IN_PROGRESS", "Lịch hẹn không ở trạng thái đang khám.")
	}
	var current Encounter
	if err := scanEncounter(tx.QueryRow(ctx, encounterQuery+` WHERE e.id=$1 FOR UPDATE`, id), &current); err != nil {
		return Encounter{}, err
	}
	if current.Status != "DRAFT" {
		return Encounter{}, problem.New(409, "ENCOUNTER_FINALIZED_IMMUTABLE", "Bệnh án đã hoàn tất và không thể sửa trực tiếp.")
	}
	if current.Version != input.ExpectedVersion {
		return Encounter{}, concurrent()
	}
	changed := make([]string, 0, 6)
	// Spec rule: clinical text is trimmed and never stored as "". A blank value
	// (already trimmed by clinicalText) clears the field to NULL; finalize then
	// rejects missing required fields with ENCOUNTER_REQUIRED_FIELD_MISSING.
	applyText := func(name string, dst **string, src *string) {
		if src != nil {
			if *src == "" {
				*dst = nil
			} else {
				*dst = src
			}
			changed = append(changed, name)
		}
	}
	applyText("chief_complaint", &current.ChiefComplaint, input.ChiefComplaint)
	applyText("symptoms", &current.Symptoms, input.Symptoms)
	applyText("diagnosis", &current.Diagnosis, input.Diagnosis)
	applyText("conclusion", &current.Conclusion, input.Conclusion)
	applyText("clinical_notes", &current.ClinicalNotes, input.ClinicalNotes)
	applyText("follow_up_recommendation", &current.FollowUpRecommendation, input.FollowUpRecommendation)
	if _, err := tx.Exec(ctx, `UPDATE medical_encounters SET chief_complaint=$2,symptoms=$3,diagnosis=$4,conclusion=$5,clinical_notes=$6,follow_up_recommendation=$7,version=version+1,updated_at=now() WHERE id=$1`, id, current.ChiefComplaint, current.Symptoms, current.Diagnosis, current.Conclusion, current.ClinicalNotes, current.FollowUpRecommendation); err != nil {
		return Encounter{}, mapConstraint(err)
	}
	e := audit.UserEvent(actor.UserID, "ENCOUNTER", "ENCOUNTER_UPDATED", "ENCOUNTER", id)
	e.Metadata = map[string]any{"fields": changed, "from_version": current.Version, "to_version": current.Version + 1}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return Encounter{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Encounter{}, err
	}
	return s.getRaw(ctx, id)
}

func (s *Service) CreateVital(ctx context.Context, actor auth.Principal, encounterID int64, input VitalInput) (VitalSign, error) {
	appointmentID, doctorID, err := s.preliminaryOwnership(ctx, encounterID)
	if err != nil {
		return VitalSign{}, err
	}
	if err := s.requireAssignedDoctor(ctx, actor, doctorID); err != nil {
		return VitalSign{}, err
	}
	measured, notes, err := s.validateVital(input, false)
	if err != nil {
		return VitalSign{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return VitalSign{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockDraft(ctx, tx, appointmentID, encounterID, doctorID); err != nil {
		return VitalSign{}, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO encounter_vital_signs(encounter_id,temperature_c,systolic_bp,diastolic_bp,heart_rate_bpm,respiratory_rate_bpm,spo2_percent,weight_kg,height_cm,pain_score,notes,measured_at,recorded_by_user_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, encounterID, input.TemperatureC, input.SystolicBP, input.DiastolicBP, input.HeartRateBPM, input.RespiratoryRateBPM, input.SpO2Percent, input.WeightKG, input.HeightCM, input.PainScore, notes, measured, actor.UserID).Scan(&id)
	if err != nil {
		return VitalSign{}, mapConstraint(err)
	}
	e := audit.UserEvent(actor.UserID, "ENCOUNTER", "ENCOUNTER_VITAL_SIGNS_RECORDED", "ENCOUNTER_VITAL_SIGN", id)
	e.Metadata = map[string]any{"encounter_id": encounterID}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return VitalSign{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VitalSign{}, err
	}
	return s.getVital(ctx, encounterID, id)
}

func (s *Service) UpdateVital(ctx context.Context, actor auth.Principal, encounterID, vitalID int64, input VitalInput) (VitalSign, error) {
	appointmentID, doctorID, err := s.preliminaryOwnership(ctx, encounterID)
	if err != nil {
		return VitalSign{}, err
	}
	if err := s.requireAssignedDoctor(ctx, actor, doctorID); err != nil {
		return VitalSign{}, err
	}
	measured, notes, err := s.validateVital(input, true)
	if err != nil {
		return VitalSign{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return VitalSign{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockDraft(ctx, tx, appointmentID, encounterID, doctorID); err != nil {
		return VitalSign{}, err
	}
	var version int64
	if err := tx.QueryRow(ctx, `SELECT version FROM encounter_vital_signs WHERE id=$1 AND encounter_id=$2 FOR UPDATE`, vitalID, encounterID).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return VitalSign{}, problem.New(404, "ENCOUNTER_VITAL_SIGN_NOT_FOUND", "Không tìm thấy lần đo chỉ số sinh tồn.")
	} else if err != nil {
		return VitalSign{}, err
	}
	if version != input.ExpectedVersion {
		return VitalSign{}, concurrent()
	}
	_, err = tx.Exec(ctx, `UPDATE encounter_vital_signs SET temperature_c=$2,systolic_bp=$3,diastolic_bp=$4,heart_rate_bpm=$5,respiratory_rate_bpm=$6,spo2_percent=$7,weight_kg=$8,height_cm=$9,pain_score=$10,notes=$11,measured_at=$12,version=version+1,updated_at=now() WHERE id=$1`, vitalID, input.TemperatureC, input.SystolicBP, input.DiastolicBP, input.HeartRateBPM, input.RespiratoryRateBPM, input.SpO2Percent, input.WeightKG, input.HeightCM, input.PainScore, notes, measured)
	if err != nil {
		return VitalSign{}, mapConstraint(err)
	}
	e := audit.UserEvent(actor.UserID, "ENCOUNTER", "ENCOUNTER_VITAL_SIGNS_UPDATED", "ENCOUNTER_VITAL_SIGN", vitalID)
	e.Metadata = map[string]any{"encounter_id": encounterID, "from_version": version, "to_version": version + 1}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return VitalSign{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VitalSign{}, err
	}
	return s.getVital(ctx, encounterID, vitalID)
}

func (s *Service) validateVital(input VitalInput, requireVersion bool) (time.Time, *string, error) {
	if requireVersion && input.ExpectedVersion < 1 {
		return time.Time{}, nil, invalid("expected_version không hợp lệ.")
	}
	if input.TemperatureC == nil && input.SystolicBP == nil && input.DiastolicBP == nil && input.HeartRateBPM == nil && input.RespiratoryRateBPM == nil && input.SpO2Percent == nil && input.WeightKG == nil && input.HeightCM == nil && input.PainScore == nil {
		return time.Time{}, nil, problem.New(422, "ENCOUNTER_VITAL_SIGNS_INVALID", "Cần ít nhất một chỉ số sinh tồn.")
	}
	if (input.TemperatureC != nil && *input.TemperatureC < 0) || (input.SystolicBP != nil && *input.SystolicBP < 0) || (input.DiastolicBP != nil && *input.DiastolicBP < 0) || (input.HeartRateBPM != nil && *input.HeartRateBPM < 0) || (input.RespiratoryRateBPM != nil && *input.RespiratoryRateBPM < 0) || (input.SpO2Percent != nil && (*input.SpO2Percent < 0 || *input.SpO2Percent > 100)) || (input.WeightKG != nil && *input.WeightKG < 0) || (input.HeightCM != nil && *input.HeightCM < 0) || (input.PainScore != nil && (*input.PainScore < 0 || *input.PainScore > 10)) || (input.SystolicBP != nil && input.DiastolicBP != nil && *input.SystolicBP <= *input.DiastolicBP) {
		return time.Time{}, nil, problem.New(422, "ENCOUNTER_VITAL_SIGNS_INVALID", "Chỉ số sinh tồn không hợp lệ.")
	}
	measured := s.now()
	if strings.TrimSpace(input.MeasuredAt) != "" {
		parsed, err := time.Parse(time.RFC3339, input.MeasuredAt)
		if err != nil || parsed.After(s.now().Add(5*time.Minute)) {
			return time.Time{}, nil, problem.New(422, "ENCOUNTER_VITAL_SIGNS_INVALID", "Thời điểm đo không hợp lệ.")
		}
		measured = parsed
	}
	notes := input.Notes
	if notes != nil {
		clean, err := clinicalText(*notes, 1000)
		if err != nil {
			return time.Time{}, nil, err
		}
		if clean == "" {
			notes = nil
		} else {
			notes = &clean
		}
	}
	return measured, notes, nil
}

func (s *Service) Finalize(ctx context.Context, actor auth.Principal, appointmentID, appointmentVersion, encounterVersion int64) (Encounter, bool, error) {
	var encounterID, doctorID int64
	var appointmentStatus, encounterStatus string
	var currentAppointmentVersion, currentEncounterVersion int64
	err := s.pool.QueryRow(ctx, `SELECT e.id,e.doctor_profile_id,a.status::text,e.status::text,a.version,e.version FROM appointments a JOIN medical_encounters e ON e.appointment_id=a.id WHERE a.id=$1`, appointmentID).Scan(&encounterID, &doctorID, &appointmentStatus, &encounterStatus, &currentAppointmentVersion, &currentEncounterVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return Encounter{}, false, notFound()
	}
	if err != nil {
		return Encounter{}, false, err
	}
	if err := s.requireAssignedDoctor(ctx, actor, doctorID); err != nil {
		return Encounter{}, false, err
	}
	if appointmentStatus == "COMPLETED" && encounterStatus == "FINALIZED" {
		item, err := s.getRaw(ctx, encounterID)
		return item, true, err
	}
	if appointmentStatus != "IN_PROGRESS" || encounterStatus != "DRAFT" {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_APPOINTMENT_STATE_INCONSISTENT", "Trạng thái bệnh án và lịch hẹn không nhất quán.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Encounter{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedAppointmentStatus string
	var lockedAppointmentVersion int64
	if err := tx.QueryRow(ctx, `SELECT status::text,version FROM appointments WHERE id=$1 FOR UPDATE`, appointmentID).Scan(&lockedAppointmentStatus, &lockedAppointmentVersion); err != nil {
		return Encounter{}, false, err
	}
	if lockedAppointmentVersion != appointmentVersion {
		return Encounter{}, false, concurrent()
	}
	if lockedAppointmentStatus != "IN_PROGRESS" {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_APPOINTMENT_NOT_IN_PROGRESS", "Lịch hẹn không ở trạng thái đang khám.")
	}
	var chief, diagnosis, conclusion *string
	var lockedEncounterStatus string
	var lockedEncounterVersion int64
	if err := tx.QueryRow(ctx, `SELECT status::text,version,chief_complaint,diagnosis,conclusion FROM medical_encounters WHERE id=$1 AND appointment_id=$2 AND doctor_profile_id=$3 FOR UPDATE`, encounterID, appointmentID, doctorID).Scan(&lockedEncounterStatus, &lockedEncounterVersion, &chief, &diagnosis, &conclusion); err != nil {
		return Encounter{}, false, err
	}
	if lockedEncounterVersion != encounterVersion {
		return Encounter{}, false, concurrent()
	}
	if lockedEncounterStatus != "DRAFT" {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_FINALIZED_IMMUTABLE", "Bệnh án đã hoàn tất.")
	}
	if chief == nil || diagnosis == nil || conclusion == nil {
		return Encounter{}, false, problem.New(422, "ENCOUNTER_REQUIRED_FIELD_MISSING", "Cần lý do khám, chẩn đoán và kết luận trước khi hoàn tất.")
	}
	var vitalCount, consultationCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM encounter_vital_signs WHERE encounter_id=$1`, encounterID).Scan(&vitalCount); err != nil {
		return Encounter{}, false, err
	}
	if vitalCount < 1 {
		return Encounter{}, false, problem.New(422, "ENCOUNTER_VITAL_SIGNS_REQUIRED", "Cần ít nhất một lần đo chỉ số sinh tồn.")
	}
	rows, err := tx.Query(ctx, `SELECT id FROM encounter_services WHERE encounter_id=$1 AND status='ACTIVE' ORDER BY id FOR UPDATE`, encounterID)
	if err != nil {
		return Encounter{}, false, err
	}
	for rows.Next() {
		consultationCount++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Encounter{}, false, err
	}
	if consultationCount < 1 {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_INVALID_SERVICE_ASSIGNMENT", "Encounter không có dịch vụ hợp lệ.")
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM encounter_services WHERE encounter_id=$1 AND status='ACTIVE' AND source='SYSTEM_CONSULTATION'`, encounterID).Scan(&consultationCount); err != nil {
		return Encounter{}, false, err
	}
	if consultationCount != 1 {
		return Encounter{}, false, problem.New(409, "ENCOUNTER_INVALID_SERVICE_ASSIGNMENT", "Encounter phải có đúng một phí khám hệ thống.")
	}
	if _, err := tx.Exec(ctx, `UPDATE medical_encounters SET status='FINALIZED',finalized_at=now(),finalized_by_user_id=$2,version=version+1,updated_at=now() WHERE id=$1`, encounterID, actor.UserID); err != nil {
		return Encounter{}, false, mapConstraint(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO medical_encounter_status_histories(encounter_id,from_status,to_status,actor_user_id,metadata) VALUES($1,'DRAFT','FINALIZED',$2,jsonb_build_object('appointment_id',$3::bigint))`, encounterID, actor.UserID, appointmentID); err != nil {
		return Encounter{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE appointments SET status='COMPLETED',version=version+1,updated_at=now() WHERE id=$1`, appointmentID); err != nil {
		return Encounter{}, false, mapConstraint(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO appointment_status_histories(appointment_id,from_status,to_status,actor_user_id,actor_type,metadata) VALUES($1,'IN_PROGRESS','COMPLETED',$2,'USER',jsonb_build_object('encounter_id',$3::bigint))`, appointmentID, actor.UserID, encounterID); err != nil {
		return Encounter{}, false, err
	}
	for _, event := range []audit.Event{audit.UserEvent(actor.UserID, "ENCOUNTER", "ENCOUNTER_FINALIZED", "ENCOUNTER", encounterID), audit.UserEvent(actor.UserID, "APPOINTMENT", "APPOINTMENT_COMPLETED", "APPOINTMENT", appointmentID)} {
		if err := audit.Insert(ctx, tx, event); err != nil {
			return Encounter{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Encounter{}, false, mapConstraint(err)
	}
	item, err := s.getRaw(ctx, encounterID)
	return item, false, err
}

func (s *Service) AddService(ctx context.Context, actor auth.Principal, encounterID, serviceID int64, quantity int16) (ServiceOrder, error) {
	if quantity < 1 || quantity > 999 {
		return ServiceOrder{}, problem.New(422, "ENCOUNTER_SERVICE_QUANTITY_INVALID", "Số lượng phải từ 1 đến 999.")
	}
	appointmentID, doctorID, err := s.preliminaryOwnership(ctx, encounterID)
	if err != nil {
		return ServiceOrder{}, err
	}
	if err := s.requireAssignedDoctor(ctx, actor, doctorID); err != nil {
		return ServiceOrder{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ServiceOrder{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockDraft(ctx, tx, appointmentID, encounterID, doctorID); err != nil {
		return ServiceOrder{}, err
	}
	var code, name, unit, serviceType, status string
	var deleted bool
	if err := tx.QueryRow(ctx, `SELECT code,name,unit_label,service_type::text,status::text,is_deleted FROM services WHERE id=$1 FOR SHARE`, serviceID).Scan(&code, &name, &unit, &serviceType, &status, &deleted); errors.Is(err, pgx.ErrNoRows) {
		return ServiceOrder{}, problem.New(404, "SERVICE_NOT_FOUND", "Không tìm thấy dịch vụ.")
	} else if err != nil {
		return ServiceOrder{}, err
	}
	if deleted || status != "ACTIVE" || serviceType == "CONSULTATION" {
		return ServiceOrder{}, problem.New(409, "SERVICE_INVALID_FOR_ENCOUNTER", "Dịch vụ không hợp lệ để chỉ định.")
	}
	var priceID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM service_price_versions WHERE service_id=$1 AND effective_from<=now() AND cancelled_at IS NULL ORDER BY effective_from DESC,id DESC LIMIT 1 FOR SHARE`, serviceID).Scan(&priceID); errors.Is(err, pgx.ErrNoRows) {
		return ServiceOrder{}, problem.New(409, "SERVICE_PRICE_REQUIRED", "Dịch vụ chưa có giá đang hiệu lực.")
	} else if err != nil {
		return ServiceOrder{}, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO encounter_services(encounter_id,service_id,service_price_version_id_at_order,service_code_at_order,service_name_at_order,unit_label_at_order,source,quantity,assigned_by_user_id) VALUES($1,$2,$3,$4,$5,$6,'DOCTOR_ORDER',$7,$8) RETURNING id`, encounterID, serviceID, priceID, code, name, unit, quantity, actor.UserID).Scan(&id); err != nil {
		return ServiceOrder{}, mapConstraint(err)
	}
	e := audit.UserEvent(actor.UserID, "ENCOUNTER", "ENCOUNTER_SERVICE_ASSIGNED", "ENCOUNTER_SERVICE", id)
	e.Metadata = map[string]any{"encounter_id": encounterID, "service_id": serviceID, "quantity": quantity}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return ServiceOrder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ServiceOrder{}, mapConstraint(err)
	}
	return s.getServiceOrder(ctx, encounterID, id)
}

func (s *Service) UpdateService(ctx context.Context, actor auth.Principal, encounterID, orderID int64, quantity int16, expectedVersion int64) (ServiceOrder, error) {
	return s.mutateService(ctx, actor, encounterID, orderID, quantity, expectedVersion, "")
}
func (s *Service) RemoveService(ctx context.Context, actor auth.Principal, encounterID, orderID, expectedVersion int64, reason string) (ServiceOrder, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return ServiceOrder{}, invalid("Lý do gỡ dịch vụ là bắt buộc và tối đa 500 ký tự.")
	}
	return s.mutateService(ctx, actor, encounterID, orderID, 0, expectedVersion, reason)
}
func (s *Service) mutateService(ctx context.Context, actor auth.Principal, encounterID, orderID int64, quantity int16, expectedVersion int64, reason string) (ServiceOrder, error) {
	if reason == "" && (quantity < 1 || quantity > 999) {
		return ServiceOrder{}, problem.New(422, "ENCOUNTER_SERVICE_QUANTITY_INVALID", "Số lượng phải từ 1 đến 999.")
	}
	appointmentID, doctorID, err := s.preliminaryOwnership(ctx, encounterID)
	if err != nil {
		return ServiceOrder{}, err
	}
	if err := s.requireAssignedDoctor(ctx, actor, doctorID); err != nil {
		return ServiceOrder{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ServiceOrder{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockDraft(ctx, tx, appointmentID, encounterID, doctorID); err != nil {
		return ServiceOrder{}, err
	}
	var source, status string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT source::text,status::text,version FROM encounter_services WHERE id=$1 AND encounter_id=$2 FOR UPDATE`, orderID, encounterID).Scan(&source, &status, &version); errors.Is(err, pgx.ErrNoRows) {
		return ServiceOrder{}, problem.New(404, "ENCOUNTER_SERVICE_NOT_FOUND", "Không tìm thấy chỉ định dịch vụ.")
	} else if err != nil {
		return ServiceOrder{}, err
	}
	if source == "SYSTEM_CONSULTATION" {
		return ServiceOrder{}, problem.New(409, "ENCOUNTER_SERVICE_SYSTEM_ROW_IMMUTABLE", "Không thể sửa phí khám hệ thống.")
	}
	if status != "ACTIVE" {
		return ServiceOrder{}, problem.New(409, "ENCOUNTER_SERVICE_NOT_FOUND", "Chỉ định dịch vụ không còn hoạt động.")
	}
	if version != expectedVersion {
		return ServiceOrder{}, concurrent()
	}
	eventCode := "ENCOUNTER_SERVICE_QUANTITY_UPDATED"
	if reason == "" {
		_, err = tx.Exec(ctx, `UPDATE encounter_services SET quantity=$2,version=version+1,updated_at=now() WHERE id=$1`, orderID, quantity)
	} else {
		eventCode = "ENCOUNTER_SERVICE_REMOVED"
		_, err = tx.Exec(ctx, `UPDATE encounter_services SET status='REMOVED',removed_by_user_id=$2,removed_at=now(),removal_reason=$3,version=version+1,updated_at=now() WHERE id=$1`, orderID, actor.UserID, reason)
	}
	if err != nil {
		return ServiceOrder{}, mapConstraint(err)
	}
	e := audit.UserEvent(actor.UserID, "ENCOUNTER", eventCode, "ENCOUNTER_SERVICE", orderID)
	e.Metadata = map[string]any{"encounter_id": encounterID, "quantity": quantity, "reason": reason, "from_version": version, "to_version": version + 1}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return ServiceOrder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ServiceOrder{}, err
	}
	return s.getServiceOrder(ctx, encounterID, orderID)
}

func (s *Service) ListServices(ctx context.Context, actor auth.Principal, encounterID int64) ([]ServiceOrder, error) {
	if _, err := s.Get(ctx, actor, encounterID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, serviceOrderQuery+` WHERE es.encounter_id=$1 AND ($2=false OR es.status='ACTIVE') ORDER BY es.id`, encounterID, actor.AccountType == auth.AccountTypePatient)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ServiceOrder, 0)
	for rows.Next() {
		item, err := scanServiceOrder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) AddAmendment(ctx context.Context, actor auth.Principal, encounterID int64, amendmentType, reason, content string) (Amendment, error) {
	amendmentType = strings.ToUpper(strings.TrimSpace(amendmentType))
	reason = strings.TrimSpace(reason)
	content = strings.TrimSpace(content)
	if reason == "" || len([]rune(reason)) > 1000 {
		return Amendment{}, problem.New(422, "ENCOUNTER_AMENDMENT_REASON_REQUIRED", "Lý do amendment là bắt buộc và tối đa 1.000 ký tự.")
	}
	if content == "" || len([]rune(content)) > 8000 || strings.ContainsAny(content, "<>") {
		return Amendment{}, invalid("Nội dung amendment không hợp lệ.")
	}
	_, doctorID, err := s.preliminaryOwnership(ctx, encounterID)
	if err != nil {
		return Amendment{}, err
	}
	all, err := s.authz.Allowed(ctx, actor, "encounter.amend.all")
	if err != nil {
		return Amendment{}, err
	}
	if all.Allowed {
		if amendmentType != "ADMINISTRATIVE_NOTE" {
			return Amendment{}, problem.New(409, "ENCOUNTER_AMENDMENT_NOT_ALLOWED", "Admin chỉ được thêm ghi chú hành chính.")
		}
	} else {
		if err := s.requireAssignedDoctor(ctx, actor, doctorID); err != nil {
			return Amendment{}, err
		}
		if amendmentType != "CLINICAL_ADDENDUM" && amendmentType != "CLINICAL_CORRECTION" {
			return Amendment{}, problem.New(409, "ENCOUNTER_AMENDMENT_NOT_ALLOWED", "Bác sĩ chỉ được thêm amendment lâm sàng.")
		}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Amendment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	var finalized *time.Time
	if err := tx.QueryRow(ctx, `SELECT status::text,finalized_at FROM medical_encounters WHERE id=$1 FOR UPDATE`, encounterID).Scan(&status, &finalized); err != nil {
		return Amendment{}, err
	}
	if status != "FINALIZED" || finalized == nil {
		return Amendment{}, problem.New(409, "ENCOUNTER_AMENDMENT_NOT_ALLOWED", "Chỉ được bổ sung bệnh án đã hoàn tất.")
	}
	if s.now().After(finalized.Add(10 * 24 * time.Hour)) {
		return Amendment{}, problem.New(409, "ENCOUNTER_AMENDMENT_WINDOW_EXPIRED", "Đã quá cửa sổ amendment 10 ngày.")
	}
	var sequence int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence_number),0)+1 FROM medical_encounter_amendments WHERE encounter_id=$1`, encounterID).Scan(&sequence); err != nil {
		return Amendment{}, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO medical_encounter_amendments(encounter_id,sequence_number,amendment_type,reason,content,created_by_user_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, encounterID, sequence, amendmentType, reason, content, actor.UserID).Scan(&id); err != nil {
		return Amendment{}, mapConstraint(err)
	}
	e := audit.UserEvent(actor.UserID, "ENCOUNTER", "ENCOUNTER_AMENDMENT_ADDED", "ENCOUNTER_AMENDMENT", id)
	e.Metadata = map[string]any{"encounter_id": encounterID, "sequence_number": sequence, "amendment_type": amendmentType}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return Amendment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Amendment{}, err
	}
	return s.getAmendment(ctx, encounterID, id)
}

func (s *Service) ListAmendments(ctx context.Context, actor auth.Principal, encounterID int64) ([]Amendment, error) {
	if _, err := s.Get(ctx, actor, encounterID); err != nil {
		return nil, err
	}
	return s.listAmendments(ctx, encounterID)
}

func (s *Service) Get(ctx context.Context, actor auth.Principal, id int64) (Encounter, error) {
	item, err := s.getRaw(ctx, id)
	if err != nil {
		return Encounter{}, err
	}
	projection, err := s.authorizeView(ctx, actor, item)
	if err != nil {
		return Encounter{}, err
	}
	if projection == "patient" {
		item.ClinicalNotes = nil
	} else if projection == "admin" {
		decision, err := s.authz.Allowed(ctx, actor, "encounter.clinical_notes.view.all")
		if err != nil {
			return Encounter{}, err
		}
		if !decision.Allowed {
			item.ClinicalNotes = nil
			item.ClinicalNotesRedacted = true
		}
	}
	item.VitalSigns, err = s.listVitals(ctx, id)
	if err != nil {
		return Encounter{}, err
	}
	item.Amendments, err = s.listAmendments(ctx, id)
	return item, err
}

func (s *Service) List(ctx context.Context, actor auth.Principal, ownPatient bool, filter ListFilter) (ListResult, error) {
	if filter.Limit < 1 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	viewAll, err := s.authz.Allowed(ctx, actor, "encounter.view.all")
	if err != nil {
		return ListResult{}, err
	}
	showClinicalNotes := actor.AccountType != auth.AccountTypePatient
	if viewAll.Allowed {
		clinicalDecision, err := s.authz.Allowed(ctx, actor, "encounter.clinical_notes.view.all")
		if err != nil {
			return ListResult{}, err
		}
		showClinicalNotes = clinicalDecision.Allowed
	}
	doctorID, patientID := int64(0), filter.PatientID
	if ownPatient || actor.AccountType == auth.AccountTypePatient {
		if err := s.pool.QueryRow(ctx, `SELECT id FROM patients WHERE user_id=$1 AND NOT is_deleted`, actor.UserID).Scan(&patientID); errors.Is(err, pgx.ErrNoRows) {
			return ListResult{Items: []Encounter{}}, nil
		} else if err != nil {
			return ListResult{}, err
		}
	} else if !viewAll.Allowed {
		doctorID, err = s.actorDoctorID(ctx, actor.UserID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ListResult{Items: []Encounter{}}, nil
		}
		if err != nil {
			return ListResult{}, err
		}
	}
	cursorTime := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	cursorID := int64(1 << 62)
	from := time.Time{}
	to := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if filter.From != "" {
		from, err = time.Parse(time.RFC3339, filter.From)
		if err != nil {
			return ListResult{}, invalid("from không hợp lệ.")
		}
	}
	if filter.To != "" {
		to, err = time.Parse(time.RFC3339, filter.To)
		if err != nil {
			return ListResult{}, invalid("to không hợp lệ.")
		}
	}
	// Cursor is encoded as unix nanos and ID; use separate variables to keep SQL stable.
	if filter.Cursor != "" {
		var nanos int64
		decoded, _ := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if _, err := fmt.Sscanf(string(decoded), "%d:%d", &nanos, &cursorID); err != nil {
			return ListResult{}, invalid("cursor không hợp lệ.")
		}
		cursorTime = time.Unix(0, nanos)
	}
	rows, err := s.pool.Query(ctx, encounterQuery+`
		WHERE e.started_at >= $1 AND e.started_at < $2
		  AND (e.started_at,e.id) < ($3,$4)
		  AND ($5='' OR e.status::text=$5)
		  AND ($6=0 OR e.specialty_id=$6)
		  AND ($7=0 OR e.appointment_id=$7)
		  AND ($8='' OR e.encounter_code=$8)
		  AND ($9=0 OR e.patient_id=$9)
		  AND ($10=0 OR e.doctor_profile_id=$10)
		  AND (
		    $11
		    OR ($12>0 AND (
		      (e.status='DRAFT' AND e.doctor_profile_id=$12)
		      OR (e.status='FINALIZED' AND (
		        e.doctor_profile_id=$12
		        OR EXISTS(SELECT 1 FROM appointments a2
		                  WHERE a2.patient_id=e.patient_id AND a2.doctor_profile_id=$12
		                    AND a2.status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))
		        OR EXISTS(SELECT 1 FROM medical_encounters h
		                  WHERE h.patient_id=e.patient_id AND h.doctor_profile_id=$12
		                    AND h.status='FINALIZED')
		      ))
		    ))
		  )
		  AND (NOT $14 OR e.status='FINALIZED')
		ORDER BY e.started_at DESC,e.id DESC LIMIT $13`, from, to, cursorTime, cursorID, strings.ToUpper(strings.TrimSpace(filter.Status)), filter.SpecialtyID, filter.AppointmentID, strings.TrimSpace(filter.EncounterCode), patientID, filter.DoctorProfileID, viewAll.Allowed || actor.AccountType == auth.AccountTypePatient, doctorID, filter.Limit+1, actor.AccountType == auth.AccountTypePatient)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	items := make([]Encounter, 0, filter.Limit+1)
	for rows.Next() {
		var item Encounter
		if err := scanEncounter(rows, &item); err != nil {
			return ListResult{}, err
		}
		if !showClinicalNotes {
			item.ClinicalNotes = nil
			if viewAll.Allowed {
				item.ClinicalNotesRedacted = true
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	result := ListResult{Items: items}
	if len(items) > filter.Limit {
		last := items[filter.Limit-1]
		result.Items = items[:filter.Limit]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", last.StartedAt.UnixNano(), last.ID)))
	}
	return result, nil
}

func (s *Service) authorizeView(ctx context.Context, actor auth.Principal, item Encounter) (string, error) {
	all, err := s.authz.Allowed(ctx, actor, "encounter.view.all")
	if err != nil {
		return "", err
	}
	if all.Allowed {
		return "admin", nil
	}
	if actor.AccountType == auth.AccountTypePatient {
		var own bool
		if item.Status == "FINALIZED" {
			err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patients WHERE id=$1 AND user_id=$2)`, item.PatientID, actor.UserID).Scan(&own)
		}
		if err != nil {
			return "", err
		}
		if own {
			return "patient", nil
		}
		return "", notFound()
	}
	doctorID, err := s.actorDoctorID(ctx, actor.UserID)
	if err != nil {
		return "", notFound()
	}
	if item.Status == "DRAFT" {
		if item.DoctorProfileID == doctorID {
			return "doctor", nil
		}
		return "", notFound()
	}
	var allowed bool
	err = s.pool.QueryRow(ctx, `SELECT $3::bigint=$2::bigint OR EXISTS(SELECT 1 FROM appointments WHERE patient_id=$1 AND doctor_profile_id=$3 AND status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS')) OR EXISTS(SELECT 1 FROM medical_encounters WHERE patient_id=$1 AND doctor_profile_id=$3 AND status='FINALIZED')`, item.PatientID, item.DoctorProfileID, doctorID).Scan(&allowed)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", notFound()
	}
	return "doctor", nil
}

func (s *Service) preliminaryOwnership(ctx context.Context, id int64) (int64, int64, error) {
	var appointmentID, doctorID int64
	err := s.pool.QueryRow(ctx, `SELECT appointment_id,doctor_profile_id FROM medical_encounters WHERE id=$1 AND NOT is_deleted`, id).Scan(&appointmentID, &doctorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, notFound()
	}
	return appointmentID, doctorID, err
}
func (s *Service) requireAssignedDoctor(ctx context.Context, actor auth.Principal, doctorID int64) error {
	id, err := s.actorDoctorID(ctx, actor.UserID)
	if err != nil || id != doctorID {
		return scopeDenied()
	}
	return nil
}
func (s *Service) actorDoctorID(ctx context.Context, userID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT d.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id JOIN users u ON u.id=sp.user_id WHERE sp.user_id=$1 AND u.status='ACTIVE' AND NOT u.is_deleted AND sp.employment_status='ACTIVE' AND NOT sp.is_deleted AND d.status='ACTIVE' AND NOT d.is_deleted`, userID).Scan(&id)
	return id, err
}
func lockDraft(ctx context.Context, tx pgx.Tx, appointmentID, encounterID, doctorID int64) error {
	var appointmentStatus string
	if err := tx.QueryRow(ctx, `SELECT status::text FROM appointments WHERE id=$1 FOR UPDATE`, appointmentID).Scan(&appointmentStatus); err != nil {
		return err
	}
	if appointmentStatus != "IN_PROGRESS" {
		return problem.New(409, "ENCOUNTER_APPOINTMENT_NOT_IN_PROGRESS", "Lịch hẹn không ở trạng thái đang khám.")
	}
	var status string
	var lockedDoctor int64
	if err := tx.QueryRow(ctx, `SELECT status::text,doctor_profile_id FROM medical_encounters WHERE id=$1 FOR UPDATE`, encounterID).Scan(&status, &lockedDoctor); err != nil {
		return err
	}
	if status != "DRAFT" {
		return problem.New(409, "ENCOUNTER_FINALIZED_IMMUTABLE", "Bệnh án đã hoàn tất.")
	}
	if lockedDoctor != doctorID {
		return scopeDenied()
	}
	return nil
}

const encounterQuery = `SELECT e.id,e.encounter_code,e.appointment_id,e.patient_id,e.doctor_profile_id,e.specialty_id,e.patient_code_at_start,e.patient_name_at_start,e.patient_date_of_birth_at_start,e.patient_gender_at_start,e.doctor_code_at_start,e.doctor_name_at_start,e.specialty_code_at_start,e.specialty_name_at_start,e.status::text,e.chief_complaint,e.symptoms,e.diagnosis,e.conclusion,e.clinical_notes,e.follow_up_recommendation,e.started_at,e.finalized_at,e.finalized_by_user_id,e.version,e.created_at,e.updated_at FROM medical_encounters e`

type scanner interface{ Scan(...any) error }

func scanEncounter(row scanner, item *Encounter) error {
	return row.Scan(&item.ID, &item.EncounterCode, &item.AppointmentID, &item.PatientID, &item.DoctorProfileID, &item.SpecialtyID, &item.PatientCodeAtStart, &item.PatientNameAtStart, &item.PatientDateOfBirthAtStart, &item.PatientGenderAtStart, &item.DoctorCodeAtStart, &item.DoctorNameAtStart, &item.SpecialtyCodeAtStart, &item.SpecialtyNameAtStart, &item.Status, &item.ChiefComplaint, &item.Symptoms, &item.Diagnosis, &item.Conclusion, &item.ClinicalNotes, &item.FollowUpRecommendation, &item.StartedAt, &item.FinalizedAt, &item.FinalizedByUserID, &item.Version, &item.CreatedAt, &item.UpdatedAt)
}
func (s *Service) getRaw(ctx context.Context, id int64) (Encounter, error) {
	var item Encounter
	err := scanEncounter(s.pool.QueryRow(ctx, encounterQuery+` WHERE e.id=$1 AND NOT e.is_deleted`, id), &item)
	if errors.Is(err, pgx.ErrNoRows) {
		return Encounter{}, notFound()
	}
	return item, err
}
func (s *Service) getByAppointmentRaw(ctx context.Context, id int64) (Encounter, error) {
	var item Encounter
	err := scanEncounter(s.pool.QueryRow(ctx, encounterQuery+` WHERE e.appointment_id=$1 AND NOT e.is_deleted`, id), &item)
	if errors.Is(err, pgx.ErrNoRows) {
		return Encounter{}, notFound()
	}
	return item, err
}

const vitalQuery = `SELECT id,encounter_id,temperature_c,systolic_bp,diastolic_bp,heart_rate_bpm,respiratory_rate_bpm,spo2_percent,weight_kg,height_cm,pain_score,notes,measured_at,recorded_by_user_id,version,created_at,updated_at FROM encounter_vital_signs`

func scanVital(row scanner) (VitalSign, error) {
	var v VitalSign
	err := row.Scan(&v.ID, &v.EncounterID, &v.TemperatureC, &v.SystolicBP, &v.DiastolicBP, &v.HeartRateBPM, &v.RespiratoryRateBPM, &v.SpO2Percent, &v.WeightKG, &v.HeightCM, &v.PainScore, &v.Notes, &v.MeasuredAt, &v.RecordedByUserID, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func (s *Service) getVital(ctx context.Context, encounterID, id int64) (VitalSign, error) {
	v, err := scanVital(s.pool.QueryRow(ctx, vitalQuery+` WHERE id=$1 AND encounter_id=$2`, id, encounterID))
	if errors.Is(err, pgx.ErrNoRows) {
		return VitalSign{}, problem.New(404, "ENCOUNTER_VITAL_SIGN_NOT_FOUND", "Không tìm thấy lần đo chỉ số sinh tồn.")
	}
	return v, err
}
func (s *Service) listVitals(ctx context.Context, id int64) ([]VitalSign, error) {
	rows, err := s.pool.Query(ctx, vitalQuery+` WHERE encounter_id=$1 ORDER BY measured_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]VitalSign, 0)
	for rows.Next() {
		v, err := scanVital(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

const serviceOrderQuery = `SELECT es.id,es.encounter_id,es.service_id,es.service_price_version_id_at_order,es.service_code_at_order,es.service_name_at_order,es.unit_label_at_order,es.source::text,es.quantity,es.status::text,es.assigned_by_user_id,es.assigned_at,es.removed_by_user_id,es.removed_at,es.removal_reason,p.unit_price_vnd,es.version,es.created_at,es.updated_at FROM encounter_services es JOIN service_price_versions p ON p.id=es.service_price_version_id_at_order`

func scanServiceOrder(row scanner) (ServiceOrder, error) {
	var o ServiceOrder
	err := row.Scan(&o.ID, &o.EncounterID, &o.ServiceID, &o.ServicePriceVersionIDAtOrder, &o.ServiceCodeAtOrder, &o.ServiceNameAtOrder, &o.UnitLabelAtOrder, &o.Source, &o.Quantity, &o.Status, &o.AssignedByUserID, &o.AssignedAt, &o.RemovedByUserID, &o.RemovedAt, &o.RemovalReason, &o.UnitPriceVND, &o.Version, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}
func (s *Service) getServiceOrder(ctx context.Context, encounterID, id int64) (ServiceOrder, error) {
	o, err := scanServiceOrder(s.pool.QueryRow(ctx, serviceOrderQuery+` WHERE es.id=$1 AND es.encounter_id=$2`, id, encounterID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ServiceOrder{}, problem.New(404, "ENCOUNTER_SERVICE_NOT_FOUND", "Không tìm thấy chỉ định dịch vụ.")
	}
	return o, err
}

const amendmentQuery = `SELECT id,encounter_id,sequence_number,amendment_type::text,reason,content,created_by_user_id,created_at FROM medical_encounter_amendments`

func scanAmendment(row scanner) (Amendment, error) {
	var a Amendment
	err := row.Scan(&a.ID, &a.EncounterID, &a.SequenceNumber, &a.AmendmentType, &a.Reason, &a.Content, &a.CreatedByUserID, &a.CreatedAt)
	return a, err
}
func (s *Service) getAmendment(ctx context.Context, encounterID, id int64) (Amendment, error) {
	return scanAmendment(s.pool.QueryRow(ctx, amendmentQuery+` WHERE id=$1 AND encounter_id=$2`, id, encounterID))
}
func (s *Service) listAmendments(ctx context.Context, id int64) ([]Amendment, error) {
	rows, err := s.pool.Query(ctx, amendmentQuery+` WHERE encounter_id=$1 ORDER BY sequence_number`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Amendment, 0)
	for rows.Next() {
		a, err := scanAmendment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func clinicalText(value string, max int) (string, error) {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimSpace(value)
	if len([]rune(value)) > max || strings.ContainsAny(value, "<>") {
		return "", invalid("Nội dung bệnh án không hợp lệ hoặc quá dài.")
	}
	return value, nil
}
func invalid(message string) error { return problem.New(422, "VALIDATION_ERROR", message) }
func notFound() error {
	return problem.New(404, "ENCOUNTER_NOT_FOUND", "Không tìm thấy bệnh án.")
}
func appointmentNotFound() error {
	return problem.New(404, "APPOINTMENT_NOT_FOUND", "Không tìm thấy lịch hẹn.")
}
func scopeDenied() error {
	return problem.New(404, "ENCOUNTER_NOT_FOUND", "Không tìm thấy bệnh án trong phạm vi được phép.")
}
func concurrent() error {
	return problem.New(409, "ENCOUNTER_CONCURRENT_UPDATE", "Dữ liệu đã thay đổi; hãy tải lại.")
}
func consultationMissing() error {
	return problem.New(409, "ENCOUNTER_CONSULTATION_SERVICE_NOT_CONFIGURED", "Chuyên khoa chưa có phí khám và giá đang hiệu lực.")
}
func mapConstraint(err error) error {
	switch pgutil.Constraint(err) {
	case "uq_medical_encounters_appointment":
		return problem.New(409, "ENCOUNTER_ALREADY_EXISTS", "Lịch hẹn đã có bệnh án.")
	case "uq_encounter_services_active":
		return problem.New(409, "ENCOUNTER_SERVICE_DUPLICATE", "Dịch vụ đã được chỉ định trong encounter.")
	case "uq_encounter_amendments_sequence":
		return problem.New(409, "ENCOUNTER_CONCURRENT_UPDATE", "Amendment được tạo đồng thời; hãy thử lại.")
	case "ck_medical_encounters_text_length":
		return invalid("Nội dung bệnh án không hợp lệ hoặc quá dài.")
	case "ck_vital_signs_values":
		return problem.New(422, "ENCOUNTER_VITAL_SIGNS_INVALID", "Chỉ số sinh tồn không hợp lệ.")
	default:
		return fmt.Errorf("medical encounter persistence: %w", err)
	}
}
