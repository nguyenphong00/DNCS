package staff

import (
	"context"
	"dncs/backend/internal/platform/pagination"
	"errors"
	"fmt"
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

var genderPattern = regexp.MustCompile(`^(MALE|FEMALE|OTHER|UNSPECIFIED)$`)

type Service struct {
	pool      *pgxpool.Pool
	passwords auth.PasswordManager
	now       func() time.Time
	location  *time.Location
}

func NewService(pool *pgxpool.Pool, passwords auth.PasswordManager) *Service {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	return &Service{pool: pool, passwords: passwords, now: time.Now, location: location}
}

type DoctorProfile struct {
	ID         int64  `json:"id"`
	DoctorCode string `json:"doctor_code"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
}

type Profile struct {
	ID                 int64          `json:"id"`
	UserID             int64          `json:"user_id"`
	StaffCode          string         `json:"staff_code"`
	Username           string         `json:"username"`
	FullName           string         `json:"full_name"`
	PhoneNumber        *string        `json:"phone_number,omitempty"`
	Email              *string        `json:"email,omitempty"`
	DateOfBirth        *string        `json:"date_of_birth,omitempty"`
	GenderCode         *string        `json:"gender_code,omitempty"`
	Address            *string        `json:"address,omitempty"`
	EmploymentStatus   string         `json:"employment_status"`
	AccountStatus      string         `json:"account_status"`
	AccountVersion     int64          `json:"account_version"`
	RoleID             int64          `json:"role_id"`
	RoleCode           string         `json:"role_code"`
	MustChangePassword bool           `json:"must_change_password"`
	Version            int64          `json:"version"`
	IsDeleted          bool           `json:"is_deleted"`
	DoctorProfile      *DoctorProfile `json:"doctor_profile,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

type CreateInput struct {
	Username            string
	RoleID              int64
	FullName            string
	PhoneNumber         string
	Email               string
	DateOfBirth         string
	GenderCode          string
	Address             string
	CreateDoctorProfile bool
}

type Created struct {
	Profile           Profile `json:"staff"`
	TemporaryPassword string  `json:"temporary_password"`
}

func (service *Service) Create(ctx context.Context, actor auth.Principal, input CreateInput) (Created, error) {
	username, err := auth.NormalizeStaffUsername(input.Username)
	if err != nil || input.RoleID < 1 {
		return Created{}, invalid("Username hoặc role không hợp lệ.")
	}
	fields, err := service.validateProfileFields(input.FullName, input.PhoneNumber, input.Email, input.DateOfBirth, input.GenderCode, input.Address)
	if err != nil {
		return Created{}, err
	}
	temporaryPassword, err := auth.GenerateTemporaryPassword()
	if err != nil {
		return Created{}, err
	}
	passwordHash, err := service.passwords.Hash(temporaryPassword)
	if err != nil {
		return Created{}, err
	}

	tx, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Created{}, fmt.Errorf("begin staff create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var roleCode string
	err = tx.QueryRow(ctx, `
		SELECT code FROM roles
		WHERE id=$1 AND account_scope='STAFF' AND status='ACTIVE' AND is_deleted=false`, input.RoleID).Scan(&roleCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return Created{}, problem.New(422, "AUTHZ_ROLE_INACTIVE", "Role không khả dụng cho tài khoản nhân sự.")
	}
	if err != nil {
		return Created{}, fmt.Errorf("load staff role: %w", err)
	}
	if roleCode == "SUPER_ADMIN" {
		return Created{}, problem.New(403, "STAFF_PROTECTED_OPERATION_FORBIDDEN", "Không thể tạo Super Admin qua luồng quản trị thông thường.")
	}
	var userID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO users(
			role_id,account_type,username,normalized_username,password_hash,status,must_change_password
		) VALUES($1,'STAFF',$2,$3,$4,'ACTIVE',true) RETURNING id`,
		input.RoleID, strings.TrimSpace(input.Username), username, passwordHash,
	).Scan(&userID)
	if err != nil {
		return Created{}, mapCreateError(err)
	}
	var staffID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO staff_profiles(
			user_id,full_name,phone_number,normalized_phone_number,email,normalized_email,
			date_of_birth,gender_code,address,created_by,updated_by
		) VALUES($1,$2,$3,$3,$4,$5,$6,$7,$8,$9,$9) RETURNING id`,
		userID, fields.fullName, fields.phone, fields.email, fields.normalizedEmail,
		fields.dateOfBirth, fields.gender, fields.address, actor.UserID,
	).Scan(&staffID)
	if err != nil {
		return Created{}, mapCreateError(err)
	}
	if input.CreateDoctorProfile {
		if _, err := tx.Exec(ctx, `
			INSERT INTO doctor_profiles(staff_profile_id,created_by) VALUES($1,$2)`,
			staffID, actor.UserID); err != nil {
			return Created{}, mapCreateError(err)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_role_histories(user_id,to_role_id,changed_by_user_id,reason)
		VALUES($1,$2,$3,'INITIAL_STAFF_ROLE')`, userID, input.RoleID, actor.UserID); err != nil {
		return Created{}, fmt.Errorf("insert initial staff role history: %w", err)
	}
	event := audit.UserEvent(actor.UserID, "STAFF", "STAFF_ACCOUNT_CREATED", "STAFF_PROFILE", staffID)
	event.Metadata = map[string]any{"user_id": userID, "role_id": input.RoleID, "doctor_profile_created": input.CreateDoctorProfile}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Created{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Created{}, fmt.Errorf("commit staff create: %w", err)
	}
	profile, err := service.Get(ctx, staffID, true)
	if err != nil {
		return Created{}, err
	}
	return Created{Profile: profile, TemporaryPassword: temporaryPassword}, nil
}

type UpdateInput struct {
	ExpectedVersion int64
	FullName        *string
	PhoneNumber     *string
	Email           *string
	DateOfBirth     *string
	GenderCode      *string
	Address         *string
}

func (service *Service) Update(ctx context.Context, actor auth.Principal, staffID int64, input UpdateInput) (Profile, error) {
	current, err := service.Get(ctx, staffID, false)
	if err != nil {
		return Profile{}, err
	}
	if current.RoleCode == "SUPER_ADMIN" {
		return Profile{}, protected()
	}
	fullName := current.FullName
	phone := value(current.PhoneNumber)
	email := value(current.Email)
	dob := value(current.DateOfBirth)
	gender := value(current.GenderCode)
	address := value(current.Address)
	if input.FullName != nil {
		fullName = *input.FullName
	}
	if input.PhoneNumber != nil {
		phone = *input.PhoneNumber
	}
	if input.Email != nil {
		email = *input.Email
	}
	if input.DateOfBirth != nil {
		dob = *input.DateOfBirth
	}
	if input.GenderCode != nil {
		gender = *input.GenderCode
	}
	if input.Address != nil {
		address = *input.Address
	}
	fields, err := service.validateProfileFields(fullName, phone, email, dob, gender, address)
	if err != nil {
		return Profile{}, err
	}
	tx, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, fmt.Errorf("begin staff update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
		UPDATE staff_profiles SET full_name=$3,phone_number=$4,normalized_phone_number=$4,
			email=$5,normalized_email=$6,date_of_birth=$7,gender_code=$8,address=$9,
			updated_by=$10,updated_at=now(),version=version+1
		WHERE id=$1 AND version=$2 AND is_deleted=false`,
		staffID, input.ExpectedVersion, fields.fullName, fields.phone, fields.email,
		fields.normalizedEmail, fields.dateOfBirth, fields.gender, fields.address, actor.UserID)
	if err != nil {
		return Profile{}, mapCreateError(err)
	}
	if command.RowsAffected() != 1 {
		return Profile{}, concurrent()
	}
	event := audit.UserEvent(actor.UserID, "STAFF", "STAFF_PROFILE_UPDATED", "STAFF_PROFILE", staffID)
	event.Metadata = map[string]any{"expected_version": input.ExpectedVersion}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, fmt.Errorf("commit staff update: %w", err)
	}
	return service.Get(ctx, staffID, false)
}

type StatusInput struct {
	ExpectedVersion          int64
	Reason                   string
	ConfirmExistingOverrides bool
}

func (service *Service) ChangeEmploymentStatus(ctx context.Context, actor auth.Principal, staffID int64, action string, input StatusInput) (Profile, error) {
	if input.ExpectedVersion < 1 {
		return Profile{}, invalid("expected_version không hợp lệ.")
	}
	if action != "activate" && strings.TrimSpace(input.Reason) == "" {
		return Profile{}, invalid("Lý do là bắt buộc.")
	}
	var userID int64
	if err := service.pool.QueryRow(ctx, `SELECT user_id FROM staff_profiles WHERE id=$1`, staffID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, notFound()
	} else if err != nil {
		return Profile{}, fmt.Errorf("resolve staff user: %w", err)
	}
	tx, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, fmt.Errorf("begin staff status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var accountStatus, roleCode, roleStatus string
	var userDeleted bool
	err = tx.QueryRow(ctx, `
		SELECT u.status::text,r.code,r.status::text,u.is_deleted
		FROM users u JOIN roles r ON r.id=u.role_id WHERE u.id=$1 FOR UPDATE OF u`, userID,
	).Scan(&accountStatus, &roleCode, &roleStatus, &userDeleted)
	if err != nil {
		return Profile{}, fmt.Errorf("lock staff user: %w", err)
	}
	var employmentStatus string
	var version int64
	var staffDeleted bool
	err = tx.QueryRow(ctx, `SELECT employment_status::text,version,is_deleted FROM staff_profiles WHERE id=$1 FOR UPDATE`, staffID).
		Scan(&employmentStatus, &version, &staffDeleted)
	if err != nil {
		return Profile{}, fmt.Errorf("lock staff profile: %w", err)
	}
	if version != input.ExpectedVersion {
		return Profile{}, concurrent()
	}
	if roleCode == "SUPER_ADMIN" {
		return Profile{}, protected()
	}
	if staffDeleted || userDeleted {
		return Profile{}, problem.New(409, "STAFF_DELETED", "Nhân sự đã bị lưu trữ.")
	}
	if action == "activate" {
		if employmentStatus == "TERMINATED" {
			return Profile{}, problem.New(409, "STAFF_TERMINATED", "Nhân sự đã chấm dứt làm việc.")
		}
		if employmentStatus == "ACTIVE" && accountStatus == "ACTIVE" {
			return service.finishCurrent(ctx, tx, staffID)
		}
		if roleStatus != "ACTIVE" {
			return Profile{}, problem.New(409, "AUTHZ_ROLE_INACTIVE", "Role hiện tại không hoạt động.")
		}
		var overrides int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM user_permission_overrides WHERE user_id=$1 AND is_deleted=false`, userID).Scan(&overrides); err != nil {
			return Profile{}, fmt.Errorf("count staff overrides: %w", err)
		}
		if overrides > 0 && !input.ConfirmExistingOverrides {
			return Profile{}, problem.New(409, "STAFF_OVERRIDE_CONFIRMATION_REQUIRED", "Phải xác nhận các permission override hiện có.")
		}
		if _, err := tx.Exec(ctx, `UPDATE staff_profiles SET employment_status='ACTIVE',updated_by=$2,updated_at=now(),version=version+1 WHERE id=$1`, staffID, actor.UserID); err != nil {
			return Profile{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET status='ACTIVE',failed_login_count=0,locked_until=NULL,updated_at=now(),version=version+1 WHERE id=$1`, userID); err != nil {
			return Profile{}, err
		}
		event := audit.UserEvent(actor.UserID, "STAFF", "STAFF_REACTIVATED", "STAFF_PROFILE", staffID)
		event.Metadata = map[string]any{"confirmed_existing_overrides": input.ConfirmExistingOverrides}
		if err := audit.Insert(ctx, tx, event); err != nil {
			return Profile{}, err
		}
	} else {
		if employmentStatus == "TERMINATED" {
			return Profile{}, problem.New(409, "STAFF_TERMINATED", "Nhân sự đã chấm dứt làm việc.")
		}
		if action == "deactivate" && employmentStatus == "INACTIVE" {
			return service.finishCurrent(ctx, tx, staffID)
		}
		if err := service.ensureNoFutureAssignments(ctx, tx, staffID); err != nil {
			return Profile{}, err
		}
		newStatus, actionCode := "INACTIVE", "STAFF_DEACTIVATED"
		if action == "terminate" {
			newStatus, actionCode = "TERMINATED", "STAFF_TERMINATED"
		}
		if _, err := tx.Exec(ctx, `UPDATE staff_profiles SET employment_status=$2,updated_by=$3,updated_at=now(),version=version+1 WHERE id=$1`, staffID, newStatus, actor.UserID); err != nil {
			return Profile{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET status='INACTIVE',token_version=token_version+1,updated_at=now(),version=version+1 WHERE id=$1`, userID); err != nil {
			return Profile{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now(),revoke_reason=$2,updated_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID, actionCode); err != nil {
			return Profile{}, err
		}
		event := audit.UserEvent(actor.UserID, "STAFF", actionCode, "STAFF_PROFILE", staffID)
		event.Metadata = map[string]any{"reason": strings.TrimSpace(input.Reason)}
		if err := audit.Insert(ctx, tx, event); err != nil {
			return Profile{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, fmt.Errorf("commit staff status: %w", err)
	}
	return service.Get(ctx, staffID, false)
}

func (service *Service) CreateDoctorProfile(ctx context.Context, actor auth.Principal, staffID, expectedVersion int64) (Profile, error) {
	var userID int64
	if err := service.pool.QueryRow(ctx, `SELECT user_id FROM staff_profiles WHERE id=$1`, staffID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, notFound()
	} else if err != nil {
		return Profile{}, err
	}
	tx, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return Profile{}, err
	}
	var status string
	var version int64
	var deleted bool
	err = tx.QueryRow(ctx, `SELECT employment_status::text,version,is_deleted FROM staff_profiles WHERE id=$1 FOR UPDATE`, staffID).Scan(&status, &version, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, notFound()
	}
	if err != nil {
		return Profile{}, err
	}
	if version != expectedVersion {
		return Profile{}, concurrent()
	}
	if status != "ACTIVE" || deleted {
		return Profile{}, problem.New(409, "CLINIC_DOCTOR_INACTIVE", "Nhân sự không đủ điều kiện tạo hồ sơ bác sĩ.")
	}
	var doctorID int64
	err = tx.QueryRow(ctx, `INSERT INTO doctor_profiles(staff_profile_id,created_by) VALUES($1,$2) RETURNING id`, staffID, actor.UserID).Scan(&doctorID)
	if err != nil {
		return Profile{}, mapCreateError(err)
	}
	event := audit.UserEvent(actor.UserID, "STAFF", "DOCTOR_PROFILE_CREATED", "DOCTOR_PROFILE", doctorID)
	event.Metadata = map[string]any{"staff_profile_id": staffID}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return service.Get(ctx, staffID, false)
}

func (service *Service) ChangeDoctorStatus(ctx context.Context, actor auth.Principal, staffID int64, activate bool, input StatusInput) (Profile, error) {
	var userID, resolvedDoctorID int64
	if err := service.pool.QueryRow(ctx, `SELECT sp.user_id,dp.id FROM staff_profiles sp JOIN doctor_profiles dp ON dp.staff_profile_id=sp.id WHERE sp.id=$1`, staffID).Scan(&userID, &resolvedDoctorID); errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, problem.New(404, "DOCTOR_PROFILE_NOT_FOUND", "Không tìm thấy hồ sơ bác sĩ.")
	} else if err != nil {
		return Profile{}, err
	}
	tx, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return Profile{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM staff_profiles WHERE id=$1 FOR UPDATE`, staffID); err != nil {
		return Profile{}, err
	}
	var doctorID int64
	var status string
	var version int64
	var deleted bool
	err = tx.QueryRow(ctx, `SELECT id,status::text,version,is_deleted FROM doctor_profiles WHERE id=$1 FOR UPDATE`, resolvedDoctorID).Scan(&doctorID, &status, &version, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, problem.New(404, "DOCTOR_PROFILE_NOT_FOUND", "Không tìm thấy hồ sơ bác sĩ.")
	}
	if err != nil {
		return Profile{}, err
	}
	if version != input.ExpectedVersion {
		return Profile{}, concurrent()
	}
	if deleted {
		return Profile{}, problem.New(409, "DOCTOR_PROFILE_DELETED", "Hồ sơ bác sĩ đã bị lưu trữ.")
	}
	target, actionCode := "INACTIVE", "DOCTOR_PROFILE_DEACTIVATED"
	if activate {
		target, actionCode = "ACTIVE", "DOCTOR_PROFILE_REACTIVATED"
	}
	if status == target {
		return service.finishCurrent(ctx, tx, staffID)
	}
	if !activate {
		if strings.TrimSpace(input.Reason) == "" {
			return Profile{}, invalid("Lý do là bắt buộc.")
		}
		if err := service.ensureNoFutureAssignments(ctx, tx, staffID); err != nil {
			return Profile{}, err
		}
	} else {
		var eligible bool
		if err := tx.QueryRow(ctx, `SELECT employment_status='ACTIVE' AND NOT is_deleted FROM staff_profiles WHERE id=$1`, staffID).Scan(&eligible); err != nil {
			return Profile{}, err
		}
		if !eligible {
			return Profile{}, problem.New(409, "CLINIC_DOCTOR_INACTIVE", "Nhân sự không hoạt động.")
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE doctor_profiles SET status=$2,updated_at=now(),version=version+1 WHERE id=$1`, doctorID, target); err != nil {
		return Profile{}, err
	}
	event := audit.UserEvent(actor.UserID, "STAFF", actionCode, "DOCTOR_PROFILE", doctorID)
	event.Metadata = map[string]any{"reason": strings.TrimSpace(input.Reason)}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return service.Get(ctx, staffID, false)
}

func (service *Service) Archive(ctx context.Context, actor auth.Principal, staffID, expectedVersion int64, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return invalid("Lý do soft delete là bắt buộc.")
	}
	var userID int64
	if err := service.pool.QueryRow(ctx, `SELECT user_id FROM staff_profiles WHERE id=$1`, staffID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return notFound()
	} else if err != nil {
		return err
	}
	tx, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var roleCode string
	var userDeleted bool
	if err := tx.QueryRow(ctx, `SELECT r.code,u.is_deleted FROM users u JOIN roles r ON r.id=u.role_id WHERE u.id=$1 FOR UPDATE OF u`, userID).Scan(&roleCode, &userDeleted); err != nil {
		return err
	}
	var status string
	var version int64
	var staffDeleted bool
	if err := tx.QueryRow(ctx, `SELECT employment_status::text,version,is_deleted FROM staff_profiles WHERE id=$1 FOR UPDATE`, staffID).Scan(&status, &version, &staffDeleted); err != nil {
		return err
	}
	if version != expectedVersion {
		return concurrent()
	}
	if roleCode == "SUPER_ADMIN" {
		return protected()
	}
	if staffDeleted || userDeleted {
		return nil
	}
	if status != "INACTIVE" && status != "TERMINATED" {
		return problem.New(409, "STAFF_MUST_BE_INACTIVE", "Phải vô hiệu hóa hoặc chấm dứt nhân sự trước khi soft delete.")
	}
	var doctorID int64
	doctorErr := tx.QueryRow(ctx, `SELECT id FROM doctor_profiles WHERE staff_profile_id=$1 FOR UPDATE`, staffID).Scan(&doctorID)
	if doctorErr != nil && !errors.Is(doctorErr, pgx.ErrNoRows) {
		return doctorErr
	}
	if err := service.ensureNoFutureAssignments(ctx, tx, staffID); err != nil {
		return err
	}
	if doctorErr == nil {
		if _, err := tx.Exec(ctx, `UPDATE doctor_profiles SET status='INACTIVE',is_deleted=true,updated_at=now(),version=version+1 WHERE id=$1`, doctorID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE staff_profiles SET is_deleted=true,updated_by=$2,updated_at=now(),version=version+1 WHERE id=$1`, staffID, actor.UserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET is_deleted=true,status='INACTIVE',token_version=token_version+1,updated_at=now(),version=version+1 WHERE id=$1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now(),revoke_reason='STAFF_ARCHIVED',updated_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	event := audit.UserEvent(actor.UserID, "STAFF", "STAFF_SOFT_DELETED", "STAFF_PROFILE", staffID)
	event.Metadata = map[string]any{"reason": strings.TrimSpace(reason)}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (service *Service) Restore(ctx context.Context, actor auth.Principal, staffID, expectedVersion int64, reason string) (Profile, error) {
	if actor.RoleCode != "SUPER_ADMIN" || strings.TrimSpace(reason) == "" {
		return Profile{}, protected()
	}
	var userID int64
	if err := service.pool.QueryRow(ctx, `SELECT user_id FROM staff_profiles WHERE id=$1`, staffID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, notFound()
	} else if err != nil {
		return Profile{}, err
	}
	tx, err := service.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var roleCode, roleStatus string
	var userDeleted bool
	if err := tx.QueryRow(ctx, `SELECT r.code,r.status::text,u.is_deleted FROM users u JOIN roles r ON r.id=u.role_id WHERE u.id=$1 FOR UPDATE OF u`, userID).Scan(&roleCode, &roleStatus, &userDeleted); err != nil {
		return Profile{}, err
	}
	var version int64
	var staffDeleted bool
	if err := tx.QueryRow(ctx, `SELECT version,is_deleted FROM staff_profiles WHERE id=$1 FOR UPDATE`, staffID).Scan(&version, &staffDeleted); err != nil {
		return Profile{}, err
	}
	if version != expectedVersion {
		return Profile{}, concurrent()
	}
	if roleCode == "SUPER_ADMIN" {
		return Profile{}, protected()
	}
	if !staffDeleted || !userDeleted {
		return Profile{}, problem.New(409, "STAFF_NOT_ARCHIVED", "Nhân sự chưa được soft delete.")
	}
	if roleStatus != "ACTIVE" {
		return Profile{}, problem.New(409, "AUTHZ_ROLE_INACTIVE", "Role hiện tại không hoạt động.")
	}
	var doctorID int64
	doctorErr := tx.QueryRow(ctx, `SELECT id FROM doctor_profiles WHERE staff_profile_id=$1 FOR UPDATE`, staffID).Scan(&doctorID)
	if doctorErr != nil && !errors.Is(doctorErr, pgx.ErrNoRows) {
		return Profile{}, doctorErr
	}
	if doctorErr == nil {
		if _, err := tx.Exec(ctx, `UPDATE doctor_profiles SET is_deleted=false,status='INACTIVE',updated_at=now(),version=version+1 WHERE id=$1`, doctorID); err != nil {
			return Profile{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE staff_profiles SET is_deleted=false,updated_by=$2,updated_at=now(),version=version+1 WHERE id=$1`, staffID, actor.UserID); err != nil {
		return Profile{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET is_deleted=false,status='INACTIVE',failed_login_count=0,locked_until=NULL,updated_at=now(),version=version+1 WHERE id=$1`, userID); err != nil {
		return Profile{}, err
	}
	event := audit.UserEvent(actor.UserID, "STAFF", "STAFF_RESTORED", "STAFF_PROFILE", staffID)
	event.Metadata = map[string]any{"reason": strings.TrimSpace(reason)}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return service.Get(ctx, staffID, false)
}

func (service *Service) Get(ctx context.Context, staffID int64, includeDeleted bool) (Profile, error) {
	return scanProfile(service.pool.QueryRow(ctx, profileQuery+` WHERE sp.id=$1 AND ($2 OR sp.is_deleted=false)`, staffID, includeDeleted))
}

func (service *Service) GetByUser(ctx context.Context, userID int64) (Profile, error) {
	return scanProfile(service.pool.QueryRow(ctx, profileQuery+` WHERE sp.user_id=$1 AND sp.is_deleted=false`, userID))
}

func (service *Service) List(ctx context.Context, keyword, status string, includeDeleted bool, page, pageSize int) ([]Profile, error) {
	return service.list(ctx, keyword, status, includeDeleted, page, pageSize, false)
}

func (service *Service) ListPage(ctx context.Context, keyword, status string, includeDeleted bool, page, pageSize int) (pagination.Page[Profile], error) {
	page, pageSize = pagination.Normalize(page, pageSize)
	items, err := service.list(ctx, keyword, status, includeDeleted, page, pageSize, true)
	return pagination.From(items, page, pageSize), err
}

func (service *Service) list(ctx context.Context, keyword, status string, includeDeleted bool, page, pageSize int, extra bool) ([]Profile, error) {
	fetch := pageSize
	if extra {
		fetch++
	}
	offset := (page - 1) * pageSize
	rows, err := service.pool.Query(ctx, profileQuery+`
		WHERE ($1 OR sp.is_deleted=false)
		  AND ($2='' OR sp.employment_status::text=$2)
		  AND ($3='' OR sp.staff_code ILIKE '%'||$3||'%' OR sp.full_name ILIKE '%'||$3||'%' OR u.normalized_username ILIKE '%'||lower($3)||'%')
		ORDER BY sp.created_at DESC,sp.id DESC LIMIT $4 OFFSET $5`, includeDeleted, strings.ToUpper(status), strings.TrimSpace(keyword), fetch, offset)
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}
	defer rows.Close()
	result := make([]Profile, 0)
	for rows.Next() {
		item, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

const profileQuery = `
	SELECT sp.id,sp.user_id,sp.staff_code,u.username,sp.full_name,sp.phone_number,sp.email,
	       to_char(sp.date_of_birth,'YYYY-MM-DD'),sp.gender_code,sp.address,sp.employment_status::text,
	       u.status::text,u.role_id,r.code,u.must_change_password,sp.version,sp.is_deleted,
	       dp.id,dp.doctor_code,dp.status::text,dp.version,sp.created_at,sp.updated_at,u.version
	FROM staff_profiles sp JOIN users u ON u.id=sp.user_id JOIN roles r ON r.id=u.role_id
	LEFT JOIN doctor_profiles dp ON dp.staff_profile_id=sp.id`

type rowScanner interface{ Scan(...any) error }

func scanProfile(row rowScanner) (Profile, error) {
	var p Profile
	var doctorID *int64
	var doctorCode, doctorStatus *string
	var doctorVersion *int64
	err := row.Scan(&p.ID, &p.UserID, &p.StaffCode, &p.Username, &p.FullName, &p.PhoneNumber, &p.Email, &p.DateOfBirth, &p.GenderCode, &p.Address, &p.EmploymentStatus, &p.AccountStatus, &p.RoleID, &p.RoleCode, &p.MustChangePassword, &p.Version, &p.IsDeleted, &doctorID, &doctorCode, &doctorStatus, &doctorVersion, &p.CreatedAt, &p.UpdatedAt, &p.AccountVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, notFound()
	}
	if err != nil {
		return Profile{}, fmt.Errorf("scan staff profile: %w", err)
	}
	if doctorID != nil {
		p.DoctorProfile = &DoctorProfile{ID: *doctorID, DoctorCode: *doctorCode, Status: *doctorStatus, Version: *doctorVersion}
	}
	return p, nil
}

type validatedFields struct {
	fullName                                                    string
	phone, email, normalizedEmail, dateOfBirth, gender, address any
}

func (service *Service) validateProfileFields(fullName, phone, email, dob, gender, address string) (validatedFields, error) {
	name, err := textnorm.HumanName(fullName, 150)
	if err != nil {
		return validatedFields{}, invalid("Họ tên không hợp lệ.")
	}
	var normalizedPhone any
	if strings.TrimSpace(phone) != "" {
		v, e := auth.NormalizeVietnamPhone(phone)
		if e != nil {
			return validatedFields{}, invalid("Số điện thoại không hợp lệ.")
		}
		normalizedPhone = v
	}
	emailValue, normalizedEmail, err := textnorm.Email(email)
	if err != nil {
		return validatedFields{}, invalid("Email không hợp lệ.")
	}
	var dateValue any
	if strings.TrimSpace(dob) != "" {
		parsed, e := time.ParseInLocation("2006-01-02", dob, service.location)
		if e != nil || parsed.After(service.now().In(service.location)) {
			return validatedFields{}, invalid("Ngày sinh không hợp lệ.")
		}
		dateValue = parsed
	}
	var genderValue any
	if strings.TrimSpace(gender) != "" {
		v := strings.ToUpper(strings.TrimSpace(gender))
		if !genderPattern.MatchString(v) {
			return validatedFields{}, invalid("Giới tính không hợp lệ.")
		}
		genderValue = v
	}
	addressValue, err := textnorm.Optional(address, 500)
	if err != nil {
		return validatedFields{}, invalid("Địa chỉ quá dài.")
	}
	return validatedFields{fullName: name, phone: normalizedPhone, email: emailValue, normalizedEmail: normalizedEmail, dateOfBirth: dateValue, gender: genderValue, address: addressValue}, nil
}

func (service *Service) ensureNoFutureAssignments(ctx context.Context, tx pgx.Tx, staffID int64) error {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(
		 SELECT 1 FROM doctor_profiles dp JOIN doctor_schedules ds ON ds.doctor_profile_id=dp.id
		 WHERE dp.staff_profile_id=$1 AND ds.status='ACTIVE' AND ds.end_at>now()
		 UNION ALL
		 SELECT 1 FROM doctor_profiles dp JOIN appointments a ON a.doctor_profile_id=dp.id
		 WHERE dp.staff_profile_id=$1 AND a.end_at>now() AND a.status IN('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS')
		)`, staffID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check future staff assignments: %w", err)
	}
	if exists {
		return problem.New(409, "STAFF_HAS_FUTURE_ASSIGNMENTS", "Nhân sự còn lịch hoặc lịch hẹn tương lai cần xử lý.")
	}
	return nil
}

func (service *Service) finishCurrent(ctx context.Context, tx pgx.Tx, staffID int64) (Profile, error) {
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return service.Get(ctx, staffID, true)
}

func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func invalid(message string) *problem.Error { return problem.New(422, "VALIDATION_ERROR", message) }
func notFound() *problem.Error {
	return problem.New(404, "STAFF_NOT_FOUND", "Không tìm thấy nhân sự.")
}
func protected() *problem.Error {
	return problem.New(403, "STAFF_PROTECTED_OPERATION_FORBIDDEN", "Không được thao tác lên tài khoản được bảo vệ.")
}
func concurrent() *problem.Error {
	return problem.New(409, "STAFF_CONCURRENT_UPDATE", "Dữ liệu nhân sự đã được cập nhật bởi request khác.")
}
func mapCreateError(err error) error {
	switch pgutil.Constraint(err) {
	case "uq_users_normalized_username":
		return problem.New(409, "AUTH_USERNAME_ALREADY_EXISTS", "Username đã được sử dụng.")
	case "uq_staff_profiles_user":
		return problem.New(409, "STAFF_PROFILE_ALREADY_EXISTS", "User đã có hồ sơ nhân sự.")
	case "uq_doctor_profiles_staff":
		return problem.New(409, "DOCTOR_PROFILE_ALREADY_EXISTS", "Nhân sự đã có hồ sơ bác sĩ.")
	case "uq_staff_profiles_phone":
		return problem.New(409, "STAFF_PHONE_ALREADY_EXISTS", "Số điện thoại liên hệ đã được sử dụng.")
	case "uq_staff_profiles_email":
		return problem.New(409, "STAFF_EMAIL_ALREADY_EXISTS", "Email đã được sử dụng.")
	default:
		return fmt.Errorf("write staff aggregate: %w", err)
	}
}
