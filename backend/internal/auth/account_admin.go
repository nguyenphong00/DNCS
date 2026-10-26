package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dncs/backend/internal/audit"
	"github.com/jackc/pgx/v5"
)

type AccountAdminInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type AccountAdminResult struct {
	UserID             int64       `json:"user_id"`
	AccountType        AccountType `json:"account_type"`
	RoleCode           string      `json:"role_code"`
	Status             string      `json:"status"`
	MustChangePassword bool        `json:"must_change_password"`
	Version            int64       `json:"version"`
	TemporaryPassword  string      `json:"temporary_password,omitempty"`
}

type accountAdminRow struct {
	AccountAdminResult
	IsDeleted bool
}

func (service *Service) ResetPassword(ctx context.Context, actor Principal, userID int64, input AccountAdminInput) (AccountAdminResult, error) {
	if err := validateAccountAdminInput(actor, userID, input); err != nil {
		return AccountAdminResult{}, err
	}
	temporaryPassword, err := GenerateTemporaryPassword()
	if err != nil {
		return AccountAdminResult{}, err
	}
	passwordHash, err := service.passwords.Hash(temporaryPassword)
	if err != nil {
		return AccountAdminResult{}, err
	}

	tx, err := service.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AccountAdminResult{}, fmt.Errorf("begin password reset: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := lockAccountForAdmin(ctx, tx, actor, userID, input.ExpectedVersion)
	if err != nil {
		return AccountAdminResult{}, err
	}
	if current.IsDeleted {
		return AccountAdminResult{}, ErrAccountInvalidTransition
	}
	if _, err = tx.Exec(ctx, `
		UPDATE users SET password_hash=$2,must_change_password=true,token_version=token_version+1,
		password_changed_at=now(),updated_at=now(),version=version+1 WHERE id=$1`, userID, passwordHash); err != nil {
		return AccountAdminResult{}, fmt.Errorf("reset account password: %w", err)
	}
	if err := revokeAccountSessions(ctx, tx, userID, "ADMIN_PASSWORD_RESET"); err != nil {
		return AccountAdminResult{}, err
	}
	event := audit.UserEvent(actor.UserID, "AUTHENTICATION", "ACCOUNT_PASSWORD_RESET", "USER", userID)
	event.Metadata = map[string]any{"reason": strings.TrimSpace(input.Reason), "revoked_all_sessions": true}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return AccountAdminResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountAdminResult{}, fmt.Errorf("commit password reset: %w", err)
	}
	result, err := service.loadAccountAdminResult(ctx, userID)
	result.TemporaryPassword = temporaryPassword
	return result, err
}

func (service *Service) LockAccount(ctx context.Context, actor Principal, userID int64, input AccountAdminInput) (AccountAdminResult, error) {
	return service.changeAccountStatus(ctx, actor, userID, input, "LOCKED", "ACCOUNT_LOCKED", "ADMIN_ACCOUNT_LOCKED")
}

func (service *Service) UnlockAccount(ctx context.Context, actor Principal, userID int64, input AccountAdminInput) (AccountAdminResult, error) {
	return service.changeAccountStatus(ctx, actor, userID, input, "ACTIVE", "ACCOUNT_UNLOCKED", "")
}

func (service *Service) DeactivateAccount(ctx context.Context, actor Principal, userID int64, input AccountAdminInput) (AccountAdminResult, error) {
	return service.changeAccountStatus(ctx, actor, userID, input, "INACTIVE", "ACCOUNT_DEACTIVATED", "ADMIN_ACCOUNT_DEACTIVATED")
}

func (service *Service) changeAccountStatus(ctx context.Context, actor Principal, userID int64, input AccountAdminInput, target, actionCode, revokeReason string) (AccountAdminResult, error) {
	if err := validateAccountAdminInput(actor, userID, input); err != nil {
		return AccountAdminResult{}, err
	}
	tx, err := service.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AccountAdminResult{}, fmt.Errorf("begin account status change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := lockAccountForAdmin(ctx, tx, actor, userID, input.ExpectedVersion)
	if err != nil {
		return AccountAdminResult{}, err
	}
	if current.IsDeleted {
		return AccountAdminResult{}, ErrAccountInvalidTransition
	}
	if current.Status == target {
		return current.AccountAdminResult, nil
	}
	if target == "ACTIVE" && current.Status != "LOCKED" {
		return AccountAdminResult{}, ErrAccountInvalidTransition
	}
	tokenExpression := "token_version"
	if revokeReason != "" {
		tokenExpression = "token_version+1"
	}
	query := fmt.Sprintf(`UPDATE users SET status=$2,failed_login_count=0,locked_until=NULL,token_version=%s,updated_at=now(),version=version+1 WHERE id=$1`, tokenExpression)
	if _, err := tx.Exec(ctx, query, userID, target); err != nil {
		return AccountAdminResult{}, fmt.Errorf("change account status: %w", err)
	}
	if revokeReason != "" {
		if err := revokeAccountSessions(ctx, tx, userID, revokeReason); err != nil {
			return AccountAdminResult{}, err
		}
	}
	event := audit.UserEvent(actor.UserID, "AUTHENTICATION", actionCode, "USER", userID)
	event.Metadata = map[string]any{"from_status": current.Status, "to_status": target, "reason": strings.TrimSpace(input.Reason)}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return AccountAdminResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountAdminResult{}, fmt.Errorf("commit account status change: %w", err)
	}
	return service.loadAccountAdminResult(ctx, userID)
}

func validateAccountAdminInput(actor Principal, userID int64, input AccountAdminInput) error {
	reason := strings.TrimSpace(input.Reason)
	if userID < 1 || input.ExpectedVersion < 1 || reason == "" || len([]rune(reason)) > 500 {
		return ErrAccountAdminInput
	}
	if actor.UserID == userID {
		return ErrAccountSelfAdmin
	}
	return nil
}

func lockAccountForAdmin(ctx context.Context, tx pgx.Tx, actor Principal, userID, expectedVersion int64) (accountAdminRow, error) {
	var row accountAdminRow
	err := tx.QueryRow(ctx, `
		SELECT u.id,u.account_type::text,r.code,u.status::text,u.must_change_password,u.version,u.is_deleted
		FROM users u JOIN roles r ON r.id=u.role_id WHERE u.id=$1 FOR UPDATE OF u`, userID).Scan(
		&row.UserID, &row.AccountType, &row.RoleCode, &row.Status, &row.MustChangePassword, &row.Version, &row.IsDeleted,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return accountAdminRow{}, ErrAccountNotFound
	}
	if err != nil {
		return accountAdminRow{}, fmt.Errorf("lock account: %w", err)
	}
	if row.RoleCode == "SUPER_ADMIN" && actor.RoleCode != "SUPER_ADMIN" {
		return accountAdminRow{}, ErrAccountProtected
	}
	if row.Version != expectedVersion {
		return accountAdminRow{}, ErrAccountConcurrentUpdate
	}
	return row, nil
}

func revokeAccountSessions(ctx context.Context, tx pgx.Tx, userID int64, reason string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=now(),revoke_reason=$2,updated_at=now()
		WHERE user_id=$1 AND revoked_at IS NULL`, userID, reason); err != nil {
		return fmt.Errorf("revoke account sessions: %w", err)
	}
	return nil
}

func (service *Service) loadAccountAdminResult(ctx context.Context, userID int64) (AccountAdminResult, error) {
	var result AccountAdminResult
	err := service.store.pool.QueryRow(ctx, `
		SELECT u.id,u.account_type::text,r.code,u.status::text,u.must_change_password,u.version
		FROM users u JOIN roles r ON r.id=u.role_id WHERE u.id=$1`, userID).Scan(
		&result.UserID, &result.AccountType, &result.RoleCode, &result.Status, &result.MustChangePassword, &result.Version,
	)
	if err != nil {
		return AccountAdminResult{}, fmt.Errorf("load account result: %w", err)
	}
	return result, nil
}
