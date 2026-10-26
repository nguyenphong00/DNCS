package clinic

import (
	"context"
	"dncs/backend/internal/platform/pagination"
	"errors"
	"regexp"
	"strings"
	"time"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"dncs/backend/internal/platform/textnorm"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var specialtyCodePattern = regexp.MustCompile(`^SPC_[A-Z0-9_]{2,30}$`)
var roomCodePattern = regexp.MustCompile(`^RM-[A-Z0-9-]{2,20}$`)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type Specialty struct {
	ID              int64     `json:"id"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	Description     *string   `json:"description,omitempty"`
	Status          string    `json:"status"`
	Version         int64     `json:"version"`
	RelationVersion *int64    `json:"relation_version,omitempty"`
	IsDeleted       bool      `json:"is_deleted"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type Room struct {
	ID           int64     `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	RoomType     string    `json:"room_type"`
	Floor        *string   `json:"floor,omitempty"`
	LocationNote *string   `json:"location_note,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Status       string    `json:"status"`
	Version      int64     `json:"version"`
	IsDeleted    bool      `json:"is_deleted"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
type Doctor struct {
	ID         int64  `json:"id"`
	DoctorCode string `json:"doctor_code"`
	FullName   string `json:"full_name"`
}

func (s *Service) CreateSpecialty(ctx context.Context, actor auth.Principal, code, name, description string) (Specialty, error) {
	code = textnorm.UpperCode(code)
	cleanName, err := textnorm.HumanName(name, 200)
	if err != nil || !specialtyCodePattern.MatchString(code) {
		return Specialty{}, invalid("Mã hoặc tên chuyên khoa không hợp lệ.")
	}
	desc, err := textnorm.Optional(description, 1000)
	if err != nil {
		return Specialty{}, invalid("Mô tả quá dài.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Specialty{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO specialties(code,name,normalized_name,description,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$5) RETURNING id`, code, cleanName, textnorm.Search(cleanName), desc, actor.UserID).Scan(&id)
	if err != nil {
		return Specialty{}, mapConstraint(err)
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "CLINIC", "SPECIALTY_CREATED", "SPECIALTY", id)); err != nil {
		return Specialty{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Specialty{}, err
	}
	return s.GetSpecialty(ctx, id, false)
}
func (s *Service) UpdateSpecialty(ctx context.Context, actor auth.Principal, id, version int64, name, description *string) (Specialty, error) {
	current, err := s.GetSpecialty(ctx, id, false)
	if err != nil {
		return Specialty{}, err
	}
	newName := current.Name
	newDesc := value(current.Description)
	if name != nil {
		newName = *name
	}
	if description != nil {
		newDesc = *description
	}
	cleanName, err := textnorm.HumanName(newName, 200)
	if err != nil {
		return Specialty{}, invalid("Tên chuyên khoa không hợp lệ.")
	}
	desc, err := textnorm.Optional(newDesc, 1000)
	if err != nil {
		return Specialty{}, invalid("Mô tả quá dài.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Specialty{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cmd, err := tx.Exec(ctx, `UPDATE specialties SET name=$3,normalized_name=$4,description=$5,updated_by=$6,updated_at=now(),version=version+1 WHERE id=$1 AND version=$2 AND is_deleted=false`, id, version, cleanName, textnorm.Search(cleanName), desc, actor.UserID)
	if err != nil {
		return Specialty{}, mapConstraint(err)
	}
	if cmd.RowsAffected() != 1 {
		return Specialty{}, concurrent()
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "CLINIC", "SPECIALTY_UPDATED", "SPECIALTY", id)); err != nil {
		return Specialty{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Specialty{}, err
	}
	return s.GetSpecialty(ctx, id, false)
}
func (s *Service) ChangeSpecialty(ctx context.Context, actor auth.Principal, id, version int64, action, reason string) (Specialty, error) {
	if action != "activate" && strings.TrimSpace(reason) == "" {
		return Specialty{}, invalid("Lý do là bắt buộc.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Specialty{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current string
	var deleted bool
	var actual int64
	err = tx.QueryRow(ctx, `SELECT status::text,is_deleted,version FROM specialties WHERE id=$1 FOR UPDATE`, id).Scan(&current, &deleted, &actual)
	if errors.Is(err, pgx.ErrNoRows) {
		return Specialty{}, specialtyNotFound()
	}
	if err != nil {
		return Specialty{}, err
	}
	if actual != version {
		return Specialty{}, concurrent()
	}
	if deleted {
		return Specialty{}, problem.New(409, "SPECIALTY_DELETED", "Chuyên khoa đã được lưu trữ.")
	}
	if action != "activate" {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM appointments WHERE specialty_id=$1 AND status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))`, id).Scan(&used); err != nil {
			return Specialty{}, err
		}
		if used {
			return Specialty{}, problem.New(409, "SPECIALTY_IN_ACTIVE_USE", "Chuyên khoa đang được lịch hẹn hoạt động sử dụng.")
		}
	}
	if action == "archive" {
		if current != "INACTIVE" {
			return Specialty{}, problem.New(409, "SPECIALTY_MUST_BE_INACTIVE", "Phải vô hiệu hóa chuyên khoa trước khi soft delete.")
		}
		var assigned bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doctor_specialties WHERE specialty_id=$1 AND is_deleted=false)`, id).Scan(&assigned); err != nil {
			return Specialty{}, err
		}
		if assigned {
			return Specialty{}, problem.New(409, "SPECIALTY_HAS_ACTIVE_DOCTORS", "Phải bỏ các quan hệ bác sĩ–chuyên khoa trước khi soft delete.")
		}
	}
	status := "INACTIVE"
	deletedValue := false
	event := "SPECIALTY_DEACTIVATED"
	if action == "activate" {
		status = "ACTIVE"
		event = "SPECIALTY_ACTIVATED"
	}
	if action == "archive" {
		deletedValue = true
		event = "SPECIALTY_SOFT_DELETED"
	}
	if _, err := tx.Exec(ctx, `UPDATE specialties SET status=$2,is_deleted=$3,updated_by=$4,updated_at=now(),version=version+1 WHERE id=$1`, id, status, deletedValue, actor.UserID); err != nil {
		return Specialty{}, err
	}
	e := audit.UserEvent(actor.UserID, "CLINIC", event, "SPECIALTY", id)
	e.Metadata = map[string]any{"reason": strings.TrimSpace(reason)}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return Specialty{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Specialty{}, err
	}
	return s.GetSpecialty(ctx, id, true)
}
func (s *Service) GetSpecialty(ctx context.Context, id int64, includeDeleted bool) (Specialty, error) {
	var item Specialty
	err := s.pool.QueryRow(ctx, `SELECT id,code,name,description,status::text,version,is_deleted,created_at,updated_at FROM specialties WHERE id=$1 AND ($2 OR is_deleted=false)`, id, includeDeleted).Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.Status, &item.Version, &item.IsDeleted, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Specialty{}, specialtyNotFound()
	}
	return item, err
}
func (s *Service) ListSpecialties(ctx context.Context, query string, onlyActive bool, page, size int) ([]Specialty, error) {
	return s.listSpecialties(ctx, query, onlyActive, page, size, false)
}

func (s *Service) ListSpecialtiesPage(ctx context.Context, query string, onlyActive bool, page, size int) (pagination.Page[Specialty], error) {
	page, size = pagination.Normalize(page, size)
	items, err := s.listSpecialties(ctx, query, onlyActive, page, size, true)
	return pagination.From(items, page, size), err
}

func (s *Service) listSpecialties(ctx context.Context, query string, onlyActive bool, page, size int, extra bool) ([]Specialty, error) {
	fetch := size
	if extra {
		fetch++
	}
	rows, err := s.pool.Query(ctx, `SELECT id,code,name,description,status::text,version,is_deleted,created_at,updated_at FROM specialties WHERE is_deleted=false AND (NOT $1 OR status='ACTIVE') AND ($2='' OR code=$2 OR normalized_name LIKE '%'||$3||'%') ORDER BY normalized_name,id LIMIT $4 OFFSET $5`, onlyActive, textnorm.UpperCode(query), textnorm.Search(query), fetch, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Specialty, 0)
	for rows.Next() {
		var item Specialty
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.Status, &item.Version, &item.IsDeleted, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateRoom(ctx context.Context, actor auth.Principal, code, name, roomType, floor, location, description string) (Room, error) {
	code = textnorm.UpperCode(code)
	cleanName, err := textnorm.HumanName(name, 200)
	roomType = strings.ToUpper(strings.TrimSpace(roomType))
	if err != nil || !roomCodePattern.MatchString(code) || (roomType != "CONSULTATION" && roomType != "FUNCTIONAL" && roomType != "OTHER") {
		return Room{}, invalid("Thông tin phòng không hợp lệ.")
	}
	f, err := textnorm.Optional(floor, 100)
	if err != nil {
		return Room{}, invalid("Tầng quá dài.")
	}
	l, err := textnorm.Optional(location, 500)
	if err != nil {
		return Room{}, invalid("Vị trí quá dài.")
	}
	d, err := textnorm.Optional(description, 1000)
	if err != nil {
		return Room{}, invalid("Mô tả quá dài.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Room{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO rooms(code,name,normalized_name,room_type,floor,location_note,description,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`, code, cleanName, textnorm.Search(cleanName), roomType, f, l, d, actor.UserID).Scan(&id)
	if err != nil {
		return Room{}, mapConstraint(err)
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "CLINIC", "ROOM_CREATED", "ROOM", id)); err != nil {
		return Room{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Room{}, err
	}
	return s.GetRoom(ctx, id, false)
}

type RoomUpdate struct {
	ExpectedVersion                                  int64
	Name, RoomType, Floor, LocationNote, Description *string
}

func (s *Service) UpdateRoom(ctx context.Context, actor auth.Principal, id int64, input RoomUpdate) (Room, error) {
	current, err := s.GetRoom(ctx, id, false)
	if err != nil {
		return Room{}, err
	}
	name := current.Name
	roomType := current.RoomType
	floor := value(current.Floor)
	location := value(current.LocationNote)
	description := value(current.Description)
	if input.Name != nil {
		name = *input.Name
	}
	if input.RoomType != nil {
		roomType = *input.RoomType
	}
	if input.Floor != nil {
		floor = *input.Floor
	}
	if input.LocationNote != nil {
		location = *input.LocationNote
	}
	if input.Description != nil {
		description = *input.Description
	}
	cleanName, err := textnorm.HumanName(name, 200)
	roomType = strings.ToUpper(strings.TrimSpace(roomType))
	if err != nil || (roomType != "CONSULTATION" && roomType != "FUNCTIONAL" && roomType != "OTHER") {
		return Room{}, invalid("Thông tin phòng không hợp lệ.")
	}
	f, err := textnorm.Optional(floor, 100)
	if err != nil {
		return Room{}, invalid("Tầng quá dài.")
	}
	l, err := textnorm.Optional(location, 500)
	if err != nil {
		return Room{}, invalid("Vị trí quá dài.")
	}
	d, err := textnorm.Optional(description, 1000)
	if err != nil {
		return Room{}, invalid("Mô tả quá dài.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Room{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedRoomType string
	var lockedVersion int64
	if err := tx.QueryRow(ctx, `SELECT room_type::text,version FROM rooms WHERE id=$1 AND is_deleted=false FOR UPDATE`, id).Scan(&lockedRoomType, &lockedVersion); errors.Is(err, pgx.ErrNoRows) {
		return Room{}, roomNotFound()
	} else if err != nil {
		return Room{}, err
	}
	if lockedVersion != input.ExpectedVersion {
		return Room{}, concurrent()
	}
	if roomType != lockedRoomType {
		var referenced bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doctor_schedules WHERE room_id=$1 UNION ALL SELECT 1 FROM appointments WHERE room_id=$1)`, id).Scan(&referenced); err != nil {
			return Room{}, err
		}
		if referenced {
			return Room{}, problem.New(409, "ROOM_TYPE_IMMUTABLE_AFTER_USE", "Không thể đổi loại phòng sau khi phòng đã được sử dụng.")
		}
	}
	cmd, err := tx.Exec(ctx, `UPDATE rooms SET name=$3,normalized_name=$4,room_type=$5,floor=$6,location_note=$7,description=$8,updated_by=$9,updated_at=now(),version=version+1 WHERE id=$1 AND version=$2 AND is_deleted=false`, id, input.ExpectedVersion, cleanName, textnorm.Search(cleanName), roomType, f, l, d, actor.UserID)
	if err != nil {
		return Room{}, mapConstraint(err)
	}
	if cmd.RowsAffected() != 1 {
		return Room{}, concurrent()
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "CLINIC", "ROOM_UPDATED", "ROOM", id)); err != nil {
		return Room{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Room{}, err
	}
	return s.GetRoom(ctx, id, false)
}
func (s *Service) ChangeRoom(ctx context.Context, actor auth.Principal, id, version int64, action, reason string) (Room, error) {
	if action != "activate" && strings.TrimSpace(reason) == "" {
		return Room{}, invalid("Lý do là bắt buộc.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Room{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var deleted bool
	var currentStatus string
	var actual int64
	if err := tx.QueryRow(ctx, `SELECT status::text,is_deleted,version FROM rooms WHERE id=$1 FOR UPDATE`, id).Scan(&currentStatus, &deleted, &actual); errors.Is(err, pgx.ErrNoRows) {
		return Room{}, roomNotFound()
	} else if err != nil {
		return Room{}, err
	}
	if actual != version {
		return Room{}, concurrent()
	}
	if deleted {
		return Room{}, problem.New(409, "ROOM_DELETED", "Phòng đã được lưu trữ.")
	}
	if action != "activate" {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doctor_schedules WHERE room_id=$1 AND status<>'CANCELLED' AND is_deleted=false AND end_at>now() UNION ALL SELECT 1 FROM appointments WHERE room_id=$1 AND status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))`, id).Scan(&used); err != nil {
			return Room{}, err
		}
		if used {
			return Room{}, problem.New(409, "ROOM_IN_ACTIVE_USE", "Phòng đang được lịch hoạt động sử dụng.")
		}
	}
	if action == "archive" && currentStatus != "INACTIVE" {
		return Room{}, problem.New(409, "ROOM_MUST_BE_INACTIVE", "Phải vô hiệu hóa phòng trước khi soft delete.")
	}
	status := "INACTIVE"
	isDeleted := false
	event := "ROOM_DEACTIVATED"
	if action == "activate" {
		status = "ACTIVE"
		event = "ROOM_ACTIVATED"
	}
	if action == "archive" {
		isDeleted = true
		event = "ROOM_SOFT_DELETED"
	}
	if _, err := tx.Exec(ctx, `UPDATE rooms SET status=$2,is_deleted=$3,updated_by=$4,updated_at=now(),version=version+1 WHERE id=$1`, id, status, isDeleted, actor.UserID); err != nil {
		return Room{}, err
	}
	e := audit.UserEvent(actor.UserID, "CLINIC", event, "ROOM", id)
	e.Metadata = map[string]any{"reason": strings.TrimSpace(reason)}
	if err := audit.Insert(ctx, tx, e); err != nil {
		return Room{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Room{}, err
	}
	return s.GetRoom(ctx, id, true)
}
func (s *Service) GetRoom(ctx context.Context, id int64, includeDeleted bool) (Room, error) {
	var item Room
	err := s.pool.QueryRow(ctx, `SELECT id,code,name,room_type::text,floor,location_note,description,status::text,version,is_deleted,created_at,updated_at FROM rooms WHERE id=$1 AND ($2 OR is_deleted=false)`, id, includeDeleted).Scan(&item.ID, &item.Code, &item.Name, &item.RoomType, &item.Floor, &item.LocationNote, &item.Description, &item.Status, &item.Version, &item.IsDeleted, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Room{}, roomNotFound()
	}
	return item, err
}
func (s *Service) ListRooms(ctx context.Context, query, roomType string, onlyActive bool, page, size int) ([]Room, error) {
	return s.listRooms(ctx, query, roomType, onlyActive, page, size, false)
}

func (s *Service) ListRoomsPage(ctx context.Context, query, roomType string, onlyActive bool, page, size int) (pagination.Page[Room], error) {
	page, size = pagination.Normalize(page, size)
	items, err := s.listRooms(ctx, query, roomType, onlyActive, page, size, true)
	return pagination.From(items, page, size), err
}

func (s *Service) listRooms(ctx context.Context, query, roomType string, onlyActive bool, page, size int, extra bool) ([]Room, error) {
	fetch := size
	if extra {
		fetch++
	}
	rows, err := s.pool.Query(ctx, `SELECT id,code,name,room_type::text,floor,location_note,description,status::text,version,is_deleted,created_at,updated_at FROM rooms WHERE is_deleted=false AND (NOT $1 OR status='ACTIVE') AND ($2='' OR room_type::text=$2) AND ($3='' OR code=$3 OR normalized_name LIKE '%'||$4||'%') ORDER BY normalized_name,id LIMIT $5 OFFSET $6`, onlyActive, strings.ToUpper(strings.TrimSpace(roomType)), textnorm.UpperCode(query), textnorm.Search(query), fetch, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Room, 0)
	for rows.Next() {
		var item Room
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.RoomType, &item.Floor, &item.LocationNote, &item.Description, &item.Status, &item.Version, &item.IsDeleted, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListDoctors(ctx context.Context, specialtyID int64) ([]Doctor, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.id,d.doctor_code,sp.full_name FROM specialties s JOIN doctor_specialties ds ON ds.specialty_id=s.id AND ds.is_deleted=false JOIN doctor_profiles d ON d.id=ds.doctor_profile_id AND d.status='ACTIVE' AND d.is_deleted=false JOIN staff_profiles sp ON sp.id=d.staff_profile_id AND sp.employment_status='ACTIVE' AND sp.is_deleted=false JOIN users u ON u.id=sp.user_id AND u.status<>'INACTIVE' AND u.is_deleted=false WHERE s.id=$1 AND s.status='ACTIVE' AND s.is_deleted=false ORDER BY sp.full_name,d.id`, specialtyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Doctor, 0)
	for rows.Next() {
		var item Doctor
		if err := rows.Scan(&item.ID, &item.DoctorCode, &item.FullName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Service) DoctorSpecialties(ctx context.Context, doctorID int64) ([]Specialty, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.code,s.name,s.description,s.status::text,s.version,s.is_deleted,s.created_at,s.updated_at,ds.version FROM doctor_specialties ds JOIN specialties s ON s.id=ds.specialty_id WHERE ds.doctor_profile_id=$1 AND ds.is_deleted=false ORDER BY s.normalized_name`, doctorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Specialty, 0)
	for rows.Next() {
		var item Specialty
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.Status, &item.Version, &item.IsDeleted, &item.CreatedAt, &item.UpdatedAt, &item.RelationVersion); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Service) AssignDoctor(ctx context.Context, actor auth.Principal, doctorID, specialtyID int64) error {
	return s.changeAssignment(ctx, actor, doctorID, specialtyID, false, "", 0)
}
func (s *Service) RemoveDoctor(ctx context.Context, actor auth.Principal, doctorID, specialtyID, version int64, reason string) error {
	return s.changeAssignment(ctx, actor, doctorID, specialtyID, true, reason, version)
}
func (s *Service) changeAssignment(ctx context.Context, actor auth.Principal, doctorID, specialtyID int64, remove bool, reason string, version int64) error {
	if remove && strings.TrimSpace(reason) == "" {
		return invalid("Lý do bỏ gán là bắt buộc.")
	}
	var userID, staffID int64
	if err := s.pool.QueryRow(ctx, `SELECT sp.user_id,sp.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id WHERE d.id=$1`, doctorID).Scan(&userID, &staffID); errors.Is(err, pgx.ErrNoRows) {
		return problem.New(404, "DOCTOR_NOT_FOUND", "Không tìm thấy bác sĩ.")
	} else if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var specialtyActive bool
	if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM specialties WHERE id=$1 FOR UPDATE`, specialtyID).Scan(&specialtyActive); errors.Is(err, pgx.ErrNoRows) {
		return specialtyNotFound()
	} else if err != nil {
		return err
	}
	if !specialtyActive && !remove {
		return problem.New(409, "SPECIALTY_INACTIVE", "Chuyên khoa không hoạt động.")
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
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
	if !remove && (!staffActive || !doctorActive) {
		return problem.New(409, "DOCTOR_INACTIVE", "Bác sĩ không hoạt động.")
	}
	if remove {
		var relationID, actual int64
		err := tx.QueryRow(ctx, `SELECT id,version FROM doctor_specialties WHERE doctor_profile_id=$1 AND specialty_id=$2 AND is_deleted=false FOR UPDATE`, doctorID, specialtyID).Scan(&relationID, &actual)
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.New(404, "DOCTOR_SPECIALTY_NOT_FOUND", "Quan hệ chuyên khoa không tồn tại.")
		}
		if err != nil {
			return err
		}
		if actual != version {
			return concurrent()
		}
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM appointments WHERE doctor_profile_id=$1 AND specialty_id=$2 AND status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))`, doctorID, specialtyID).Scan(&used); err != nil {
			return err
		}
		if used {
			return problem.New(409, "DOCTOR_SPECIALTY_IN_ACTIVE_USE", "Quan hệ đang được lịch hẹn hoạt động sử dụng.")
		}
		if _, err := tx.Exec(ctx, `UPDATE doctor_specialties SET is_deleted=true,removed_by=$2,removed_at=now(),removal_reason=$3,updated_at=now(),version=version+1 WHERE id=$1`, relationID, actor.UserID, strings.TrimSpace(reason)); err != nil {
			return err
		}
		if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "CLINIC", "DOCTOR_SPECIALTY_REMOVED", "DOCTOR_SPECIALTY", relationID)); err != nil {
			return err
		}
	} else {
		var relationID int64
		err := tx.QueryRow(ctx, `INSERT INTO doctor_specialties(doctor_profile_id,specialty_id,assigned_by) VALUES($1,$2,$3) RETURNING id`, doctorID, specialtyID, actor.UserID).Scan(&relationID)
		if err != nil {
			return mapConstraint(err)
		}
		if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "CLINIC", "DOCTOR_SPECIALTY_ASSIGNED", "DOCTOR_SPECIALTY", relationID)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func invalid(message string) error { return problem.New(422, "CLINIC_VALIDATION_FAILED", message) }
func concurrent() error {
	return problem.New(409, "CONCURRENT_UPDATE", "Dữ liệu đã thay đổi; hãy tải lại.")
}
func specialtyNotFound() error {
	return problem.New(404, "SPECIALTY_NOT_FOUND", "Không tìm thấy chuyên khoa.")
}
func roomNotFound() error { return problem.New(404, "ROOM_NOT_FOUND", "Không tìm thấy phòng.") }
func mapConstraint(err error) error {
	switch pgutil.Constraint(err) {
	case "uq_specialties_code":
		return problem.New(409, "SPECIALTY_CODE_EXISTS", "Mã chuyên khoa đã tồn tại.")
	case "uq_specialties_normalized_name":
		return problem.New(409, "SPECIALTY_NAME_EXISTS", "Tên chuyên khoa đã tồn tại.")
	case "uq_rooms_code":
		return problem.New(409, "ROOM_CODE_EXISTS", "Mã phòng đã tồn tại.")
	case "uq_rooms_normalized_name":
		return problem.New(409, "ROOM_NAME_EXISTS", "Tên phòng đã tồn tại.")
	case "uq_doctor_specialties_active":
		return problem.New(409, "DOCTOR_SPECIALTY_EXISTS", "Bác sĩ đã được gán chuyên khoa.")
	default:
		return err
	}
}
