package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/textnorm"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const bootstrapLockID int64 = 211420260902

var (
	ErrAlreadyBootstrapped = errors.New("a super-admin account already exists")
	ErrUsernameTaken       = errors.New("bootstrap username is already in use")
	ErrBootstrapState      = errors.New("existing bootstrap account is not operational")
)

type Input struct {
	Username string
	FullName string
	Password string
}

type Result struct {
	UserID         int64
	StaffProfileID int64
	Username       string
	Created        bool
}

// SuperAdmin creates the one-time initial SUPER_ADMIN identity. Repeating the
// command with the same normalized username is safe and returns the existing
// aggregate without changing its password or profile.
func SuperAdmin(ctx context.Context, pool *pgxpool.Pool, passwords auth.PasswordManager, input Input) (Result, error) {
	username, err := auth.NormalizeStaffUsername(input.Username)
	if err != nil {
		return Result{}, fmt.Errorf("invalid bootstrap username: %w", err)
	}
	fullName, err := textnorm.HumanName(input.FullName, 150)
	if err != nil {
		return Result{}, fmt.Errorf("invalid bootstrap full name: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin super-admin bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockID); err != nil {
		return Result{}, fmt.Errorf("lock super-admin bootstrap: %w", err)
	}

	var roleID int64
	if err := tx.QueryRow(ctx, `
		SELECT id FROM roles
		WHERE code='SUPER_ADMIN' AND account_scope='STAFF' AND status='ACTIVE'
		  AND is_protected=true AND is_deleted=false`).Scan(&roleID); errors.Is(err, pgx.ErrNoRows) {
		return Result{}, errors.New("active protected SUPER_ADMIN role is unavailable; run migrations first")
	} else if err != nil {
		return Result{}, fmt.Errorf("load SUPER_ADMIN role: %w", err)
	}

	var existingUserID int64
	var existingRoleCode, existingAccountType, existingStatus string
	var existingUserDeleted bool
	var existingStaffID *int64
	var existingStaffDeleted *bool
	err = tx.QueryRow(ctx, `
		SELECT u.id,r.code,u.account_type::text,u.status::text,u.is_deleted,sp.id,sp.is_deleted
		FROM users u
		JOIN roles r ON r.id=u.role_id
		LEFT JOIN staff_profiles sp ON sp.user_id=u.id
		WHERE u.normalized_username=$1
		FOR UPDATE OF u`, username,
	).Scan(&existingUserID, &existingRoleCode, &existingAccountType, &existingStatus, &existingUserDeleted, &existingStaffID, &existingStaffDeleted)
	if err == nil {
		if existingRoleCode != "SUPER_ADMIN" || existingAccountType != "STAFF" {
			return Result{}, ErrUsernameTaken
		}
		if existingStatus != "ACTIVE" || existingUserDeleted || existingStaffID == nil || existingStaffDeleted == nil || *existingStaffDeleted {
			return Result{}, ErrBootstrapState
		}
		return Result{UserID: existingUserID, StaffProfileID: *existingStaffID, Username: username, Created: false}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, fmt.Errorf("check bootstrap username: %w", err)
	}

	var anotherSuperAdmin bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM users u JOIN roles r ON r.id=u.role_id
			WHERE r.code='SUPER_ADMIN'
		)`).Scan(&anotherSuperAdmin); err != nil {
		return Result{}, fmt.Errorf("check existing super-admin: %w", err)
	}
	if anotherSuperAdmin {
		return Result{}, ErrAlreadyBootstrapped
	}

	passwordHash, err := passwords.Hash(input.Password)
	if err != nil {
		return Result{}, fmt.Errorf("invalid bootstrap password: %w", err)
	}
	var userID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO users(
			role_id,account_type,username,normalized_username,password_hash,status,must_change_password
		) VALUES($1,'STAFF',$2,$3,$4,'ACTIVE',true)
		RETURNING id`, roleID, strings.TrimSpace(input.Username), username, passwordHash,
	).Scan(&userID); err != nil {
		return Result{}, fmt.Errorf("insert bootstrap user: %w", err)
	}
	var staffID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO staff_profiles(user_id,full_name)
		VALUES($1,$2) RETURNING id`, userID, fullName,
	).Scan(&staffID); err != nil {
		return Result{}, fmt.Errorf("insert bootstrap staff profile: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_role_histories(user_id,to_role_id,changed_by_user_id,reason)
		VALUES($1,$2,$1,'INITIAL_SUPER_ADMIN_BOOTSTRAP')`, userID, roleID); err != nil {
		return Result{}, fmt.Errorf("insert bootstrap role history: %w", err)
	}
	event := audit.SystemEvent("AUTHORIZATION", "SUPER_ADMIN_BOOTSTRAPPED", "USER", userID)
	event.Metadata = map[string]any{"staff_profile_id": staffID}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit super-admin bootstrap: %w", err)
	}
	return Result{UserID: userID, StaffProfileID: staffID, Username: username, Created: true}, nil
}
