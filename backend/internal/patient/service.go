package patient

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

var (
	cccdPattern      = regexp.MustCompile(`^[0-9]{12}$`)
	insurancePattern = regexp.MustCompile(`^[A-Z0-9]{15}$`)
	genderPattern    = regexp.MustCompile(`^(MALE|FEMALE|OTHER|UNSPECIFIED)$`)
)

type Service struct {
	pool      *pgxpool.Pool
	passwords auth.PasswordManager
}

func NewService(pool *pgxpool.Pool, passwords auth.PasswordManager) *Service {
	return &Service{pool: pool, passwords: passwords}
}

type Profile struct {
	ID                    int64      `json:"id"`
	UserID                int64      `json:"user_id"`
	PatientCode           string     `json:"patient_code"`
	PhoneNumber           string     `json:"phone_number"`
	FullName              string     `json:"full_name"`
	DateOfBirth           *string    `json:"date_of_birth,omitempty"`
	GenderCode            *string    `json:"gender_code,omitempty"`
	Address               *string    `json:"address,omitempty"`
	CCCD                  *string    `json:"cccd,omitempty"`
	HealthInsuranceNumber *string    `json:"health_insurance_number,omitempty"`
	MedicalHistory        *string    `json:"medical_history,omitempty"`
	Allergies             *string    `json:"allergies,omitempty"`
	CreatedSource         string     `json:"created_source"`
	AccountStatus         string     `json:"account_status"`
	AccountVersion        int64      `json:"account_version"`
	MustChangePassword    bool       `json:"must_change_password"`
	Version               int64      `json:"version"`
	IsDeleted             bool       `json:"is_deleted"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	DeletedAt             *time.Time `json:"deleted_at,omitempty"`
}

type CreateInput struct {
	PhoneNumber           string
	Password              string
	FullName              string
	DateOfBirth           string
	GenderCode            string
	Address               string
	CCCD                  string
	HealthInsuranceNumber string
	MedicalHistory        string
	Allergies             string
}

type Created struct {
	Profile           Profile `json:"patient"`
	TemporaryPassword string  `json:"temporary_password,omitempty"`
	UserID            int64   `json:"-"`
}

func (s *Service) Create(ctx context.Context, actor *auth.Principal, input CreateInput) (Created, error) {
	phone, err := auth.NormalizeVietnamPhone(input.PhoneNumber)
	if err != nil {
		return Created{}, invalid("Số điện thoại không hợp lệ.")
	}
	fields, err := validateFields(input)
	if err != nil {
		return Created{}, err
	}
	password := input.Password
	temporary := ""
	if actor != nil {
		temporary, err = auth.GenerateTemporaryPassword()
		if err != nil {
			return Created{}, err
		}
		password = temporary
	}
	hash, err := s.passwords.Hash(password)
	if err != nil {
		if errors.Is(err, auth.ErrPasswordPolicy) {
			return Created{}, invalid("Mật khẩu phải dài từ 8 đến 128 ký tự.")
		}
		return Created{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Created{}, fmt.Errorf("begin patient create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var roleID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE code='PATIENT' AND account_scope='PATIENT' AND status='ACTIVE' AND is_deleted=false`).Scan(&roleID); err != nil {
		return Created{}, fmt.Errorf("load patient role: %w", err)
	}
	var userID int64
	mustChange := actor != nil
	err = tx.QueryRow(ctx, `
		INSERT INTO users(role_id,account_type,phone_number,normalized_phone_number,password_hash,status,must_change_password)
		VALUES($1,'PATIENT',$2,$2,$3,'ACTIVE',$4) RETURNING id`, roleID, phone, hash, mustChange).Scan(&userID)
	if err != nil {
		return Created{}, mapConstraint(err)
	}
	source := "SELF_REGISTRATION"
	createdBy := userID
	if actor != nil {
		createdBy = actor.UserID
		if actor.RoleCode == "CASHIER" {
			source = "COUNTER_CASHIER"
		} else {
			source = "COUNTER_ADMIN"
		}
	}
	var patientID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO patients(user_id,full_name,normalized_full_name,date_of_birth,gender_code,address,cccd,
			health_insurance_number,medical_history,allergies,created_source,created_by_user_id,updated_by_user_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12) RETURNING id`,
		userID, fields.name, fields.normalizedName, fields.dob, fields.gender, fields.address,
		fields.cccd, fields.insurance, fields.history, fields.allergies, source, createdBy).Scan(&patientID)
	if err != nil {
		return Created{}, mapConstraint(err)
	}
	event := audit.UserEvent(createdBy, "PATIENT", "PATIENT_ACCOUNT_CREATED", "PATIENT", patientID)
	event.Metadata = map[string]any{"source": source, "user_id": userID}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Created{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Created{}, fmt.Errorf("commit patient create: %w", err)
	}
	profile, err := s.Get(ctx, patientID, true)
	return Created{Profile: profile, TemporaryPassword: temporary, UserID: userID}, err
}

type UpdateInput struct {
	ExpectedVersion       int64
	FullName              *string
	DateOfBirth           *string
	GenderCode            *string
	Address               *string
	CCCD                  *string
	HealthInsuranceNumber *string
	MedicalHistory        *string
	Allergies             *string
}

func (s *Service) Update(ctx context.Context, actor auth.Principal, patientID int64, input UpdateInput, own bool) (Profile, error) {
	current, err := s.Get(ctx, patientID, false)
	if err != nil {
		return Profile{}, err
	}
	if own && current.UserID != actor.UserID {
		return Profile{}, forbidden()
	}
	create := CreateInput{FullName: current.FullName, DateOfBirth: value(current.DateOfBirth), GenderCode: value(current.GenderCode), Address: value(current.Address), CCCD: value(current.CCCD), HealthInsuranceNumber: value(current.HealthInsuranceNumber), MedicalHistory: value(current.MedicalHistory), Allergies: value(current.Allergies)}
	if input.FullName != nil {
		create.FullName = *input.FullName
	}
	if input.DateOfBirth != nil {
		create.DateOfBirth = *input.DateOfBirth
	}
	if input.GenderCode != nil {
		create.GenderCode = *input.GenderCode
	}
	if input.Address != nil {
		create.Address = *input.Address
	}
	if input.CCCD != nil {
		create.CCCD = *input.CCCD
	}
	if input.HealthInsuranceNumber != nil {
		create.HealthInsuranceNumber = *input.HealthInsuranceNumber
	}
	if input.MedicalHistory != nil {
		create.MedicalHistory = *input.MedicalHistory
	}
	if input.Allergies != nil {
		create.Allergies = *input.Allergies
	}
	fields, err := validateFields(create)
	if err != nil {
		return Profile{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
		UPDATE patients SET full_name=$3,normalized_full_name=$4,date_of_birth=$5,gender_code=$6,address=$7,
			cccd=$8,health_insurance_number=$9,medical_history=$10,allergies=$11,updated_by_user_id=$12,
			updated_at=now(),version=version+1
		WHERE id=$1 AND version=$2 AND is_deleted=false`, patientID, input.ExpectedVersion, fields.name,
		fields.normalizedName, fields.dob, fields.gender, fields.address, fields.cccd, fields.insurance,
		fields.history, fields.allergies, actor.UserID)
	if err != nil {
		return Profile{}, mapConstraint(err)
	}
	if command.RowsAffected() != 1 {
		return Profile{}, concurrent()
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "PATIENT", "PATIENT_PROFILE_UPDATED", "PATIENT", patientID)); err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return s.Get(ctx, patientID, false)
}

func (s *Service) Get(ctx context.Context, id int64, includeDeleted bool) (Profile, error) {
	row := s.pool.QueryRow(ctx, profileQuery+` WHERE p.id=$1 AND ($2 OR p.is_deleted=false)`, id, includeDeleted)
	return scanProfile(row)
}

func (s *Service) GetOwn(ctx context.Context, userID int64) (Profile, error) {
	return scanProfile(s.pool.QueryRow(ctx, profileQuery+` WHERE p.user_id=$1 AND p.is_deleted=false`, userID))
}

func (s *Service) GetScoped(ctx context.Context, principal auth.Principal, id int64) (Profile, error) {
	profile, err := s.Get(ctx, id, false)
	if err != nil {
		return Profile{}, err
	}
	if principal.AccountType == auth.AccountTypePatient && profile.UserID != principal.UserID {
		return Profile{}, forbidden()
	}
	if principal.RoleCode == "DOCTOR" {
		var allowed bool
		err := s.pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id
			JOIN appointments a ON a.doctor_profile_id=d.id
			WHERE sp.user_id=$1 AND a.patient_id=$2 AND a.status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))`, principal.UserID, id).Scan(&allowed)
		if err != nil {
			return Profile{}, err
		}
		if !allowed {
			return Profile{}, forbidden()
		}
	}
	return profile, nil
}

func (s *Service) List(ctx context.Context, principal auth.Principal, keyword string, page, size int) ([]Profile, error) {
	return s.list(ctx, principal, keyword, page, size, false)
}

func (s *Service) ListPage(ctx context.Context, principal auth.Principal, keyword string, page, size int) (pagination.Page[Profile], error) {
	page, size = pagination.Normalize(page, size)
	items, err := s.list(ctx, principal, keyword, page, size, true)
	return pagination.From(items, page, size), err
}

func (s *Service) list(ctx context.Context, principal auth.Principal, keyword string, page, size int, extra bool) ([]Profile, error) {
	fetch := size
	if extra {
		fetch++
	}
	raw := strings.TrimSpace(keyword)
	phone, _ := auth.NormalizeVietnamPhone(raw)
	cccd := textnorm.Digits(raw)
	rows, err := s.pool.Query(ctx, profileQuery+`
		WHERE p.is_deleted=false AND ($1='' OR p.patient_code=$2 OR u.normalized_phone_number=$3 OR p.cccd=$4 OR p.normalized_full_name LIKE '%'||$5||'%')
		AND ($6<>'DOCTOR' OR EXISTS(SELECT 1 FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id
			JOIN appointments a ON a.doctor_profile_id=d.id WHERE sp.user_id=$7 AND a.patient_id=p.id
			AND a.status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS')))
		ORDER BY p.created_at DESC,p.id DESC LIMIT $8 OFFSET $9`, raw, textnorm.UpperCode(raw), phone, cccd, textnorm.Search(raw), principal.RoleCode, principal.UserID, fetch, (page-1)*size)
	if err != nil {
		return nil, err
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

func (s *Service) ChangePhone(ctx context.Context, actor auth.Principal, id, expectedVersion int64, phone, reason string) (Profile, error) {
	normalized, err := auth.NormalizeVietnamPhone(phone)
	if err != nil || strings.TrimSpace(reason) == "" {
		return Profile{}, invalid("Số điện thoại hoặc lý do không hợp lệ.")
	}
	var userID int64
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM patients WHERE id=$1`, id).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, notFound()
	} else if err != nil {
		return Profile{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return Profile{}, err
	}
	var version int64
	if err := tx.QueryRow(ctx, `SELECT version FROM patients WHERE id=$1 AND is_deleted=false FOR UPDATE`, id).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, notFound()
	} else if err != nil {
		return Profile{}, err
	}
	if version != expectedVersion {
		return Profile{}, concurrent()
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET phone_number=$2,normalized_phone_number=$2,token_version=token_version+1,updated_at=now(),version=version+1 WHERE id=$1`, userID, normalized); err != nil {
		return Profile{}, mapConstraint(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE patients SET updated_by_user_id=$2,updated_at=now(),version=version+1 WHERE id=$1`, id, actor.UserID); err != nil {
		return Profile{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now(),revoke_reason='PATIENT_PHONE_CHANGED',updated_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return Profile{}, err
	}
	event := audit.UserEvent(actor.UserID, "PATIENT", "PATIENT_PHONE_CHANGED", "PATIENT", id)
	event.Metadata = map[string]any{"reason": strings.TrimSpace(reason)}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return s.Get(ctx, id, false)
}

func (s *Service) Archive(ctx context.Context, actor auth.Principal, id, expectedVersion int64, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return invalid("Lý do archive là bắt buộc.")
	}
	var userID int64
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM patients WHERE id=$1`, id).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return notFound()
	} else if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return err
	}
	var version int64
	if err := tx.QueryRow(ctx, `SELECT version FROM patients WHERE id=$1 AND is_deleted=false FOR UPDATE`, id).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return notFound()
	} else if err != nil {
		return err
	}
	if version != expectedVersion {
		return concurrent()
	}
	var holding bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM appointments WHERE patient_id=$1 AND status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))`, id).Scan(&holding); err != nil {
		return err
	}
	if holding {
		return problem.New(409, "PATIENT_HAS_ACTIVE_APPOINTMENTS", "Bệnh nhân còn lịch hẹn đang hoạt động.")
	}
	if _, err := tx.Exec(ctx, `UPDATE patients SET is_deleted=true,deleted_at=now(),deleted_by_user_id=$2,deletion_reason=$3,updated_by_user_id=$2,updated_at=now(),version=version+1 WHERE id=$1`, id, actor.UserID, strings.TrimSpace(reason)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET is_deleted=true,status='INACTIVE',token_version=token_version+1,updated_at=now(),version=version+1 WHERE id=$1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now(),revoke_reason='PATIENT_ARCHIVED',updated_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "PATIENT", "PATIENT_ARCHIVED", "PATIENT", id)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) Restore(ctx context.Context, actor auth.Principal, id, expectedVersion int64) (Created, error) {
	var userID int64
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM patients WHERE id=$1`, id).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return Created{}, notFound()
	} else if err != nil {
		return Created{}, err
	}
	temporary, err := auth.GenerateTemporaryPassword()
	if err != nil {
		return Created{}, err
	}
	hash, err := s.passwords.Hash(temporary)
	if err != nil {
		return Created{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Created{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return Created{}, err
	}
	var version int64
	if err := tx.QueryRow(ctx, `SELECT version FROM patients WHERE id=$1 AND is_deleted=true FOR UPDATE`, id).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return Created{}, problem.New(409, "PATIENT_NOT_ARCHIVED", "Bệnh nhân chưa được archive.")
	} else if err != nil {
		return Created{}, err
	}
	if version != expectedVersion {
		return Created{}, concurrent()
	}
	if _, err := tx.Exec(ctx, `UPDATE patients SET is_deleted=false,restored_at=now(),restored_by_user_id=$2,updated_by_user_id=$2,updated_at=now(),version=version+1 WHERE id=$1`, id, actor.UserID); err != nil {
		return Created{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET is_deleted=false,status='ACTIVE',password_hash=$2,must_change_password=true,failed_login_count=0,locked_until=NULL,token_version=token_version+1,updated_at=now(),version=version+1 WHERE id=$1`, userID, hash); err != nil {
		return Created{}, err
	}
	if err := audit.Insert(ctx, tx, audit.UserEvent(actor.UserID, "PATIENT", "PATIENT_RESTORED", "PATIENT", id)); err != nil {
		return Created{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Created{}, err
	}
	profile, err := s.Get(ctx, id, false)
	return Created{Profile: profile, TemporaryPassword: temporary, UserID: userID}, err
}

const profileQuery = `SELECT p.id,p.user_id,p.patient_code,u.phone_number,p.full_name,p.date_of_birth::text,p.gender_code,p.address,p.cccd,
	p.health_insurance_number,p.medical_history,p.allergies,p.created_source::text,u.status::text,u.must_change_password,
	p.version,p.is_deleted,p.created_at,p.updated_at,p.deleted_at,u.version FROM patients p JOIN users u ON u.id=p.user_id`

type scanner interface{ Scan(...any) error }

func scanProfile(row scanner) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.UserID, &p.PatientCode, &p.PhoneNumber, &p.FullName, &p.DateOfBirth, &p.GenderCode, &p.Address, &p.CCCD, &p.HealthInsuranceNumber, &p.MedicalHistory, &p.Allergies, &p.CreatedSource, &p.AccountStatus, &p.MustChangePassword, &p.Version, &p.IsDeleted, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt, &p.AccountVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, notFound()
	}
	return p, err
}

type fields struct {
	name, normalizedName                                      string
	dob, gender, address, cccd, insurance, history, allergies *string
}

func validateFields(input CreateInput) (fields, error) {
	name, err := textnorm.HumanName(input.FullName, 150)
	if err != nil {
		return fields{}, invalid("Họ tên không hợp lệ.")
	}
	result := fields{name: name, normalizedName: textnorm.Search(name)}
	if input.DateOfBirth != "" {
		date, err := time.Parse("2006-01-02", strings.TrimSpace(input.DateOfBirth))
		if err != nil || date.After(time.Now()) {
			return fields{}, invalid("Ngày sinh không hợp lệ.")
		}
		formatted := date.Format("2006-01-02")
		result.dob = &formatted
	}
	if input.GenderCode != "" {
		value := strings.ToUpper(strings.TrimSpace(input.GenderCode))
		if !genderPattern.MatchString(value) {
			return fields{}, invalid("Giới tính không hợp lệ.")
		}
		result.gender = &value
	}
	if result.address, err = textnorm.Optional(input.Address, 500); err != nil {
		return fields{}, invalid("Địa chỉ quá dài.")
	}
	cccd := textnorm.Digits(input.CCCD)
	if input.CCCD != "" {
		if !cccdPattern.MatchString(cccd) {
			return fields{}, invalid("CCCD phải gồm 12 chữ số.")
		}
		result.cccd = &cccd
	}
	insurance := textnorm.UpperCode(input.HealthInsuranceNumber)
	if insurance != "" {
		if !insurancePattern.MatchString(insurance) {
			return fields{}, invalid("Mã BHYT không hợp lệ.")
		}
		result.insurance = &insurance
	}
	if result.history, err = textnorm.Optional(input.MedicalHistory, 4000); err != nil {
		return fields{}, invalid("Tiền sử bệnh quá dài.")
	}
	if result.allergies, err = textnorm.Optional(input.Allergies, 2000); err != nil {
		return fields{}, invalid("Thông tin dị ứng quá dài.")
	}
	return result, nil
}

func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func invalid(message string) error { return problem.New(422, "PATIENT_VALIDATION_FAILED", message) }
func notFound() error {
	return problem.New(404, "PATIENT_NOT_FOUND", "Không tìm thấy bệnh nhân.")
}
func forbidden() error {
	return problem.New(403, "PATIENT_SCOPE_FORBIDDEN", "Không được truy cập hồ sơ bệnh nhân này.")
}
func concurrent() error {
	return problem.New(409, "CONCURRENT_UPDATE", "Dữ liệu đã thay đổi; hãy tải lại.")
}
func mapConstraint(err error) error {
	name := pgutil.Constraint(err)
	switch name {
	case "uq_users_normalized_phone":
		return problem.New(409, "PATIENT_PHONE_EXISTS", "Số điện thoại đã được sử dụng.")
	case "uq_patients_cccd":
		return problem.New(409, "PATIENT_CCCD_EXISTS", "CCCD đã được sử dụng.")
	case "uq_patients_health_insurance":
		return problem.New(409, "PATIENT_INSURANCE_EXISTS", "Mã BHYT đã được sử dụng.")
	default:
		return err
	}
}
