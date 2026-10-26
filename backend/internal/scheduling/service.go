package scheduling

import (
	"context"
	"dncs/backend/internal/platform/pagination"
	"errors"
	"fmt"
	"strings"
	"time"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"dncs/backend/internal/platform/textnorm"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

type Schedule struct {
	ID                  int64     `json:"id"`
	DoctorProfileID     int64     `json:"doctor_profile_id"`
	DoctorCode          string    `json:"doctor_code"`
	DoctorName          string    `json:"doctor_name"`
	RoomID              int64     `json:"room_id"`
	RoomCode            string    `json:"room_code"`
	RoomName            string    `json:"room_name"`
	StartAt             time.Time `json:"start_at"`
	EndAt               time.Time `json:"end_at"`
	SlotDurationMinutes int16     `json:"slot_duration_minutes"`
	Status              string    `json:"status"`
	Notes               *string   `json:"notes,omitempty"`
	CancellationReason  *string   `json:"cancellation_reason,omitempty"`
	Version             int64     `json:"version"`
	IsDeleted           bool      `json:"is_deleted"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}
type CreateInput struct {
	DoctorProfileID, RoomID int64
	StartAt, EndAt          string
	SlotDurationMinutes     int16
	InitialStatus, Notes    string
}

func (s *Service) Create(ctx context.Context, actor auth.Principal, input CreateInput) (Schedule, error) {
	start, end, notes, status, err := s.validate(input.StartAt, input.EndAt, input.SlotDurationMinutes, input.InitialStatus, input.Notes)
	if err != nil {
		return Schedule{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Schedule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.lockEligible(ctx, tx, input.DoctorProfileID, input.RoomID); err != nil {
		return Schedule{}, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO doctor_schedules(doctor_profile_id,room_id,start_at,end_at,slot_duration_minutes,status,notes,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`, input.DoctorProfileID, input.RoomID, start, end, input.SlotDurationMinutes, status, notes, actor.UserID).Scan(&id)
	if err != nil {
		return Schedule{}, mapConstraint(err)
	}
	event := audit.UserEvent(actor.UserID, "SCHEDULING", "DOCTOR_SCHEDULE_CREATED", "DOCTOR_SCHEDULE", id)
	event.Metadata = map[string]any{"initial_status": status}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Schedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, mapConstraint(err)
	}
	return s.Get(ctx, actor, id)
}

type UpdateInput struct {
	ExpectedVersion     int64
	RoomID              *int64
	StartAt, EndAt      *string
	SlotDurationMinutes *int16
	Notes               *string
}

func (s *Service) Update(ctx context.Context, actor auth.Principal, id int64, input UpdateInput) (Schedule, error) {
	current, err := s.getRaw(ctx, id, true)
	if err != nil {
		return Schedule{}, err
	}
	roomID := current.RoomID
	startText := current.StartAt.Format(time.RFC3339)
	endText := current.EndAt.Format(time.RFC3339)
	duration := current.SlotDurationMinutes
	notes := value(current.Notes)
	if input.RoomID != nil {
		roomID = *input.RoomID
	}
	if input.StartAt != nil {
		startText = *input.StartAt
	}
	if input.EndAt != nil {
		endText = *input.EndAt
	}
	if input.SlotDurationMinutes != nil {
		duration = *input.SlotDurationMinutes
	}
	if input.Notes != nil {
		notes = *input.Notes
	}
	start, end, noteValue, _, err := s.validate(startText, endText, duration, current.Status, notes)
	if err != nil {
		return Schedule{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Schedule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.lockEligible(ctx, tx, current.DoctorProfileID, roomID); err != nil {
		return Schedule{}, err
	}
	var version int64
	var status string
	var deleted bool
	if err := tx.QueryRow(ctx, `SELECT version,status::text,is_deleted FROM doctor_schedules WHERE id=$1 FOR UPDATE`, id).Scan(&version, &status, &deleted); err != nil {
		return Schedule{}, err
	}
	if version != input.ExpectedVersion {
		return Schedule{}, concurrent()
	}
	if deleted || status == "CANCELLED" {
		return Schedule{}, problem.New(409, "SCHEDULE_NOT_EDITABLE", "Lịch không còn được phép sửa.")
	}
	var holding bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM appointments WHERE doctor_schedule_id=$1 AND status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))`, id).Scan(&holding); err != nil {
		return Schedule{}, err
	}
	if holding {
		return Schedule{}, problem.New(409, "SCHEDULE_HAS_HOLDING_APPOINTMENTS", "Lịch có lịch hẹn đang giữ chỗ.")
	}
	if _, err := tx.Exec(ctx, `UPDATE doctor_schedules SET room_id=$2,start_at=$3,end_at=$4,slot_duration_minutes=$5,notes=$6,updated_by=$7,updated_at=now(),version=version+1 WHERE id=$1`, id, roomID, start, end, duration, noteValue, actor.UserID); err != nil {
		return Schedule{}, mapConstraint(err)
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "SCHEDULING", "DOCTOR_SCHEDULE_UPDATED", "DOCTOR_SCHEDULE", id)); err != nil {
		return Schedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, mapConstraint(err)
	}
	return s.Get(ctx, actor, id)
}
func (s *Service) Activate(ctx context.Context, actor auth.Principal, id, expected int64) (Schedule, error) {
	current, err := s.getRaw(ctx, id, true)
	if err != nil {
		return Schedule{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Schedule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.lockEligible(ctx, tx, current.DoctorProfileID, current.RoomID); err != nil {
		return Schedule{}, err
	}
	var version int64
	var status string
	var deleted bool
	if err := tx.QueryRow(ctx, `SELECT version,status::text,is_deleted FROM doctor_schedules WHERE id=$1 FOR UPDATE`, id).Scan(&version, &status, &deleted); err != nil {
		return Schedule{}, err
	}
	if version != expected {
		return Schedule{}, concurrent()
	}
	if deleted || status != "DRAFT" {
		return Schedule{}, problem.New(409, "SCHEDULE_CANNOT_ACTIVATE", "Chỉ lịch draft còn hiệu lực mới được kích hoạt.")
	}
	if current.StartAt.Before(s.now().Add(30 * time.Minute)) {
		return Schedule{}, invalid("Lịch phải bắt đầu sau hiện tại ít nhất 30 phút.")
	}
	if _, err := tx.Exec(ctx, `UPDATE doctor_schedules SET status='ACTIVE',updated_by=$2,updated_at=now(),version=version+1 WHERE id=$1`, id, actor.UserID); err != nil {
		return Schedule{}, mapConstraint(err)
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "SCHEDULING", "DOCTOR_SCHEDULE_ACTIVATED", "DOCTOR_SCHEDULE", id)); err != nil {
		return Schedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, mapConstraint(err)
	}
	return s.Get(ctx, actor, id)
}
func (s *Service) Cancel(ctx context.Context, actor auth.Principal, id, expected int64, reason string) (Schedule, error) {
	if strings.TrimSpace(reason) == "" {
		return Schedule{}, invalid("Lý do hủy là bắt buộc.")
	}
	current, err := s.getRaw(ctx, id, true)
	if err != nil {
		return Schedule{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Schedule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.lockEligibleForMutation(ctx, tx, current.DoctorProfileID, current.RoomID); err != nil {
		return Schedule{}, err
	}
	var version int64
	var status string
	if err := tx.QueryRow(ctx, `SELECT version,status::text FROM doctor_schedules WHERE id=$1 AND is_deleted=false FOR UPDATE`, id).Scan(&version, &status); errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, notFound()
	} else if err != nil {
		return Schedule{}, err
	}
	if version != expected {
		return Schedule{}, concurrent()
	}
	if status == "CANCELLED" {
		return current, nil
	}
	var holding bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM appointments WHERE doctor_schedule_id=$1 AND status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))`, id).Scan(&holding); err != nil {
		return Schedule{}, err
	}
	if holding {
		return Schedule{}, problem.New(409, "SCHEDULE_HAS_HOLDING_APPOINTMENTS", "Phải xử lý các lịch hẹn giữ chỗ trước khi hủy lịch.")
	}
	if _, err := tx.Exec(ctx, `UPDATE doctor_schedules SET status='CANCELLED',cancellation_reason=$2,cancelled_by=$3,cancelled_at=now(),updated_by=$3,updated_at=now(),version=version+1 WHERE id=$1`, id, strings.TrimSpace(reason), actor.UserID); err != nil {
		return Schedule{}, err
	}
	event := audit.UserEvent(actor.UserID, "SCHEDULING", "DOCTOR_SCHEDULE_CANCELLED", "DOCTOR_SCHEDULE", id)
	event.Metadata = map[string]any{"reason": strings.TrimSpace(reason)}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Schedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, err
	}
	return s.Get(ctx, actor, id)
}
func (s *Service) Archive(ctx context.Context, actor auth.Principal, id, expected int64) error {
	current, err := s.getRaw(ctx, id, true)
	if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.lockEligibleForMutation(ctx, tx, current.DoctorProfileID, current.RoomID); err != nil {
		return err
	}
	var version int64
	var status string
	if err := tx.QueryRow(ctx, `SELECT version,status::text FROM doctor_schedules WHERE id=$1 AND is_deleted=false FOR UPDATE`, id).Scan(&version, &status); errors.Is(err, pgx.ErrNoRows) {
		return notFound()
	} else if err != nil {
		return err
	}
	if version != expected {
		return concurrent()
	}
	if status == "ACTIVE" {
		return problem.New(409, "ACTIVE_SCHEDULE_CANNOT_ARCHIVE", "Phải hủy lịch active trước khi archive.")
	}
	var used bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM appointments WHERE doctor_schedule_id=$1)`, id).Scan(&used); err != nil {
		return err
	}
	if used {
		return problem.New(409, "SCHEDULE_HAS_APPOINTMENTS", "Lịch đã phát sinh appointment nên không thể archive.")
	}
	if _, err := tx.Exec(ctx, `UPDATE doctor_schedules SET is_deleted=true,deleted_by=$2,deleted_at=now(),updated_by=$2,updated_at=now(),version=version+1 WHERE id=$1`, id, actor.UserID); err != nil {
		return err
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "SCHEDULING", "DOCTOR_SCHEDULE_SOFT_DELETED", "DOCTOR_SCHEDULE", id)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, id int64) (Schedule, error) {
	item, err := s.getRaw(ctx, id, false)
	if err != nil {
		return Schedule{}, err
	}
	allowed, err := s.authz.Allowed(ctx, principal, "doctor_schedule.view.all")
	if err != nil {
		return Schedule{}, err
	}
	if !allowed.Allowed {
		own, err := s.ownsDoctor(ctx, principal.UserID, item.DoctorProfileID)
		if err != nil {
			return Schedule{}, err
		}
		if !own {
			return Schedule{}, problem.New(403, "SCHEDULE_SCOPE_FORBIDDEN", "Không được xem lịch này.")
		}
	}
	return item, nil
}
func (s *Service) List(ctx context.Context, principal auth.Principal, from, to, status string, doctorID, roomID int64, page, size int) ([]Schedule, error) {
	return s.list(ctx, principal, from, to, status, doctorID, roomID, page, size, false)
}

func (s *Service) ListPage(ctx context.Context, principal auth.Principal, from, to, status string, doctorID, roomID int64, page, size int) (pagination.Page[Schedule], error) {
	page, size = pagination.Normalize(page, size)
	items, err := s.list(ctx, principal, from, to, status, doctorID, roomID, page, size, true)
	return pagination.From(items, page, size), err
}

func (s *Service) list(ctx context.Context, principal auth.Principal, from, to, status string, doctorID, roomID int64, page, size int, extra bool) ([]Schedule, error) {
	fetch := size
	if extra {
		fetch++
	}
	allowed, err := s.authz.Allowed(ctx, principal, "doctor_schedule.view.all")
	if err != nil {
		return nil, err
	}
	if !allowed.Allowed {
		if err := s.pool.QueryRow(ctx, `SELECT d.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id WHERE sp.user_id=$1 AND d.is_deleted=false`, principal.UserID).Scan(&doctorID); errors.Is(err, pgx.ErrNoRows) {
			return []Schedule{}, nil
		} else if err != nil {
			return nil, err
		}
	}
	fromTime := s.now().AddDate(0, -1, 0)
	toTime := s.now().AddDate(0, 6, 0)
	if from != "" {
		if parsed, err := time.Parse(time.RFC3339, from); err == nil {
			fromTime = parsed
		}
	}
	if to != "" {
		if parsed, err := time.Parse(time.RFC3339, to); err == nil {
			toTime = parsed
		}
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	rows, err := s.pool.Query(ctx, scheduleQuery+` WHERE ds.is_deleted=false AND ds.start_at<$2 AND ds.end_at>$1 AND ($3=0 OR ds.doctor_profile_id=$3) AND ($4=0 OR ds.room_id=$4) AND ($5='' OR ds.status::text=$5) ORDER BY ds.start_at,ds.id LIMIT $6 OFFSET $7`, fromTime, toTime, doctorID, roomID, status, fetch, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Schedule, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) validate(startText, endText string, duration int16, status, notes string) (time.Time, time.Time, *string, string, error) {
	start, err := time.Parse(time.RFC3339, startText)
	if err != nil {
		return time.Time{}, time.Time{}, nil, "", invalid("start_at không hợp lệ.")
	}
	end, err := time.Parse(time.RFC3339, endText)
	if err != nil || !end.After(start) {
		return time.Time{}, time.Time{}, nil, "", invalid("end_at không hợp lệ.")
	}
	startLocal, endLocal := start.In(s.location), end.In(s.location)
	if startLocal.Format("2006-01-02") != endLocal.Format("2006-01-02") {
		return time.Time{}, time.Time{}, nil, "", invalid("Lịch phải nằm trong cùng một ngày địa phương.")
	}
	if start.Before(s.now().Add(30*time.Minute)) || start.After(s.now().AddDate(0, 0, 180)) {
		return time.Time{}, time.Time{}, nil, "", invalid("Lịch phải nằm trong khoảng từ 30 phút đến 180 ngày tới.")
	}
	validDuration := duration == 15 || duration == 20 || duration == 30 || duration == 45 || duration == 60
	if !validDuration || int(end.Sub(start).Minutes())%int(duration) != 0 {
		return time.Time{}, time.Time{}, nil, "", invalid("Thời lượng slot không hợp lệ hoặc không chia hết ca.")
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		status = "DRAFT"
	}
	if status != "DRAFT" && status != "ACTIVE" {
		return time.Time{}, time.Time{}, nil, "", invalid("initial_status không hợp lệ.")
	}
	note, err := textnorm.Optional(notes, 1000)
	if err != nil {
		return time.Time{}, time.Time{}, nil, "", invalid("Ghi chú quá dài.")
	}
	return start, end, note, status, nil
}
func (s *Service) lockEligible(ctx context.Context, tx pgx.Tx, doctorID, roomID int64) error {
	if err := s.lockRoom(ctx, tx, roomID, true); err != nil {
		return err
	}
	var userID, staffID int64
	if err := s.pool.QueryRow(ctx, `SELECT sp.user_id,sp.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id WHERE d.id=$1`, doctorID).Scan(&userID, &staffID); errors.Is(err, pgx.ErrNoRows) {
		return problem.New(404, "DOCTOR_NOT_FOUND", "Không tìm thấy bác sĩ.")
	} else if err != nil {
		return err
	}
	var userActive bool
	if err := tx.QueryRow(ctx, `SELECT status<>'INACTIVE' AND NOT is_deleted FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&userActive); err != nil {
		return err
	}
	var staffActive bool
	if err := tx.QueryRow(ctx, `SELECT employment_status='ACTIVE' AND NOT is_deleted FROM staff_profiles WHERE id=$1 FOR UPDATE`, staffID).Scan(&staffActive); err != nil {
		return err
	}
	var doctorActive bool
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM doctor_profiles WHERE id=$1 FOR UPDATE`, doctorID).Scan(&doctorActive); err != nil {
		return err
	}
	if !userActive || !staffActive || !doctorActive {
		return problem.New(409, "DOCTOR_INACTIVE", "Bác sĩ không đủ điều kiện nhận lịch.")
	}
	return nil
}
func (s *Service) lockEligibleForMutation(ctx context.Context, tx pgx.Tx, doctorID, roomID int64) error {
	if err := s.lockRoom(ctx, tx, roomID, false); err != nil {
		return err
	}
	var userID, staffID int64
	if err := s.pool.QueryRow(ctx, `SELECT sp.user_id,sp.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id WHERE d.id=$1`, doctorID).Scan(&userID, &staffID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM staff_profiles WHERE id=$1 FOR UPDATE`, staffID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM doctor_profiles WHERE id=$1 FOR UPDATE`, doctorID); err != nil {
		return err
	}
	return nil
}
func (s *Service) lockRoom(ctx context.Context, tx pgx.Tx, roomID int64, requireActive bool) error {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM rooms WHERE id=$1 FOR UPDATE`, roomID).Scan(&active); errors.Is(err, pgx.ErrNoRows) {
		return problem.New(404, "ROOM_NOT_FOUND", "Không tìm thấy phòng.")
	} else if err != nil {
		return err
	}
	if requireActive && !active {
		return problem.New(409, "ROOM_INACTIVE", "Phòng không hoạt động.")
	}
	return nil
}
func (s *Service) ownsDoctor(ctx context.Context, userID, doctorID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id WHERE d.id=$1 AND sp.user_id=$2)`, doctorID, userID).Scan(&ok)
	return ok, err
}

const scheduleQuery = `SELECT ds.id,ds.doctor_profile_id,d.doctor_code,sp.full_name,ds.room_id,r.code,r.name,ds.start_at,ds.end_at,ds.slot_duration_minutes,ds.status::text,ds.notes,ds.cancellation_reason,ds.version,ds.is_deleted,ds.created_at,ds.updated_at FROM doctor_schedules ds JOIN doctor_profiles d ON d.id=ds.doctor_profile_id JOIN staff_profiles sp ON sp.id=d.staff_profile_id JOIN rooms r ON r.id=ds.room_id`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Schedule, error) {
	var item Schedule
	err := row.Scan(&item.ID, &item.DoctorProfileID, &item.DoctorCode, &item.DoctorName, &item.RoomID, &item.RoomCode, &item.RoomName, &item.StartAt, &item.EndAt, &item.SlotDurationMinutes, &item.Status, &item.Notes, &item.CancellationReason, &item.Version, &item.IsDeleted, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, notFound()
	}
	return item, err
}
func (s *Service) getRaw(ctx context.Context, id int64, includeDeleted bool) (Schedule, error) {
	return scan(s.pool.QueryRow(ctx, scheduleQuery+` WHERE ds.id=$1 AND ($2 OR ds.is_deleted=false)`, id, includeDeleted))
}
func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func invalid(message string) error { return problem.New(422, "SCHEDULE_VALIDATION_FAILED", message) }
func notFound() error {
	return problem.New(404, "SCHEDULE_NOT_FOUND", "Không tìm thấy lịch bác sĩ.")
}
func concurrent() error {
	return problem.New(409, "CONCURRENT_UPDATE", "Dữ liệu đã thay đổi; hãy tải lại.")
}
func mapConstraint(err error) error {
	switch pgutil.Constraint(err) {
	case "ex_doctor_schedules_doctor_overlap":
		return problem.New(409, "DOCTOR_SCHEDULE_DOCTOR_OVERLAP", "Bác sĩ đã có lịch trùng thời gian.")
	case "ex_doctor_schedules_room_overlap":
		return problem.New(409, "DOCTOR_SCHEDULE_ROOM_OVERLAP", "Phòng đã có lịch trùng thời gian.")
	default:
		return fmt.Errorf("schedule persistence: %w", err)
	}
}
