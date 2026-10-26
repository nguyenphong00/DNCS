package settings

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var appointmentKeys = []string{
	"appointment_auto_approval_enabled",
	"appointment_booking_max_advance_days",
	"appointment_booking_min_lead_minutes",
	"appointment_check_in_early_minutes",
	"appointment_check_in_late_minutes",
	"appointment_max_holding_future_per_patient",
	"appointment_no_show_grace_minutes",
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, now: time.Now} }

type SettingValue struct {
	Key          string    `json:"key"`
	ValueType    string    `json:"value_type"`
	BooleanValue *bool     `json:"boolean_value,omitempty"`
	IntegerValue *int      `json:"integer_value,omitempty"`
	Version      int64     `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AppointmentInput struct {
	AutoApprovalEnabled        bool
	BookingMinLeadMinutes      int
	BookingMaxAdvanceDays      int
	CheckInEarlyMinutes        int
	CheckInLateMinutes         int
	NoShowGraceMinutes         int
	MaxHoldingFuturePerPatient int
	ExpectedVersions           map[string]int64
}

func (s *Service) Settings(ctx context.Context, keys []string) ([]SettingValue, error) {
	rows, err := s.pool.Query(ctx, `SELECT setting_key,value_type::text,boolean_value,integer_value,version,updated_at FROM system_settings WHERE setting_key=ANY($1) ORDER BY setting_key`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SettingValue{}
	for rows.Next() {
		var v SettingValue
		if err = rows.Scan(&v.Key, &v.ValueType, &v.BooleanValue, &v.IntegerValue, &v.Version, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(keys) {
		return nil, problem.New(404, "SETTING_NOT_FOUND", "Thiếu cấu hình hệ thống bắt buộc.")
	}
	return out, nil
}

func (s *Service) Appointment(ctx context.Context) ([]SettingValue, error) {
	return s.Settings(ctx, appointmentKeys)
}

func validateAppointment(in AppointmentInput) error {
	if in.BookingMinLeadMinutes < 0 || in.BookingMinLeadMinutes > 1440 ||
		in.BookingMaxAdvanceDays < 1 || in.BookingMaxAdvanceDays > 365 ||
		in.CheckInEarlyMinutes < 0 || in.CheckInEarlyMinutes > 240 ||
		in.CheckInLateMinutes < 0 || in.CheckInLateMinutes > 240 ||
		in.NoShowGraceMinutes < 0 || in.NoShowGraceMinutes > in.CheckInLateMinutes ||
		in.MaxHoldingFuturePerPatient < 1 || in.MaxHoldingFuturePerPatient > 20 {
		return problem.New(422, "SETTING_VALUE_INVALID", "Giá trị cấu hình Appointment không hợp lệ.")
	}
	if len(in.ExpectedVersions) != len(appointmentKeys) {
		return problem.New(422, "SETTING_VALUE_INVALID", "expected_versions phải chứa đủ nhóm Appointment.")
	}
	for _, key := range appointmentKeys {
		if in.ExpectedVersions[key] < 1 {
			return problem.New(422, "SETTING_VALUE_INVALID", "expected_versions không hợp lệ.")
		}
	}
	return nil
}

func (s *Service) UpdateAppointment(ctx context.Context, actor auth.Principal, in AppointmentInput) ([]SettingValue, error) {
	if err := validateAppointment(in); err != nil {
		return nil, err
	}
	bools := map[string]bool{"appointment_auto_approval_enabled": in.AutoApprovalEnabled}
	ints := map[string]int{
		"appointment_booking_min_lead_minutes":       in.BookingMinLeadMinutes,
		"appointment_booking_max_advance_days":       in.BookingMaxAdvanceDays,
		"appointment_check_in_early_minutes":         in.CheckInEarlyMinutes,
		"appointment_check_in_late_minutes":          in.CheckInLateMinutes,
		"appointment_no_show_grace_minutes":          in.NoShowGraceMinutes,
		"appointment_max_holding_future_per_patient": in.MaxHoldingFuturePerPatient,
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,setting_key,value_type::text,boolean_value,integer_value,version FROM system_settings WHERE setting_key=ANY($1) ORDER BY setting_key FOR UPDATE`, appointmentKeys)
	if err != nil {
		return nil, err
	}
	type current struct {
		id       int64
		key, typ string
		b        *bool
		i        *int
		version  int64
	}
	items := []current{}
	for rows.Next() {
		var x current
		if err = rows.Scan(&x.id, &x.key, &x.typ, &x.b, &x.i, &x.version); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, x)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) != len(appointmentKeys) {
		return nil, problem.New(404, "SETTING_NOT_FOUND", "Thiếu cấu hình hệ thống bắt buộc.")
	}
	changed := []string{}
	before, after := map[string]any{}, map[string]any{}
	for _, x := range items {
		if in.ExpectedVersions[x.key] != x.version {
			return nil, problem.New(409, "SETTING_CONCURRENT_UPDATE", "Cấu hình đã được cập nhật bởi yêu cầu khác.")
		}
		if x.typ == "BOOLEAN" {
			if x.b == nil {
				return nil, problem.New(422, "SETTING_TYPE_INVALID", "Kiểu cấu hình không hợp lệ.")
			}
			before[x.key], after[x.key] = *x.b, bools[x.key]
			if *x.b != bools[x.key] {
				_, err = tx.Exec(ctx, `UPDATE system_settings SET boolean_value=$2,updated_by_user_id=$3,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, x.id, bools[x.key], actor.UserID)
				changed = append(changed, x.key)
			}
		} else {
			if x.i == nil {
				return nil, problem.New(422, "SETTING_TYPE_INVALID", "Kiểu cấu hình không hợp lệ.")
			}
			before[x.key], after[x.key] = *x.i, ints[x.key]
			if *x.i != ints[x.key] {
				_, err = tx.Exec(ctx, `UPDATE system_settings SET integer_value=$2,updated_by_user_id=$3,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, x.id, ints[x.key], actor.UserID)
				changed = append(changed, x.key)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	ev := audit.UserEvent(actor.UserID, "AUDIT_SETTINGS", "APPOINTMENT_SETTINGS_UPDATED", "SYSTEM_SETTING", items[0].id)
	ev.Metadata = map[string]any{"changed_keys": changed}
	ev.Before = before
	ev.After = after
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Appointment(ctx)
}

func allowedSingleValue(key string, value int) bool {
	switch key {
	case "default_slot_duration_minutes":
		return value == 15 || value == 20 || value == 30 || value == 45 || value == 60
	case "inventory_expiry_alert_days":
		return value >= 1 && value <= 365
	}
	return false
}

func (s *Service) UpdateInteger(ctx context.Context, actor auth.Principal, key string, value int, expected int64) (SettingValue, error) {
	if !allowedSingleValue(key, value) || expected < 1 {
		return SettingValue{}, problem.New(422, "SETTING_VALUE_INVALID", "Giá trị cấu hình không hợp lệ.")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SettingValue{}, err
	}
	defer tx.Rollback(ctx)
	var id, version int64
	var current int
	err = tx.QueryRow(ctx, `SELECT id,integer_value,version FROM system_settings WHERE setting_key=$1 FOR UPDATE`, key).Scan(&id, &current, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return SettingValue{}, problem.New(404, "SETTING_NOT_FOUND", "Không tìm thấy cấu hình.")
	}
	if err != nil {
		return SettingValue{}, err
	}
	if version != expected {
		return SettingValue{}, problem.New(409, "SETTING_CONCURRENT_UPDATE", "Cấu hình đã được cập nhật bởi yêu cầu khác.")
	}
	if current != value {
		if _, err = tx.Exec(ctx, `UPDATE system_settings SET integer_value=$2,updated_by_user_id=$3,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, id, value, actor.UserID); err != nil {
			return SettingValue{}, err
		}
	}
	action := "DEFAULT_SLOT_DURATION_SETTING_UPDATED"
	if key == "inventory_expiry_alert_days" {
		action = "INVENTORY_ALERT_SETTINGS_UPDATED"
	}
	ev := audit.UserEvent(actor.UserID, "AUDIT_SETTINGS", action, "SYSTEM_SETTING", id)
	ev.Metadata = map[string]any{"key": key, "old_value": current, "new_value": value}
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return SettingValue{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SettingValue{}, err
	}
	values, err := s.Settings(ctx, []string{key})
	if err != nil {
		return SettingValue{}, err
	}
	return values[0], nil
}

type BankAccount struct {
	ID                  int64      `json:"id"`
	BankBIN             string     `json:"bank_bin"`
	BankName            string     `json:"bank_name"`
	AccountNumber       string     `json:"account_number"`
	AccountName         string     `json:"account_name"`
	Status              string     `json:"status"`
	Version             int64      `json:"version"`
	IsDeleted           bool       `json:"is_deleted"`
	CreatedByUserID     int64      `json:"created_by_user_id"`
	CreatedAt           time.Time  `json:"created_at"`
	ActivatedByUserID   *int64     `json:"activated_by_user_id,omitempty"`
	ActivatedAt         *time.Time `json:"activated_at,omitempty"`
	DeactivatedByUserID *int64     `json:"deactivated_by_user_id,omitempty"`
	DeactivatedAt       *time.Time `json:"deactivated_at,omitempty"`
	DeletedByUserID     *int64     `json:"deleted_by_user_id,omitempty"`
	DeletedAt           *time.Time `json:"deleted_at,omitempty"`
	ArchiveReason       *string    `json:"archive_reason,omitempty"`
}

type ActiveBankAccount struct {
	ID                                            int64
	BankBIN, BankName, AccountNumber, AccountName string
}

const bankSelect = `SELECT id,btrim(bank_bin),bank_name,account_number,account_name,status::text,version,is_deleted,created_by_user_id,created_at,activated_by_user_id,activated_at,deactivated_by_user_id,deactivated_at,deleted_by_user_id,deleted_at,archive_reason FROM bank_accounts`

func scanBank(row interface{ Scan(...any) error }) (BankAccount, error) {
	var b BankAccount
	err := row.Scan(&b.ID, &b.BankBIN, &b.BankName, &b.AccountNumber, &b.AccountName, &b.Status, &b.Version, &b.IsDeleted, &b.CreatedByUserID, &b.CreatedAt, &b.ActivatedByUserID, &b.ActivatedAt, &b.DeactivatedByUserID, &b.DeactivatedAt, &b.DeletedByUserID, &b.DeletedAt, &b.ArchiveReason)
	return b, err
}

func normalizeName(v string) (string, error) {
	v = strings.Join(strings.Fields(v), " ")
	if v == "" || len([]rune(v)) > 200 {
		return "", problem.New(422, "BANK_ACCOUNT_INVALID", "Thông tin ngân hàng không hợp lệ.")
	}
	return v, nil
}
func validateDigits(v string, min, max int) bool {
	if len(v) < min || len(v) > max {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (s *Service) CreateBank(ctx context.Context, actor auth.Principal, bin, name, number, accountName string) (BankAccount, error) {
	bin = strings.TrimSpace(bin)
	number = strings.TrimSpace(number)
	var err error
	name, err = normalizeName(name)
	if err != nil {
		return BankAccount{}, err
	}
	accountName, err = normalizeName(accountName)
	if err != nil {
		return BankAccount{}, err
	}
	if !validateDigits(bin, 6, 6) || !validateDigits(number, 6, 30) {
		return BankAccount{}, problem.New(422, "BANK_ACCOUNT_INVALID", "BIN hoặc số tài khoản không hợp lệ.")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BankAccount{}, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO bank_accounts(bank_bin,bank_name,account_number,account_name,created_by_user_id)VALUES($1,$2,$3,$4,$5)RETURNING id`, bin, name, number, accountName, actor.UserID).Scan(&id)
	if err != nil {
		return BankAccount{}, mapBankErr(err)
	}
	ev := audit.UserEvent(actor.UserID, "AUDIT_SETTINGS", "BANK_ACCOUNT_CREATED", "BANK_ACCOUNT", id)
	ev.After = map[string]any{"bank_bin": bin, "bank_name": name, "status": "INACTIVE"}
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return BankAccount{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return BankAccount{}, err
	}
	return s.Bank(ctx, id, true)
}

func (s *Service) Bank(ctx context.Context, id int64, includeArchived bool) (BankAccount, error) {
	b, err := scanBank(s.pool.QueryRow(ctx, bankSelect+` WHERE id=$1 AND ($2 OR NOT is_deleted)`, id, includeArchived))
	if errors.Is(err, pgx.ErrNoRows) {
		return BankAccount{}, problem.New(404, "BANK_ACCOUNT_NOT_FOUND", "Không tìm thấy tài khoản ngân hàng.")
	}
	return b, err
}
func (s *Service) Banks(ctx context.Context, includeArchived bool, page, size int) ([]BankAccount, error) {
	rows, err := s.pool.Query(ctx, bankSelect+` WHERE ($1 OR NOT is_deleted) ORDER BY bank_name,id LIMIT $2 OFFSET $3`, includeArchived, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BankAccount{}
	for rows.Next() {
		b, e := scanBank(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Service) ActivateBank(ctx context.Context, actor auth.Principal, id, expected int64) (BankAccount, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BankAccount{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM bank_accounts WHERE id=$1 OR (status='ACTIVE' AND NOT is_deleted) ORDER BY id FOR UPDATE`, id)
	if err != nil {
		return BankAccount{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var x int64
		if err = rows.Scan(&x); err != nil {
			rows.Close()
			return BankAccount{}, err
		}
		ids = append(ids, x)
	}
	rows.Close()
	if len(ids) == 0 {
		return BankAccount{}, problem.New(404, "BANK_ACCOUNT_NOT_FOUND", "Không tìm thấy tài khoản ngân hàng.")
	}
	var candidate BankAccount
	candidate, err = scanBank(tx.QueryRow(ctx, bankSelect+` WHERE id=$1`, id))
	if err != nil {
		return BankAccount{}, err
	}
	if candidate.IsDeleted {
		return BankAccount{}, problem.New(409, "BANK_ACCOUNT_ARCHIVED", "Tài khoản ngân hàng đã archive.")
	}
	if candidate.Status == "ACTIVE" {
		return BankAccount{}, problem.New(409, "BANK_ACCOUNT_ALREADY_ACTIVE", "Tài khoản ngân hàng đã active.")
	}
	if candidate.Version != expected {
		return BankAccount{}, problem.New(409, "BANK_ACCOUNT_CONCURRENT_UPDATE", "Tài khoản ngân hàng đã được cập nhật.")
	}
	for _, x := range ids {
		if x == id {
			continue
		}
		var code string
		err = tx.QueryRow(ctx, `UPDATE bank_accounts SET status='INACTIVE',deactivated_by_user_id=$2,deactivated_at=clock_timestamp(),version=version+1 WHERE id=$1 AND status='ACTIVE' RETURNING btrim(bank_bin)`, x, actor.UserID).Scan(&code)
		if err == nil {
			ev := audit.UserEvent(actor.UserID, "AUDIT_SETTINGS", "BANK_ACCOUNT_DEACTIVATED", "BANK_ACCOUNT", x)
			ev.Metadata = map[string]any{"reason": "ACTIVE_HANDOVER"}
			if err = audit.Insert(ctx, tx, ev); err != nil {
				return BankAccount{}, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return BankAccount{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE bank_accounts SET status='ACTIVE',activated_by_user_id=$2,activated_at=clock_timestamp(),deactivated_by_user_id=NULL,deactivated_at=NULL,version=version+1 WHERE id=$1`, id, actor.UserID)
	if err != nil {
		return BankAccount{}, mapBankErr(err)
	}
	ev := audit.UserEvent(actor.UserID, "AUDIT_SETTINGS", "BANK_ACCOUNT_ACTIVATED", "BANK_ACCOUNT", id)
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return BankAccount{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return BankAccount{}, mapBankErr(err)
	}
	return s.Bank(ctx, id, false)
}

func (s *Service) DeactivateBank(ctx context.Context, actor auth.Principal, id, expected int64, reason string) (BankAccount, error) {
	return s.bankTerminal(ctx, actor, id, expected, reason, false)
}
func (s *Service) ArchiveBank(ctx context.Context, actor auth.Principal, id, expected int64, reason string) (BankAccount, error) {
	return s.bankTerminal(ctx, actor, id, expected, reason, true)
}
func (s *Service) bankTerminal(ctx context.Context, actor auth.Principal, id, expected int64, reason string, archive bool) (BankAccount, error) {
	reason = strings.Join(strings.Fields(reason), " ")
	if len([]rune(reason)) > 500 || (archive && reason == "") {
		return BankAccount{}, problem.New(422, "BANK_ACCOUNT_INVALID", "Lý do không hợp lệ.")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BankAccount{}, err
	}
	defer tx.Rollback(ctx)
	b, err := scanBank(tx.QueryRow(ctx, bankSelect+` WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return BankAccount{}, problem.New(404, "BANK_ACCOUNT_NOT_FOUND", "Không tìm thấy tài khoản ngân hàng.")
	}
	if err != nil {
		return BankAccount{}, err
	}
	if b.IsDeleted {
		return BankAccount{}, problem.New(409, "BANK_ACCOUNT_ARCHIVED", "Tài khoản ngân hàng đã archive.")
	}
	if b.Version != expected {
		return BankAccount{}, problem.New(409, "BANK_ACCOUNT_CONCURRENT_UPDATE", "Tài khoản ngân hàng đã được cập nhật.")
	}
	action := "BANK_ACCOUNT_DEACTIVATED"
	if archive {
		if b.Status == "ACTIVE" {
			return BankAccount{}, problem.New(409, "BANK_ACCOUNT_ACTIVE_ARCHIVE_FORBIDDEN", "Phải deactivate tài khoản trước khi archive.")
		}
		_, err = tx.Exec(ctx, `UPDATE bank_accounts SET is_deleted=true,deleted_by_user_id=$2,deleted_at=clock_timestamp(),archive_reason=$3,version=version+1 WHERE id=$1`, id, actor.UserID, reason)
		action = "BANK_ACCOUNT_ARCHIVED"
	} else {
		if b.Status != "ACTIVE" {
			return BankAccount{}, problem.New(409, "BANK_ACCOUNT_ALREADY_INACTIVE", "Tài khoản ngân hàng đã inactive.")
		}
		_, err = tx.Exec(ctx, `UPDATE bank_accounts SET status='INACTIVE',deactivated_by_user_id=$2,deactivated_at=clock_timestamp(),version=version+1 WHERE id=$1`, id, actor.UserID)
	}
	if err != nil {
		return BankAccount{}, mapBankErr(err)
	}
	ev := audit.UserEvent(actor.UserID, "AUDIT_SETTINGS", action, "BANK_ACCOUNT", id)
	ev.Metadata = map[string]any{"reason": reason}
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return BankAccount{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return BankAccount{}, err
	}
	return s.Bank(ctx, id, true)
}

func (s *Service) LockActiveBankAccount(ctx context.Context, tx pgx.Tx) (ActiveBankAccount, error) {
	var b ActiveBankAccount
	err := tx.QueryRow(ctx, `SELECT id,btrim(bank_bin),bank_name,account_number,account_name FROM bank_accounts WHERE status='ACTIVE' AND NOT is_deleted FOR SHARE`).Scan(&b.ID, &b.BankBIN, &b.BankName, &b.AccountNumber, &b.AccountName)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, problem.New(409, "BANK_ACCOUNT_ACTIVE_REQUIRED", "Chưa có tài khoản ngân hàng nhận tiền đang active.")
	}
	return b, err
}

func mapBankErr(err error) error {
	switch pgutil.Constraint(err) {
	case "uq_bank_accounts_identity":
		return problem.New(409, "BANK_ACCOUNT_DUPLICATE", "Tài khoản ngân hàng đã tồn tại.")
	case "uq_bank_accounts_one_active":
		return problem.New(409, "BANK_ACCOUNT_CONCURRENT_UPDATE", "Tài khoản active đã thay đổi.")
	}
	return fmt.Errorf("settings persistence: %w", err)
}

type AuditFilter struct {
	Module, Action, Outcome, TargetType, CorrelationID, Cursor string
	ActorUserID, TargetID                                      int64
	From, To                                                   time.Time
	PageSize                                                   int
}
type AuditLog struct {
	ID             int64           `json:"id"`
	OccurredAt     time.Time       `json:"occurred_at"`
	ActorType      string          `json:"actor_type"`
	ActorName      string          `json:"actor_name"`
	TargetLabel    string          `json:"target_label"`
	ActorUserID    *int64          `json:"actor_user_id,omitempty"`
	ActorService   *string         `json:"actor_service,omitempty"`
	Module         string          `json:"module"`
	ActionCode     string          `json:"action_code"`
	Outcome        string          `json:"outcome"`
	TargetType     *string         `json:"target_type,omitempty"`
	TargetID       *int64          `json:"target_id,omitempty"`
	TargetCode     *string         `json:"target_code,omitempty"`
	CorrelationID  *string         `json:"correlation_id,omitempty"`
	RequestID      *string         `json:"request_id,omitempty"`
	IPHash         *string         `json:"ip_hash,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	Before         json.RawMessage `json:"before_snapshot,omitempty"`
	After          json.RawMessage `json:"after_snapshot,omitempty"`
	RetentionUntil time.Time       `json:"retention_until"`
}

func scanAudit(row interface{ Scan(...any) error }) (AuditLog, error) {
	var a AuditLog
	err := row.Scan(&a.ID, &a.OccurredAt, &a.ActorType, &a.ActorUserID, &a.ActorService, &a.Module, &a.ActionCode, &a.Outcome, &a.TargetType, &a.TargetID, &a.TargetCode, &a.CorrelationID, &a.RequestID, &a.IPHash, &a.Metadata, &a.Before, &a.After, &a.RetentionUntil, &a.ActorName, &a.TargetLabel)
	return a, err
}

const actorNameSQL = `COALESCE(NULLIF(sp.full_name,''),NULLIF(pa.full_name,''),NULLIF(u.username,''),NULLIF(a.actor_service,''),CASE WHEN a.actor_type='SYSTEM' THEN 'Hệ thống' ELSE 'Người dùng lịch sử' END)`
const actorJoinsSQL = ` LEFT JOIN users u ON u.id=a.actor_user_id LEFT JOIN staff_profiles sp ON sp.user_id=u.id LEFT JOIN patients pa ON pa.user_id=u.id `

// Business labels exclude clinical content, contact data, bank numbers and credentials.
const targetLabelSQL = `COALESCE(NULLIF(a.target_code,''), CASE a.target_type
 WHEN 'USER' THEN (SELECT COALESCE(s.full_name,p.full_name,x.username) FROM users x LEFT JOIN staff_profiles s ON s.user_id=x.id LEFT JOIN patients p ON p.user_id=x.id WHERE x.id=a.target_id)
 WHEN 'PATIENT' THEN (SELECT patient_code||' · '||full_name FROM patients WHERE id=a.target_id)
 WHEN 'STAFF_PROFILE' THEN (SELECT staff_code||' · '||full_name FROM staff_profiles WHERE id=a.target_id)
 WHEN 'DOCTOR_PROFILE' THEN (SELECT d.doctor_code||' · '||s.full_name FROM doctor_profiles d JOIN staff_profiles s ON s.id=d.staff_profile_id WHERE d.id=a.target_id)
 WHEN 'DOCTOR_SCHEDULE' THEN (SELECT d.doctor_code||' · '||to_char(x.start_at AT TIME ZONE 'Asia/Ho_Chi_Minh','DD/MM/YYYY HH24:MI') FROM doctor_schedules x JOIN doctor_profiles d ON d.id=x.doctor_profile_id WHERE x.id=a.target_id)
 WHEN 'DOCTOR_SPECIALTY' THEN (SELECT d.doctor_code||' · '||s.name FROM doctor_specialties x JOIN doctor_profiles d ON d.id=x.doctor_profile_id JOIN specialties s ON s.id=x.specialty_id WHERE x.id=a.target_id)
 WHEN 'ENCOUNTER_SERVICE' THEN (SELECT e.encounter_code||' · '||x.service_code_at_order FROM encounter_services x JOIN medical_encounters e ON e.id=x.encounter_id WHERE x.id=a.target_id)
 WHEN 'ENCOUNTER_VITAL_SIGN' THEN (SELECT e.encounter_code||' · Sinh hiệu' FROM encounter_vital_signs x JOIN medical_encounters e ON e.id=x.encounter_id WHERE x.id=a.target_id)
 WHEN 'ENCOUNTER_AMENDMENT' THEN (SELECT e.encounter_code||' · Bổ sung' FROM medical_encounter_amendments x JOIN medical_encounters e ON e.id=x.encounter_id WHERE x.id=a.target_id)
 WHEN 'PRESCRIPTION_ITEM' THEN (SELECT p.version_code||' · '||x.medicine_code_at_prescription FROM prescription_items x JOIN prescriptions p ON p.id=x.prescription_id WHERE x.id=a.target_id)
 WHEN 'SPECIALTY' THEN (SELECT code||' · '||name FROM specialties WHERE id=a.target_id)
 WHEN 'ROOM' THEN (SELECT code||' · '||name FROM rooms WHERE id=a.target_id)
 WHEN 'MEDICINE' THEN (SELECT code||' · '||name FROM medicines WHERE id=a.target_id)
 WHEN 'SERVICE' THEN (SELECT code||' · '||name FROM services WHERE id=a.target_id)
 WHEN 'APPOINTMENT' THEN (SELECT appointment_code FROM appointments WHERE id=a.target_id)
 WHEN 'ENCOUNTER' THEN (SELECT encounter_code FROM medical_encounters WHERE id=a.target_id)
 WHEN 'PRESCRIPTION_SERIES' THEN (SELECT prescription_code FROM prescription_series WHERE id=a.target_id)
 WHEN 'PRESCRIPTION' THEN (SELECT version_code FROM prescriptions WHERE id=a.target_id)
 WHEN 'STOCK_RECEIPT' THEN (SELECT receipt_code FROM stock_receipts WHERE id=a.target_id)
 WHEN 'INVENTORY_ADJUSTMENT' THEN (SELECT adjustment_code FROM inventory_adjustments WHERE id=a.target_id)
 WHEN 'INVOICE' THEN (SELECT invoice_code FROM invoices WHERE id=a.target_id)
 WHEN 'PAYMENT' THEN (SELECT payment_code FROM payments WHERE id=a.target_id)
 WHEN 'SYSTEM_SETTING' THEN (SELECT setting_key FROM system_settings WHERE id=a.target_id)
 WHEN 'BANK_ACCOUNT' THEN (SELECT bank_name FROM bank_accounts WHERE id=a.target_id)
 WHEN 'MEDICINE_PRICE_VERSION' THEN (SELECT m.code||' · '||to_char(x.effective_from AT TIME ZONE 'Asia/Ho_Chi_Minh','DD/MM/YYYY HH24:MI') FROM medicine_price_versions x JOIN medicines m ON m.id=x.medicine_id WHERE x.id=a.target_id)
 WHEN 'SERVICE_PRICE_VERSION' THEN (SELECT m.code||' · '||to_char(x.effective_from AT TIME ZONE 'Asia/Ho_Chi_Minh','DD/MM/YYYY HH24:MI') FROM service_price_versions x JOIN services m ON m.id=x.service_id WHERE x.id=a.target_id)
 END, 'Đối tượng lịch sử '||COALESCE(a.target_id::text,''))`
const auditSelect = `SELECT a.id,a.occurred_at,a.actor_type::text,a.actor_user_id,a.actor_service,a.module,a.action_code,a.outcome::text,a.target_type,a.target_id,a.target_code,a.correlation_id::text,a.request_id::text,a.ip_hash,a.metadata,a.before_snapshot,a.after_snapshot,a.retention_until,` + actorNameSQL + `,` + targetLabelSQL + ` FROM audit_logs a ` + actorJoinsSQL

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func parseCursor(v string) (time.Time, int64, error) {
	if v == "" {
		return time.Time{}, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return time.Time{}, 0, err
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 {
		return time.Time{}, 0, errors.New("cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0, err
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	return t, id, err
}
func makeCursor(t time.Time, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.Format(time.RFC3339Nano) + "|" + strconv.FormatInt(id, 10)))
}
func (s *Service) AuditLogs(ctx context.Context, f AuditFilter) ([]AuditLog, string, error) {
	ct, cid, err := parseCursor(f.Cursor)
	if err != nil {
		return nil, "", problem.New(422, "AUDIT_CURSOR_INVALID", "Cursor audit không hợp lệ.")
	}
	if f.PageSize < 1 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	if f.To.IsZero() {
		f.To = s.now()
	}
	if f.From.IsZero() {
		f.From = f.To.AddDate(0, 0, -30)
	}
	if f.To.Before(f.From) || f.To.Sub(f.From) > 366*24*time.Hour {
		return nil, "", problem.New(422, "AUDIT_FILTER_INVALID", "Khoảng ngày audit không hợp lệ.")
	}
	for _, v := range []string{f.Module, f.Action, f.Outcome, f.TargetType} {
		if len(v) > 150 {
			return nil, "", problem.New(422, "AUDIT_FILTER_INVALID", "Bộ lọc audit không hợp lệ.")
		}
	}
	if f.CorrelationID != "" && !uuidPattern.MatchString(f.CorrelationID) {
		return nil, "", problem.New(422, "AUDIT_FILTER_INVALID", "Correlation ID không hợp lệ.")
	}
	rows, err := s.pool.Query(ctx, auditSelect+` WHERE a.occurred_at >= $1 AND a.occurred_at <= $2 AND ($3='' OR a.module=$3) AND ($4='' OR a.action_code=$4) AND ($5='' OR a.outcome::text=$5) AND ($6=0 OR a.actor_user_id=$6) AND ($7='' OR a.target_type=$7) AND ($8=0 OR a.target_id=$8) AND ($9='' OR a.correlation_id::text=$9) AND ($10::timestamptz IS NULL OR (a.occurred_at,a.id)<($10,$11)) ORDER BY a.occurred_at DESC,a.id DESC LIMIT $12`, f.From, f.To, strings.ToUpper(f.Module), strings.ToUpper(f.Action), strings.ToUpper(f.Outcome), f.ActorUserID, strings.ToUpper(f.TargetType), f.TargetID, strings.ToLower(f.CorrelationID), func() any {
		if ct.IsZero() {
			return nil
		}
		return ct
	}(), cid, f.PageSize+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []AuditLog{}
	for rows.Next() {
		a, e := scanAudit(rows)
		if e != nil {
			return nil, "", e
		}
		items = append(items, a)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > f.PageSize {
		items = items[:f.PageSize]
		last := items[len(items)-1]
		next = makeCursor(last.OccurredAt, last.ID)
	}
	return items, next, nil
}
func (s *Service) AuditLog(ctx context.Context, id int64) (AuditLog, error) {
	a, err := scanAudit(s.pool.QueryRow(ctx, auditSelect+` WHERE a.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, problem.New(404, "AUDIT_LOG_NOT_FOUND", "Không tìm thấy audit log.")
	}
	return a, err
}

func SortedAppointmentKeys() []string {
	out := append([]string(nil), appointmentKeys...)
	sort.Strings(out)
	return out
}
