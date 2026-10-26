package appointment

import (
	"context"
	"crypto/sha256"
	"dncs/backend/internal/platform/pagination"
	"encoding/hex"
	"encoding/json"
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

const holdingStatuses = `'PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'`

type Service struct {
	pool     *pgxpool.Pool
	authz    *authorization.Store
	now      func() time.Time
	location *time.Location
}

func NewService(pool *pgxpool.Pool, authz *authorization.Store) *Service {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	return &Service{pool: pool, authz: authz, now: time.Now, location: location}
}

type Appointment struct {
	ID                  int64     `json:"id"`
	AppointmentCode     string    `json:"appointment_code"`
	DoctorScheduleID    int64     `json:"doctor_schedule_id"`
	PatientID           int64     `json:"patient_id"`
	PatientCode         string    `json:"patient_code"`
	PatientName         string    `json:"patient_name"`
	DoctorProfileID     int64     `json:"doctor_profile_id"`
	DoctorCode          string    `json:"doctor_code"`
	DoctorName          string    `json:"doctor_name"`
	SpecialtyID         int64     `json:"specialty_id"`
	SpecialtyCode       string    `json:"specialty_code"`
	SpecialtyName       string    `json:"specialty_name"`
	RoomID              int64     `json:"room_id"`
	RoomCode            string    `json:"room_code"`
	RoomName            string    `json:"room_name"`
	StartAt             time.Time `json:"start_at"`
	EndAt               time.Time `json:"end_at"`
	Status              string    `json:"status"`
	Source              string    `json:"source"`
	ApprovalMode        string    `json:"approval_mode_at_booking"`
	CheckInEarlyMinutes int16     `json:"check_in_early_minutes_at_booking"`
	CheckInLateMinutes  int16     `json:"check_in_late_minutes_at_booking"`
	NoShowGraceMinutes  int16     `json:"no_show_grace_minutes_at_booking"`
	BookedByUserID      int64     `json:"booked_by_user_id"`
	Version             int64     `json:"version"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type BookInput struct {
	DoctorScheduleID int64
	PatientID        int64
	SpecialtyID      int64
	StartAt          string
	IdempotencyKey   string
}

type FollowUpInput struct {
	DoctorScheduleID int64
	StartAt          string
	IdempotencyKey   string
}

func (s *Service) FollowUp(ctx context.Context, actor auth.Principal, encounterID int64, input FollowUpInput) (Appointment, bool, error) {
	key := strings.TrimSpace(input.IdempotencyKey)
	if len(key) < 16 || len(key) > 128 || key != input.IdempotencyKey || !printableASCII(key) {
		return Appointment{}, false, problem.New(422, "IDEMPOTENCY_KEY_INVALID", "Idempotency-Key phải gồm 16-128 ký tự ASCII in được.")
	}
	startAt, err := time.Parse(time.RFC3339, input.StartAt)
	if err != nil || input.DoctorScheduleID < 1 {
		return Appointment{}, false, invalid("Thông tin slot tái khám không hợp lệ.")
	}
	var sourceAppointmentID, patientID, doctorID, specialtyID, doctorUserID, staffID, patientUserID int64
	var encounterStatus string
	err = s.pool.QueryRow(ctx, `
		SELECT e.appointment_id,e.patient_id,e.doctor_profile_id,e.specialty_id,e.status::text,
		       sp.user_id,sp.id,p.user_id
		FROM medical_encounters e JOIN doctor_profiles d ON d.id=e.doctor_profile_id
		JOIN staff_profiles sp ON sp.id=d.staff_profile_id JOIN patients p ON p.id=e.patient_id
		WHERE e.id=$1 AND NOT e.is_deleted`, encounterID).Scan(&sourceAppointmentID, &patientID, &doctorID, &specialtyID, &encounterStatus, &doctorUserID, &staffID, &patientUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Appointment{}, false, problem.New(404, "ENCOUNTER_NOT_FOUND", "Không tìm thấy bệnh án.")
	}
	if err != nil {
		return Appointment{}, false, err
	}
	if doctorUserID != actor.UserID {
		return Appointment{}, false, problem.New(404, "ENCOUNTER_NOT_FOUND", "Không tìm thấy bệnh án trong phạm vi được phép.")
	}
	if encounterStatus != "FINALIZED" {
		return Appointment{}, false, problem.New(409, "APPOINTMENT_FOLLOW_UP_ENCOUNTER_NOT_FINALIZED", "Chỉ được tạo tái khám sau khi bệnh án đã hoàn tất.")
	}
	var snapshot scheduleSnapshot
	if err := s.pool.QueryRow(ctx, `SELECT id,doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status::text FROM doctor_schedules WHERE id=$1`, input.DoctorScheduleID).Scan(&snapshot.ID, &snapshot.DoctorID, &snapshot.RoomID, &snapshot.StartAt, &snapshot.EndAt, &snapshot.Duration, &snapshot.Status); errors.Is(err, pgx.ErrNoRows) {
		return Appointment{}, false, problem.New(404, "SCHEDULE_NOT_FOUND", "Không tìm thấy lịch bác sĩ.")
	} else if err != nil {
		return Appointment{}, false, err
	}
	if snapshot.DoctorID != doctorID {
		return Appointment{}, false, problem.New(409, "APPOINTMENT_FOLLOW_UP_DOCTOR_MISMATCH", "Lịch tái khám phải thuộc bác sĩ phụ trách encounter.")
	}
	hashBytes := sha256.Sum256([]byte(fmt.Sprintf("follow-up:%d:%d:%s", encounterID, input.DoctorScheduleID, startAt.UTC().Format(time.RFC3339Nano))))
	hash := hex.EncodeToString(hashBytes[:])
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Appointment{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	principalKey := fmt.Sprintf("user:%d", actor.UserID)
	var recordID int64
	err = tx.QueryRow(ctx, `INSERT INTO idempotency_records(principal_key,actor_user_id,operation_scope,idempotency_key,request_hash,status,expires_at) VALUES($1,$2,'appointment.follow_up',$3,$4,'PROCESSING',now()+interval '24 hours') ON CONFLICT(principal_key,operation_scope,idempotency_key) DO NOTHING RETURNING id`, principalKey, actor.UserID, key, hash).Scan(&recordID)
	if errors.Is(err, pgx.ErrNoRows) {
		var storedHash, state string
		var resourceID *int64
		if err := tx.QueryRow(ctx, `SELECT request_hash,status::text,resource_id FROM idempotency_records WHERE principal_key=$1 AND operation_scope='appointment.follow_up' AND idempotency_key=$2 FOR UPDATE`, principalKey, key).Scan(&storedHash, &state, &resourceID); err != nil {
			return Appointment{}, false, err
		}
		if storedHash != hash {
			return Appointment{}, false, problem.New(409, "IDEMPOTENCY_KEY_REUSED", "Idempotency-Key đã được dùng với nội dung khác.")
		}
		if state == "COMPLETED" && resourceID != nil {
			if err := tx.Commit(ctx); err != nil {
				return Appointment{}, false, err
			}
			item, err := s.getRaw(ctx, *resourceID)
			return item, true, err
		}
		return Appointment{}, false, problem.New(409, "IDEMPOTENCY_IN_PROGRESS", "Yêu cầu tái khám đang được xử lý.")
	}
	if err != nil {
		return Appointment{}, false, err
	}
	settings, err := s.lockSettings(ctx, tx)
	if err != nil {
		return Appointment{}, false, err
	}
	var specialtyCode, specialtyName string
	var specialtyActive bool
	if err := tx.QueryRow(ctx, `SELECT code,name,status='ACTIVE' AND NOT is_deleted FROM specialties WHERE id=$1 FOR SHARE`, specialtyID).Scan(&specialtyCode, &specialtyName, &specialtyActive); err != nil {
		return Appointment{}, false, err
	}
	var roomCode, roomName string
	var roomActive bool
	if err := tx.QueryRow(ctx, `SELECT code,name,status='ACTIVE' AND NOT is_deleted FROM rooms WHERE id=$1 FOR SHARE`, snapshot.RoomID).Scan(&roomCode, &roomName, &roomActive); err != nil {
		return Appointment{}, false, err
	}
	var accountActive bool
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM users WHERE id=$1 FOR SHARE`, doctorUserID).Scan(&accountActive); err != nil {
		return Appointment{}, false, err
	}
	var doctorName string
	var staffActive bool
	if err := tx.QueryRow(ctx, `SELECT full_name,employment_status='ACTIVE' AND NOT is_deleted FROM staff_profiles WHERE id=$1 FOR SHARE`, staffID).Scan(&doctorName, &staffActive); err != nil {
		return Appointment{}, false, err
	}
	var doctorCode string
	var doctorActive bool
	if err := tx.QueryRow(ctx, `SELECT doctor_code,status='ACTIVE' AND NOT is_deleted FROM doctor_profiles WHERE id=$1 FOR SHARE`, doctorID).Scan(&doctorCode, &doctorActive); err != nil {
		return Appointment{}, false, err
	}
	var relation bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doctor_specialties WHERE doctor_profile_id=$1 AND specialty_id=$2 AND NOT is_deleted)`, doctorID, specialtyID).Scan(&relation); err != nil {
		return Appointment{}, false, err
	}
	if !specialtyActive || !roomActive || !accountActive || !staffActive || !doctorActive || !relation {
		return Appointment{}, false, problem.New(409, "SLOT_NOT_AVAILABLE", "Nguồn lực tái khám không còn khả dụng.")
	}
	if err := tx.QueryRow(ctx, `SELECT id,doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status::text FROM doctor_schedules WHERE id=$1 FOR UPDATE`, input.DoctorScheduleID).Scan(&snapshot.ID, &snapshot.DoctorID, &snapshot.RoomID, &snapshot.StartAt, &snapshot.EndAt, &snapshot.Duration, &snapshot.Status); err != nil {
		return Appointment{}, false, err
	}
	endAt := startAt.Add(time.Duration(snapshot.Duration) * time.Minute)
	if snapshot.Status != "ACTIVE" || snapshot.DoctorID != doctorID || startAt.Before(snapshot.StartAt) || endAt.After(snapshot.EndAt) || startAt.Sub(snapshot.StartAt)%(time.Duration(snapshot.Duration)*time.Minute) != 0 {
		return Appointment{}, false, problem.New(409, "SLOT_NOT_AVAILABLE", "Slot tái khám không còn khả dụng.")
	}
	now := s.now()
	if startAt.Before(now.Add(time.Duration(settings.MinLead)*time.Minute)) || startAt.After(now.AddDate(0, 0, settings.MaxAdvance)) {
		return Appointment{}, false, problem.New(409, "BOOKING_WINDOW_INVALID", "Slot nằm ngoài cửa sổ cho phép đặt lịch.")
	}
	var patientAccountActive bool
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM users WHERE id=$1 FOR SHARE`, patientUserID).Scan(&patientAccountActive); err != nil {
		return Appointment{}, false, err
	}
	if !patientAccountActive {
		return Appointment{}, false, problem.New(409, "PATIENT_INACTIVE", "Bệnh nhân không hoạt động.")
	}
	var patientCode, patientName string
	var patientActive bool
	if err := tx.QueryRow(ctx, `SELECT patient_code,full_name,NOT is_deleted FROM patients WHERE id=$1 FOR UPDATE`, patientID).Scan(&patientCode, &patientName, &patientActive); err != nil {
		return Appointment{}, false, err
	}
	if !patientActive {
		return Appointment{}, false, problem.New(409, "PATIENT_INACTIVE", "Bệnh nhân không hoạt động.")
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM appointments WHERE id=$1 FOR SHARE`, sourceAppointmentID); err != nil {
		return Appointment{}, false, err
	}
	var lockedStatus string
	if err := tx.QueryRow(ctx, `SELECT status::text FROM medical_encounters WHERE id=$1 FOR UPDATE`, encounterID).Scan(&lockedStatus); err != nil {
		return Appointment{}, false, err
	}
	if lockedStatus != "FINALIZED" {
		return Appointment{}, false, problem.New(409, "APPOINTMENT_FOLLOW_UP_ENCOUNTER_NOT_FINALIZED", "Bệnh án chưa hoàn tất.")
	}
	var activeFollowUp bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM appointments WHERE follow_up_from_encounter_id=$1 AND status IN (`+holdingStatuses+`))`, encounterID).Scan(&activeFollowUp); err != nil {
		return Appointment{}, false, err
	}
	if activeFollowUp {
		return Appointment{}, false, problem.New(409, "APPOINTMENT_FOLLOW_UP_ACTIVE_EXISTS", "Encounter đã có một lịch tái khám đang giữ chỗ.")
	}
	var holdingCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM appointments WHERE patient_id=$1 AND end_at>now() AND status IN (`+holdingStatuses+`)`, patientID).Scan(&holdingCount); err != nil {
		return Appointment{}, false, err
	}
	if holdingCount >= settings.MaxHolding {
		return Appointment{}, false, problem.New(409, "PATIENT_FUTURE_APPOINTMENT_LIMIT", "Bệnh nhân đã đạt giới hạn lịch hẹn tương lai.")
	}
	var appointmentID int64
	err = tx.QueryRow(ctx, `INSERT INTO appointments(doctor_schedule_id,patient_id,doctor_profile_id,specialty_id,room_id,doctor_code_at_booking,doctor_name_at_booking,specialty_code_at_booking,specialty_name_at_booking,room_code_at_booking,room_name_at_booking,start_at,end_at,status,source,approval_mode_at_booking,check_in_early_minutes_at_booking,check_in_late_minutes_at_booking,no_show_grace_minutes_at_booking,booked_by_user_id,follow_up_from_encounter_id,idempotency_key,idempotency_request_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'CONFIRMED','FOLLOW_UP','DIRECT_FOLLOW_UP',$14,$15,$16,$17,$18,$19,$20) RETURNING id`, snapshot.ID, patientID, doctorID, specialtyID, snapshot.RoomID, doctorCode, doctorName, specialtyCode, specialtyName, roomCode, roomName, startAt, endAt, settings.Early, settings.Late, settings.NoShowGrace, actor.UserID, encounterID, key, hash).Scan(&appointmentID)
	if err != nil {
		return Appointment{}, false, mapConstraint(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO appointment_status_histories(appointment_id,to_status,actor_user_id,actor_type,metadata) VALUES($1,'CONFIRMED',$2,'USER',jsonb_build_object('source','FOLLOW_UP','encounter_id',$3::bigint))`, appointmentID, actor.UserID, encounterID); err != nil {
		return Appointment{}, false, err
	}
	e := audit.UserEvent(actor.UserID, "APPOINTMENT", "APPOINTMENT_FOLLOW_UP_CREATED", "APPOINTMENT", appointmentID)
	e.Metadata = map[string]any{"encounter_id": encounterID, "patient_id": patientID, "doctor_profile_id": doctorID}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return Appointment{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE idempotency_records SET status='COMPLETED',resource_type='APPOINTMENT',resource_id=$2,http_status=201,response_body=jsonb_build_object('appointment_id',$2::bigint),completed_at=now(),updated_at=now() WHERE id=$1`, recordID, appointmentID); err != nil {
		return Appointment{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Appointment{}, false, mapConstraint(err)
	}
	item, err := s.getRaw(ctx, appointmentID)
	return item, false, err
}

type bookingSettings struct {
	AutoApproval bool
	MinLead      int
	MaxAdvance   int
	Early        int16
	Late         int16
	NoShowGrace  int16
	MaxHolding   int
}

type scheduleSnapshot struct {
	ID, DoctorID, RoomID int64
	StartAt, EndAt       time.Time
	Duration             int16
	Status               string
}

func (s *Service) Book(ctx context.Context, actor auth.Principal, input BookInput) (Appointment, bool, error) {
	key := strings.TrimSpace(input.IdempotencyKey)
	if len(key) < 16 || len(key) > 128 || key != input.IdempotencyKey || !printableASCII(key) {
		return Appointment{}, false, problem.New(422, "IDEMPOTENCY_KEY_INVALID", "Idempotency-Key phải gồm 16-128 ký tự ASCII in được.")
	}
	startAt, err := time.Parse(time.RFC3339, input.StartAt)
	if err != nil || input.DoctorScheduleID < 1 || input.SpecialtyID < 1 {
		return Appointment{}, false, invalid("Thông tin slot không hợp lệ.")
	}
	if actor.AccountType == auth.AccountTypePatient {
		if err := s.pool.QueryRow(ctx, `SELECT id FROM patients WHERE user_id=$1 AND is_deleted=false`, actor.UserID).Scan(&input.PatientID); errors.Is(err, pgx.ErrNoRows) {
			return Appointment{}, false, problem.New(409, "PATIENT_PROFILE_REQUIRED", "Không tìm thấy hồ sơ bệnh nhân hoạt động.")
		} else if err != nil {
			return Appointment{}, false, err
		}
	}
	if input.PatientID < 1 {
		return Appointment{}, false, invalid("patient_id là bắt buộc khi đặt tại quầy.")
	}
	hash := bookingHash(input.DoctorScheduleID, input.PatientID, input.SpecialtyID, startAt)
	var snapshot scheduleSnapshot
	if err := s.pool.QueryRow(ctx, `SELECT id,doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status::text FROM doctor_schedules WHERE id=$1`, input.DoctorScheduleID).Scan(&snapshot.ID, &snapshot.DoctorID, &snapshot.RoomID, &snapshot.StartAt, &snapshot.EndAt, &snapshot.Duration, &snapshot.Status); errors.Is(err, pgx.ErrNoRows) {
		return Appointment{}, false, problem.New(404, "SCHEDULE_NOT_FOUND", "Không tìm thấy lịch bác sĩ.")
	} else if err != nil {
		return Appointment{}, false, err
	}
	var doctorUserID, staffID int64
	if err := s.pool.QueryRow(ctx, `SELECT sp.user_id,sp.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id WHERE d.id=$1`, snapshot.DoctorID).Scan(&doctorUserID, &staffID); err != nil {
		return Appointment{}, false, err
	}
	var patientUserID int64
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM patients WHERE id=$1`, input.PatientID).Scan(&patientUserID); errors.Is(err, pgx.ErrNoRows) {
		return Appointment{}, false, problem.New(404, "PATIENT_NOT_FOUND", "Không tìm thấy bệnh nhân.")
	} else if err != nil {
		return Appointment{}, false, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Appointment{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	principalKey := fmt.Sprintf("user:%d", actor.UserID)
	var recordID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO idempotency_records(principal_key,actor_user_id,operation_scope,idempotency_key,request_hash,status,expires_at)
		VALUES($1,$2,'appointment.book',$3,$4,'PROCESSING',now()+interval '24 hours')
		ON CONFLICT(principal_key,operation_scope,idempotency_key) DO NOTHING RETURNING id`, principalKey, actor.UserID, key, hash).Scan(&recordID)
	if errors.Is(err, pgx.ErrNoRows) {
		var storedHash, status string
		var resourceID *int64
		if err := tx.QueryRow(ctx, `SELECT request_hash,status::text,resource_id FROM idempotency_records WHERE principal_key=$1 AND operation_scope='appointment.book' AND idempotency_key=$2 FOR UPDATE`, principalKey, key).Scan(&storedHash, &status, &resourceID); err != nil {
			return Appointment{}, false, err
		}
		if storedHash != hash {
			return Appointment{}, false, problem.New(409, "IDEMPOTENCY_KEY_REUSED", "Idempotency-Key đã được dùng với nội dung khác.")
		}
		if status == "COMPLETED" && resourceID != nil {
			if err := tx.Commit(ctx); err != nil {
				return Appointment{}, false, err
			}
			item, err := s.getRaw(ctx, *resourceID)
			return item, true, err
		}
		return Appointment{}, false, problem.New(409, "IDEMPOTENCY_IN_PROGRESS", "Yêu cầu cùng Idempotency-Key đang được xử lý.")
	}
	if err != nil {
		return Appointment{}, false, err
	}

	settings, err := s.lockSettings(ctx, tx)
	if err != nil {
		return Appointment{}, false, err
	}
	var specialtyCode, specialtyName string
	var specialtyActive bool
	if err := tx.QueryRow(ctx, `SELECT code,name,status='ACTIVE' AND NOT is_deleted FROM specialties WHERE id=$1 FOR SHARE`, input.SpecialtyID).Scan(&specialtyCode, &specialtyName, &specialtyActive); errors.Is(err, pgx.ErrNoRows) {
		return Appointment{}, false, problem.New(404, "SPECIALTY_NOT_FOUND", "Không tìm thấy chuyên khoa.")
	} else if err != nil {
		return Appointment{}, false, err
	}
	if !specialtyActive {
		return Appointment{}, false, problem.New(409, "SPECIALTY_INACTIVE", "Chuyên khoa không hoạt động.")
	}
	var roomCode, roomName string
	var roomActive bool
	if err := tx.QueryRow(ctx, `SELECT code,name,status='ACTIVE' AND NOT is_deleted FROM rooms WHERE id=$1 FOR SHARE`, snapshot.RoomID).Scan(&roomCode, &roomName, &roomActive); err != nil {
		return Appointment{}, false, err
	}
	if !roomActive {
		return Appointment{}, false, problem.New(409, "ROOM_INACTIVE", "Phòng không hoạt động.")
	}
	var doctorAccountActive bool
	if err := tx.QueryRow(ctx, `SELECT status<>'INACTIVE' AND NOT is_deleted FROM users WHERE id=$1 FOR SHARE`, doctorUserID).Scan(&doctorAccountActive); err != nil {
		return Appointment{}, false, err
	}
	var doctorName string
	var staffActive bool
	if err := tx.QueryRow(ctx, `SELECT full_name,employment_status='ACTIVE' AND NOT is_deleted FROM staff_profiles WHERE id=$1 FOR SHARE`, staffID).Scan(&doctorName, &staffActive); err != nil {
		return Appointment{}, false, err
	}
	var doctorCode string
	var doctorActive bool
	if err := tx.QueryRow(ctx, `SELECT doctor_code,status='ACTIVE' AND NOT is_deleted FROM doctor_profiles WHERE id=$1 FOR SHARE`, snapshot.DoctorID).Scan(&doctorCode, &doctorActive); err != nil {
		return Appointment{}, false, err
	}
	if !doctorAccountActive || !staffActive || !doctorActive {
		return Appointment{}, false, problem.New(409, "DOCTOR_INACTIVE", "Bác sĩ không hoạt động.")
	}
	var relationID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM doctor_specialties WHERE doctor_profile_id=$1 AND specialty_id=$2 AND is_deleted=false FOR SHARE`, snapshot.DoctorID, input.SpecialtyID).Scan(&relationID); errors.Is(err, pgx.ErrNoRows) {
		return Appointment{}, false, problem.New(409, "DOCTOR_SPECIALTY_MISMATCH", "Bác sĩ không thuộc chuyên khoa đã chọn.")
	} else if err != nil {
		return Appointment{}, false, err
	}
	if err := tx.QueryRow(ctx, `SELECT id,doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status::text FROM doctor_schedules WHERE id=$1 FOR UPDATE`, input.DoctorScheduleID).Scan(&snapshot.ID, &snapshot.DoctorID, &snapshot.RoomID, &snapshot.StartAt, &snapshot.EndAt, &snapshot.Duration, &snapshot.Status); err != nil {
		return Appointment{}, false, err
	}
	if snapshot.Status != "ACTIVE" || startAt.Before(snapshot.StartAt) {
		return Appointment{}, false, problem.New(409, "SLOT_NOT_AVAILABLE", "Slot không còn khả dụng.")
	}
	endAt := startAt.Add(time.Duration(snapshot.Duration) * time.Minute)
	if endAt.After(snapshot.EndAt) || startAt.Sub(snapshot.StartAt)%(time.Duration(snapshot.Duration)*time.Minute) != 0 {
		return Appointment{}, false, problem.New(409, "SLOT_NOT_AVAILABLE", "Slot không thuộc lịch đã chọn.")
	}
	now := s.now()
	if startAt.Before(now.Add(time.Duration(settings.MinLead)*time.Minute)) || startAt.After(now.AddDate(0, 0, settings.MaxAdvance)) {
		return Appointment{}, false, problem.New(409, "BOOKING_WINDOW_INVALID", "Slot nằm ngoài cửa sổ cho phép đặt lịch.")
	}
	var patientCode, patientName string
	var patientAccountActive bool
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM users WHERE id=$1 FOR UPDATE`, patientUserID).Scan(&patientAccountActive); err != nil {
		return Appointment{}, false, err
	}
	var patientActive bool
	if err := tx.QueryRow(ctx, `SELECT patient_code,full_name,NOT is_deleted FROM patients WHERE id=$1 FOR UPDATE`, input.PatientID).Scan(&patientCode, &patientName, &patientActive); err != nil {
		return Appointment{}, false, err
	}
	if !patientAccountActive || !patientActive {
		return Appointment{}, false, problem.New(409, "PATIENT_INACTIVE", "Bệnh nhân không hoạt động.")
	}
	var holdingCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM appointments WHERE patient_id=$1 AND end_at>now() AND status IN (`+holdingStatuses+`)`, input.PatientID).Scan(&holdingCount); err != nil {
		return Appointment{}, false, err
	}
	if holdingCount >= settings.MaxHolding {
		return Appointment{}, false, problem.New(409, "PATIENT_FUTURE_APPOINTMENT_LIMIT", "Bệnh nhân đã đạt giới hạn lịch hẹn tương lai.")
	}
	status, mode, source := "PENDING_APPROVAL", "MANUAL", "COUNTER"
	if settings.AutoApproval {
		status, mode = "CONFIRMED", "AUTO"
	}
	if actor.AccountType == auth.AccountTypePatient {
		source = "PATIENT_SELF"
	}
	var appointmentID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO appointments(doctor_schedule_id,patient_id,doctor_profile_id,specialty_id,room_id,
			doctor_code_at_booking,doctor_name_at_booking,specialty_code_at_booking,specialty_name_at_booking,
			room_code_at_booking,room_name_at_booking,start_at,end_at,status,source,approval_mode_at_booking,
			check_in_early_minutes_at_booking,check_in_late_minutes_at_booking,no_show_grace_minutes_at_booking,
			booked_by_user_id,idempotency_key,idempotency_request_hash)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		RETURNING id`, snapshot.ID, input.PatientID, snapshot.DoctorID, input.SpecialtyID, snapshot.RoomID,
		doctorCode, doctorName, specialtyCode, specialtyName, roomCode, roomName, startAt, endAt, status, source,
		mode, settings.Early, settings.Late, settings.NoShowGrace, actor.UserID, key, hash).Scan(&appointmentID)
	if err != nil {
		return Appointment{}, false, mapConstraint(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO appointment_status_histories(appointment_id,to_status,actor_user_id,actor_type,metadata) VALUES($1,$2,$3,'USER',jsonb_build_object('source',$4::text))`, appointmentID, status, actor.UserID, source); err != nil {
		return Appointment{}, false, err
	}
	actionCode := "APPOINTMENT_CREATED"
	if status == "CONFIRMED" {
		actionCode = "APPOINTMENT_AUTO_CONFIRMED"
	}
	event := audit.UserEvent(actor.UserID, "APPOINTMENT", actionCode, "APPOINTMENT", appointmentID)
	event.Metadata = map[string]any{"source": source, "status": status, "schedule_id": snapshot.ID}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Appointment{}, false, err
	}
	response, _ := json.Marshal(map[string]any{"appointment_id": appointmentID})
	if _, err := tx.Exec(ctx, `UPDATE idempotency_records SET status='COMPLETED',resource_type='APPOINTMENT',resource_id=$2,http_status=201,response_body=$3::jsonb,completed_at=now(),updated_at=now() WHERE id=$1`, recordID, appointmentID, response); err != nil {
		return Appointment{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Appointment{}, false, mapConstraint(err)
	}
	item, err := s.getRaw(ctx, appointmentID)
	return item, false, err
}

func (s *Service) lockSettings(ctx context.Context, tx pgx.Tx) (bookingSettings, error) {
	rows, err := tx.Query(ctx, `SELECT setting_key,COALESCE(boolean_value,false),COALESCE(integer_value,0) FROM system_settings WHERE setting_key = ANY($1) ORDER BY setting_key FOR SHARE`, []string{"appointment_auto_approval_enabled", "appointment_booking_min_lead_minutes", "appointment_booking_max_advance_days", "appointment_check_in_early_minutes", "appointment_check_in_late_minutes", "appointment_no_show_grace_minutes", "appointment_max_holding_future_per_patient"})
	if err != nil {
		return bookingSettings{}, err
	}
	defer rows.Close()
	var result bookingSettings
	count := 0
	for rows.Next() {
		var key string
		var boolean bool
		var integer int
		if err := rows.Scan(&key, &boolean, &integer); err != nil {
			return bookingSettings{}, err
		}
		count++
		switch key {
		case "appointment_auto_approval_enabled":
			result.AutoApproval = boolean
		case "appointment_booking_min_lead_minutes":
			result.MinLead = integer
		case "appointment_booking_max_advance_days":
			result.MaxAdvance = integer
		case "appointment_check_in_early_minutes":
			result.Early = int16(integer)
		case "appointment_check_in_late_minutes":
			result.Late = int16(integer)
		case "appointment_no_show_grace_minutes":
			result.NoShowGrace = int16(integer)
		case "appointment_max_holding_future_per_patient":
			result.MaxHolding = integer
		}
	}
	if err := rows.Err(); err != nil {
		return bookingSettings{}, err
	}
	if count != 7 || result.NoShowGrace > result.Late {
		return bookingSettings{}, problem.New(500, "SETTINGS_INVALID", "Cấu hình đặt lịch không đầy đủ hoặc không hợp lệ.")
	}
	return result, nil
}

func bookingHash(scheduleID, patientID, specialtyID int64, start time.Time) string {
	payload := struct {
		ScheduleID, PatientID, SpecialtyID int64
		StartAt                            string
	}{scheduleID, patientID, specialtyID, start.UTC().Format(time.RFC3339Nano)}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func printableASCII(value string) bool {
	for _, r := range value {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}

type TransitionInput struct {
	ExpectedVersion int64
	Reason          string
}

func (s *Service) Transition(ctx context.Context, actor auth.Principal, id int64, action string, input TransitionInput) (Appointment, error) {
	current, err := s.getRaw(ctx, id)
	if err != nil {
		return Appointment{}, err
	}
	if err := s.authorizeTransition(ctx, actor, current, action); err != nil {
		return Appointment{}, err
	}
	if (action == "reject" || action == "cancel" || action == "no-show") && strings.TrimSpace(input.Reason) == "" {
		return Appointment{}, invalid("Lý do là bắt buộc.")
	}
	var doctorUserID, staffID, patientUserID int64
	if err := s.pool.QueryRow(ctx, `SELECT sp.user_id,sp.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id WHERE d.id=$1`, current.DoctorProfileID).Scan(&doctorUserID, &staffID); err != nil {
		return Appointment{}, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM patients WHERE id=$1`, current.PatientID).Scan(&patientUserID); err != nil {
		return Appointment{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Appointment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Same global order as booking/mutations: room -> doctor aggregate -> schedule -> patient aggregate -> appointment.
	if _, err := tx.Exec(ctx, `SELECT id FROM rooms WHERE id=$1 FOR SHARE`, current.RoomID); err != nil {
		return Appointment{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR SHARE`, doctorUserID); err != nil {
		return Appointment{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM staff_profiles WHERE id=$1 FOR SHARE`, staffID); err != nil {
		return Appointment{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM doctor_profiles WHERE id=$1 FOR SHARE`, current.DoctorProfileID); err != nil {
		return Appointment{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM doctor_schedules WHERE id=$1 FOR UPDATE`, current.DoctorScheduleID); err != nil {
		return Appointment{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR SHARE`, patientUserID); err != nil {
		return Appointment{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM patients WHERE id=$1 FOR SHARE`, current.PatientID); err != nil {
		return Appointment{}, err
	}
	var version int64
	var from string
	var startAt time.Time
	var early, late, grace int16
	if err := tx.QueryRow(ctx, `SELECT version,status::text,start_at,check_in_early_minutes_at_booking,check_in_late_minutes_at_booking,no_show_grace_minutes_at_booking FROM appointments WHERE id=$1 FOR UPDATE`, id).Scan(&version, &from, &startAt, &early, &late, &grace); err != nil {
		return Appointment{}, err
	}
	if version != input.ExpectedVersion {
		return Appointment{}, concurrent()
	}
	to, err := transitionTarget(from, action)
	if err != nil {
		return Appointment{}, err
	}
	now := s.now()
	if action == "check-in" {
		if now.Before(startAt.Add(-time.Duration(early)*time.Minute)) || now.After(startAt.Add(time.Duration(late)*time.Minute)) {
			return Appointment{}, problem.New(409, "APPOINTMENT_CHECK_IN_WINDOW_INVALID", "Chưa đến hoặc đã quá cửa sổ check-in.")
		}
	}
	if action == "no-show" && now.Before(startAt.Add(time.Duration(grace)*time.Minute)) {
		return Appointment{}, problem.New(409, "APPOINTMENT_NO_SHOW_TOO_EARLY", "Chưa đến thời điểm được đánh dấu vắng mặt.")
	}
	if _, err := tx.Exec(ctx, `UPDATE appointments SET status=$2,updated_at=now(),version=version+1 WHERE id=$1`, id, to); err != nil {
		return Appointment{}, mapConstraint(err)
	}
	reason := strings.TrimSpace(input.Reason)
	if _, err := tx.Exec(ctx, `INSERT INTO appointment_status_histories(appointment_id,from_status,to_status,actor_user_id,actor_type,reason) VALUES($1,$2,$3,$4,'USER',NULLIF($5,''))`, id, from, to, actor.UserID, reason); err != nil {
		return Appointment{}, err
	}
	actionCode := map[string]string{
		"approve": "APPOINTMENT_APPROVED", "reject": "APPOINTMENT_REJECTED",
		"cancel": "APPOINTMENT_CANCELLED", "check-in": "APPOINTMENT_CHECKED_IN",
		"no-show": "APPOINTMENT_NO_SHOW_MARKED",
	}[action]
	event := audit.UserEvent(actor.UserID, "APPOINTMENT", actionCode, "APPOINTMENT", id)
	event.Metadata = map[string]any{"from": from, "to": to, "reason": reason}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Appointment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Appointment{}, mapConstraint(err)
	}
	return s.getRaw(ctx, id)
}

func transitionTarget(from, action string) (string, error) {
	allowed := map[string]map[string]string{
		"approve":  {"PENDING_APPROVAL": "CONFIRMED"},
		"reject":   {"PENDING_APPROVAL": "REJECTED"},
		"cancel":   {"PENDING_APPROVAL": "CANCELLED", "CONFIRMED": "CANCELLED"},
		"check-in": {"CONFIRMED": "CHECKED_IN"},
		"no-show":  {"CONFIRMED": "NO_SHOW"},
	}
	if to, ok := allowed[action][from]; ok {
		return to, nil
	}
	return "", problem.New(409, "APPOINTMENT_TRANSITION_INVALID", "Chuyển trạng thái lịch hẹn không hợp lệ.")
}

func (s *Service) authorizeTransition(ctx context.Context, actor auth.Principal, item Appointment, action string) error {
	if action == "check-in" {
		return nil // Functional permission is enforced by the route; check-in has clinic-wide scope.
	}
	if action == "no-show" {
		viewAll, err := s.authz.Allowed(ctx, actor, "appointment.view.all")
		if err != nil {
			return err
		}
		if viewAll.Allowed {
			return nil
		}
		own, err := s.ownsDoctor(ctx, actor.UserID, item.DoctorProfileID)
		if err != nil {
			return err
		}
		if own {
			return nil
		}
		return problem.New(403, "APPOINTMENT_SCOPE_FORBIDDEN", "Không được thao tác trên lịch hẹn này.")
	}
	permissionAll := map[string]string{"approve": "appointment.approve.all", "reject": "appointment.approve.all", "cancel": "appointment.cancel.all"}[action]
	if permissionAll == "" {
		return problem.New(403, "APPOINTMENT_SCOPE_FORBIDDEN", "Không được thao tác trên lịch hẹn này.")
	}
	decision, err := s.authz.Allowed(ctx, actor, permissionAll)
	if err != nil {
		return err
	}
	if decision.Allowed {
		if action == "cancel" && item.Status != "CONFIRMED" {
			return problem.New(409, "APPOINTMENT_TRANSITION_INVALID", "Nhân viên chỉ được hủy lịch đã xác nhận trong baseline MVP.")
		}
		return nil
	}
	if action == "cancel" && actor.AccountType == auth.AccountTypePatient {
		var own bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patients WHERE id=$1 AND user_id=$2)`, item.PatientID, actor.UserID).Scan(&own)
		if err != nil {
			return err
		}
		if own {
			if !s.now().Before(item.StartAt) {
				return problem.New(409, "APPOINTMENT_CANCEL_TOO_LATE", "Không thể tự hủy khi đã đến giờ hẹn.")
			}
			return nil
		}
	}
	if action == "approve" || action == "reject" {
		own, err := s.ownsDoctor(ctx, actor.UserID, item.DoctorProfileID)
		if err != nil {
			return err
		}
		if own {
			return nil
		}
	}
	return problem.New(403, "APPOINTMENT_SCOPE_FORBIDDEN", "Không được thao tác trên lịch hẹn này.")
}

func (s *Service) Get(ctx context.Context, actor auth.Principal, id int64) (Appointment, error) {
	item, err := s.getRaw(ctx, id)
	if err != nil {
		return Appointment{}, err
	}
	if err := s.authorizeView(ctx, actor, item); err != nil {
		return Appointment{}, err
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, actor auth.Principal, ownPatient bool, from, to, status string, patientID, doctorID, specialtyID, roomID int64, page, size int) ([]Appointment, error) {
	return s.list(ctx, actor, ownPatient, from, to, status, patientID, doctorID, specialtyID, roomID, page, size, false)
}

func (s *Service) ListPage(ctx context.Context, actor auth.Principal, ownPatient bool, from, to, status string, patientID, doctorID, specialtyID, roomID int64, page, size int) (pagination.Page[Appointment], error) {
	page, size = pagination.Normalize(page, size)
	items, err := s.list(ctx, actor, ownPatient, from, to, status, patientID, doctorID, specialtyID, roomID, page, size, true)
	return pagination.From(items, page, size), err
}

func (s *Service) list(ctx context.Context, actor auth.Principal, ownPatient bool, from, to, status string, patientID, doctorID, specialtyID, roomID int64, page, size int, extra bool) ([]Appointment, error) {
	fetch := size
	if extra {
		fetch++
	}
	if ownPatient || actor.AccountType == auth.AccountTypePatient {
		if err := s.pool.QueryRow(ctx, `SELECT id FROM patients WHERE user_id=$1 AND is_deleted=false`, actor.UserID).Scan(&patientID); errors.Is(err, pgx.ErrNoRows) {
			return []Appointment{}, nil
		} else if err != nil {
			return nil, err
		}
	} else {
		decision, err := s.authz.Allowed(ctx, actor, "appointment.view.all")
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			if err := s.pool.QueryRow(ctx, `SELECT d.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id WHERE sp.user_id=$1 AND d.is_deleted=false`, actor.UserID).Scan(&doctorID); errors.Is(err, pgx.ErrNoRows) {
				return []Appointment{}, nil
			} else if err != nil {
				return nil, err
			}
		}
	}
	fromTime, toTime := s.now().AddDate(0, -6, 0), s.now().AddDate(1, 0, 0)
	if from != "" {
		parsed, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return nil, invalid("from không hợp lệ.")
		}
		fromTime = parsed
	}
	if to != "" {
		parsed, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return nil, invalid("to không hợp lệ.")
		}
		toTime = parsed
	}
	rows, err := s.pool.Query(ctx, appointmentQuery+` WHERE a.start_at<$2 AND a.end_at>$1 AND ($3=0 OR a.patient_id=$3) AND ($4=0 OR a.doctor_profile_id=$4) AND ($5=0 OR a.specialty_id=$5) AND ($6=0 OR a.room_id=$6) AND ($7='' OR a.status::text=$7) ORDER BY a.start_at DESC,a.id DESC LIMIT $8 OFFSET $9`, fromTime, toTime, patientID, doctorID, specialtyID, roomID, strings.ToUpper(strings.TrimSpace(status)), fetch, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Appointment, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) authorizeView(ctx context.Context, actor auth.Principal, item Appointment) error {
	decision, err := s.authz.Allowed(ctx, actor, "appointment.view.all")
	if err != nil {
		return err
	}
	if decision.Allowed {
		return nil
	}
	if actor.AccountType == auth.AccountTypePatient {
		var own bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patients WHERE id=$1 AND user_id=$2)`, item.PatientID, actor.UserID).Scan(&own)
		if err != nil {
			return err
		}
		if own {
			return nil
		}
	} else {
		own, err := s.ownsDoctor(ctx, actor.UserID, item.DoctorProfileID)
		if err != nil {
			return err
		}
		if own {
			return nil
		}
	}
	return problem.New(403, "APPOINTMENT_SCOPE_FORBIDDEN", "Không được xem lịch hẹn này.")
}

func (s *Service) ownsDoctor(ctx context.Context, userID, doctorProfileID int64) (bool, error) {
	var own bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM doctor_profiles d
			JOIN staff_profiles sp ON sp.id=d.staff_profile_id
			WHERE d.id=$1 AND sp.user_id=$2
		)`, doctorProfileID, userID).Scan(&own)
	return own, err
}

type Slot struct {
	DoctorScheduleID int64     `json:"doctor_schedule_id"`
	DoctorProfileID  int64     `json:"doctor_profile_id"`
	DoctorCode       string    `json:"doctor_code"`
	DoctorName       string    `json:"doctor_name"`
	SpecialtyID      int64     `json:"specialty_id"`
	RoomID           int64     `json:"room_id"`
	RoomCode         string    `json:"room_code"`
	RoomName         string    `json:"room_name"`
	StartAt          time.Time `json:"start_at"`
	EndAt            time.Time `json:"end_at"`
}

func (s *Service) Slots(ctx context.Context, actor auth.Principal, specialtyID, doctorID, patientID int64, date string) ([]Slot, error) {
	day, err := time.ParseInLocation("2006-01-02", date, s.location)
	if err != nil || specialtyID < 1 {
		return nil, invalid("Ngày hoặc chuyên khoa không hợp lệ.")
	}
	if actor.AccountType == auth.AccountTypePatient {
		if err := s.pool.QueryRow(ctx, `SELECT id FROM patients WHERE user_id=$1 AND is_deleted=false`, actor.UserID).Scan(&patientID); errors.Is(err, pgx.ErrNoRows) {
			return []Slot{}, nil
		} else if err != nil {
			return nil, err
		}
	}
	var minLead, maxAdvance int
	if err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT integer_value FROM system_settings WHERE setting_key='appointment_booking_min_lead_minutes'),
			(SELECT integer_value FROM system_settings WHERE setting_key='appointment_booking_max_advance_days')`).Scan(&minLead, &maxAdvance); err != nil {
		return nil, err
	}
	earliest := s.now().Add(time.Duration(minLead) * time.Minute)
	latest := s.now().AddDate(0, 0, maxAdvance)
	if !day.Before(latest) || !day.AddDate(0, 0, 1).After(earliest) {
		return []Slot{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ds.id,d.id,d.doctor_code,sp.full_name,$1,r.id,r.code,r.name,slot_start,slot_start+ds.slot_duration_minutes*interval '1 minute'
		FROM doctor_schedules ds
		JOIN doctor_profiles d ON d.id=ds.doctor_profile_id AND d.status='ACTIVE' AND d.is_deleted=false
		JOIN staff_profiles sp ON sp.id=d.staff_profile_id AND sp.employment_status='ACTIVE' AND sp.is_deleted=false
		JOIN users u ON u.id=sp.user_id AND u.status<>'INACTIVE' AND u.is_deleted=false
		JOIN doctor_specialties dspec ON dspec.doctor_profile_id=d.id AND dspec.specialty_id=$1 AND dspec.is_deleted=false
		JOIN specialties spec ON spec.id=dspec.specialty_id AND spec.status='ACTIVE' AND spec.is_deleted=false
		JOIN rooms r ON r.id=ds.room_id AND r.status='ACTIVE' AND r.is_deleted=false
		CROSS JOIN LATERAL generate_series(ds.start_at,ds.end_at-ds.slot_duration_minutes*interval '1 minute',ds.slot_duration_minutes*interval '1 minute') slot_start
		WHERE ds.status='ACTIVE' AND ds.is_deleted=false AND ds.start_at<$3 AND ds.end_at>$2 AND ($4=0 OR d.id=$4)
		  AND slot_start >= $6 AND slot_start <= $7
		  AND NOT EXISTS(SELECT 1 FROM appointments a WHERE a.status IN (`+holdingStatuses+`) AND (a.doctor_profile_id=d.id OR a.room_id=r.id OR ($5>0 AND a.patient_id=$5)) AND a.appointment_range && tstzrange(slot_start,slot_start+ds.slot_duration_minutes*interval '1 minute','[)'))
		ORDER BY slot_start,d.id,ds.id`, specialtyID, day, day.AddDate(0, 0, 1), doctorID, patientID, earliest, latest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Slot, 0)
	for rows.Next() {
		var item Slot
		if err := rows.Scan(&item.DoctorScheduleID, &item.DoctorProfileID, &item.DoctorCode, &item.DoctorName, &item.SpecialtyID, &item.RoomID, &item.RoomCode, &item.RoomName, &item.StartAt, &item.EndAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const appointmentQuery = `SELECT a.id,a.appointment_code,a.doctor_schedule_id,a.patient_id,p.patient_code,p.full_name,a.doctor_profile_id,a.doctor_code_at_booking,a.doctor_name_at_booking,a.specialty_id,a.specialty_code_at_booking,a.specialty_name_at_booking,a.room_id,a.room_code_at_booking,a.room_name_at_booking,a.start_at,a.end_at,a.status::text,a.source::text,a.approval_mode_at_booking::text,a.check_in_early_minutes_at_booking,a.check_in_late_minutes_at_booking,a.no_show_grace_minutes_at_booking,a.booked_by_user_id,a.version,a.created_at,a.updated_at FROM appointments a JOIN patients p ON p.id=a.patient_id`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Appointment, error) {
	var a Appointment
	err := row.Scan(&a.ID, &a.AppointmentCode, &a.DoctorScheduleID, &a.PatientID, &a.PatientCode, &a.PatientName, &a.DoctorProfileID, &a.DoctorCode, &a.DoctorName, &a.SpecialtyID, &a.SpecialtyCode, &a.SpecialtyName, &a.RoomID, &a.RoomCode, &a.RoomName, &a.StartAt, &a.EndAt, &a.Status, &a.Source, &a.ApprovalMode, &a.CheckInEarlyMinutes, &a.CheckInLateMinutes, &a.NoShowGraceMinutes, &a.BookedByUserID, &a.Version, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Appointment{}, notFound()
	}
	return a, err
}
func (s *Service) getRaw(ctx context.Context, id int64) (Appointment, error) {
	return scan(s.pool.QueryRow(ctx, appointmentQuery+` WHERE a.id=$1 AND a.is_deleted=false`, id))
}
func invalid(message string) error { return problem.New(422, "APPOINTMENT_VALIDATION_FAILED", message) }
func notFound() error {
	return problem.New(404, "APPOINTMENT_NOT_FOUND", "Không tìm thấy lịch hẹn.")
}
func concurrent() error {
	return problem.New(409, "CONCURRENT_UPDATE", "Dữ liệu đã thay đổi; hãy tải lại.")
}
func mapConstraint(err error) error {
	switch pgutil.Constraint(err) {
	case "ex_appointments_doctor_overlap":
		return problem.New(409, "APPOINTMENT_DOCTOR_TIME_CONFLICT", "Bác sĩ đã có lịch hẹn trùng thời gian.")
	case "ex_appointments_patient_overlap":
		return problem.New(409, "APPOINTMENT_PATIENT_TIME_CONFLICT", "Bệnh nhân đã có lịch hẹn trùng thời gian.")
	case "ex_appointments_room_overlap":
		return problem.New(409, "APPOINTMENT_ROOM_TIME_CONFLICT", "Phòng đã có lịch hẹn trùng thời gian.")
	case "uq_appointments_booking_idempotency":
		return problem.New(409, "IDEMPOTENCY_KEY_REUSED", "Idempotency-Key đã được sử dụng.")
	case "uq_appointments_follow_up_holding":
		return problem.New(409, "APPOINTMENT_FOLLOW_UP_ACTIVE_EXISTS", "Encounter đã có một lịch tái khám đang giữ chỗ.")
	default:
		return fmt.Errorf("appointment persistence: %w", err)
	}
}
