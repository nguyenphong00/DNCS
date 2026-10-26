package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type ServiceConfig struct {
	StaffRefreshTTL   time.Duration
	PatientRefreshTTL time.Duration
	MaxLoginFailures  int
	LoginLockDuration time.Duration
}

type Service struct {
	store     *Store
	tokens    TokenManager
	passwords PasswordManager
	config    ServiceConfig
	dummyHash string
	now       func() time.Time
}

func NewService(store *Store, tokens TokenManager, passwords PasswordManager, config ServiceConfig) (*Service, error) {
	dummyHash, err := passwords.Hash("invalid-credential-sentinel")
	if err != nil {
		return nil, fmt.Errorf("create credential sentinel: %w", err)
	}
	return &Service{
		store: store, tokens: tokens, passwords: passwords, config: config,
		dummyHash: dummyHash, now: time.Now,
	}, nil
}

func (service *Service) Login(
	ctx context.Context,
	accountType AccountType,
	identifier string,
	password string,
	client ClientInfo,
) (TokenPair, error) {
	normalized, err := normalizeIdentifier(accountType, identifier)
	if err != nil || password == "" || len(password) > 1024 {
		service.passwords.Matches(service.dummyHash, password)
		return TokenPair{}, ErrInvalidCredentials
	}

	tx, err := service.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return TokenPair{}, fmt.Errorf("begin login: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := loadLoginUser(ctx, tx, accountType, normalized)
	if errors.Is(err, pgx.ErrNoRows) {
		service.passwords.Matches(service.dummyHash, password)
		return TokenPair{}, ErrInvalidCredentials
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("load login account: %w", err)
	}

	now := service.now().UTC()
	temporaryLockExpired := user.Status == "LOCKED" && user.LockedUntil != nil && !user.LockedUntil.After(now)
	passwordMatches := service.passwords.Matches(user.PasswordHash, password)
	if !passwordMatches {
		if user.Status == "ACTIVE" || temporaryLockExpired {
			failedCount := user.FailedLoginCount + 1
			if temporaryLockExpired {
				failedCount = 1
			}
			if failedCount >= service.config.MaxLoginFailures {
				_, err = tx.Exec(ctx, `
					UPDATE users SET failed_login_count=$2, status='LOCKED', locked_until=$3,
					updated_at=$4, version=version+1 WHERE id=$1`,
					user.ID, failedCount, now.Add(service.config.LoginLockDuration), now,
				)
			} else {
				_, err = tx.Exec(ctx, `
					UPDATE users SET failed_login_count=$2, status='ACTIVE', locked_until=NULL,
					updated_at=$3, version=version+1 WHERE id=$1`, user.ID, failedCount, now,
				)
			}
			if err != nil {
				return TokenPair{}, fmt.Errorf("record failed login: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return TokenPair{}, fmt.Errorf("commit failed login: %w", err)
			}
		}
		return TokenPair{}, ErrInvalidCredentials
	}

	if user.IsDeleted {
		return TokenPair{}, ErrInvalidCredentials
	}
	if user.Status == "INACTIVE" || user.RoleStatus != "ACTIVE" || user.RoleDeleted {
		return TokenPair{}, ErrAccountInactive
	}
	if user.Status == "LOCKED" && !temporaryLockExpired {
		return TokenPair{}, ErrAccountLocked
	}

	rawRefresh, refreshHash, err := GenerateRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	refreshExpiresAt := now.Add(service.refreshTTL(accountType))
	var sessionID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO auth_sessions(
			user_id, refresh_token_hash, issued_at, expires_at, ip_address, user_agent, updated_at
		) VALUES($1,$2,$3,$4,$5,$6,$3) RETURNING id`,
		user.ID, refreshHash, now, refreshExpiresAt, nullableString(client.IPAddress),
		nullableString(truncate(client.UserAgent, 1000)),
	).Scan(&sessionID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("create authentication session: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE users SET status='ACTIVE', failed_login_count=0, locked_until=NULL,
		       last_login_at=$2, updated_at=$2, version=version+1 WHERE id=$1`, user.ID, now)
	if err != nil {
		return TokenPair{}, fmt.Errorf("update successful login: %w", err)
	}
	accessToken, accessExpiresAt, err := service.tokens.IssueAccess(user.ID, sessionID, user.TokenVersion)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, fmt.Errorf("commit login: %w", err)
	}
	return TokenPair{
		AccessToken: accessToken, RefreshToken: rawRefresh, TokenType: "Bearer",
		AccessExpiresAt: accessExpiresAt, RefreshExpiresAt: refreshExpiresAt,
		MustChange: user.MustChangePassword,
	}, nil
}

func (service *Service) Refresh(ctx context.Context, rawRefresh string) (TokenPair, error) {
	if err := ValidateRefreshToken(rawRefresh); err != nil {
		return TokenPair{}, err
	}
	tx, err := service.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return TokenPair{}, fmt.Errorf("begin token refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	refreshHash := HashRefreshToken(rawRefresh)
	var refreshUserID int64
	err = tx.QueryRow(ctx, `SELECT user_id FROM auth_sessions WHERE refresh_token_hash=$1`, refreshHash).Scan(&refreshUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenPair{}, ErrSessionInvalid
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("resolve refresh session owner: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, refreshUserID).Scan(&refreshUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TokenPair{}, ErrSessionInvalid
		}
		return TokenPair{}, fmt.Errorf("lock refresh session owner: %w", err)
	}
	session, err := loadRefreshSession(ctx, tx, refreshHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenPair{}, ErrSessionInvalid
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("load refresh session: %w", err)
	}
	now := service.now().UTC()
	if session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return TokenPair{}, ErrSessionInvalid
	}
	if session.UserDeleted || session.Status != "ACTIVE" || session.RoleDeleted || session.RoleStatus != "ACTIVE" {
		return TokenPair{}, ErrAccountInactive
	}

	newRefresh, newHash, err := GenerateRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE auth_sessions SET refresh_token_hash=$2, updated_at=$3
		WHERE id=$1 AND refresh_token_hash=$4 AND revoked_at IS NULL`,
		session.SessionID, newHash, now, refreshHash,
	)
	if err != nil {
		return TokenPair{}, fmt.Errorf("rotate refresh token: %w", err)
	}
	if command.RowsAffected() != 1 {
		return TokenPair{}, ErrSessionInvalid
	}
	accessToken, accessExpiresAt, err := service.tokens.IssueAccess(
		session.UserID, session.SessionID, session.TokenVersion,
	)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, fmt.Errorf("commit token refresh: %w", err)
	}
	return TokenPair{
		AccessToken: accessToken, RefreshToken: newRefresh, TokenType: "Bearer",
		AccessExpiresAt: accessExpiresAt, RefreshExpiresAt: session.ExpiresAt,
		MustChange: session.MustChangePassword,
	}, nil
}

func (service *Service) Logout(ctx context.Context, rawRefresh string) error {
	if err := ValidateRefreshToken(rawRefresh); err != nil {
		return nil
	}
	_, err := service.store.pool.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=now(), revoke_reason='USER_LOGOUT', updated_at=now()
		WHERE refresh_token_hash=$1 AND revoked_at IS NULL`, HashRefreshToken(rawRefresh))
	if err != nil {
		return fmt.Errorf("revoke current session: %w", err)
	}
	return nil
}

// StartSessionForUser is an internal post-registration operation. Callers must
// have created and validated the user aggregate before invoking it.
func (service *Service) StartSessionForUser(ctx context.Context, userID int64, client ClientInfo) (TokenPair, error) {
	tx, err := service.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TokenPair{}, fmt.Errorf("begin trusted session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var accountType AccountType
	var tokenVersion int64
	var mustChange bool
	err = tx.QueryRow(ctx, `
		SELECT u.account_type::text, u.token_version, u.must_change_password
		FROM users u JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1 AND u.status='ACTIVE' AND u.is_deleted=false
		  AND r.status='ACTIVE' AND r.is_deleted=false
		FOR UPDATE OF u`, userID).Scan(&accountType, &tokenVersion, &mustChange)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenPair{}, ErrAccountInactive
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("load trusted session user: %w", err)
	}
	rawRefresh, refreshHash, err := GenerateRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	now := service.now().UTC()
	refreshExpiresAt := now.Add(service.refreshTTL(accountType))
	var sessionID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO auth_sessions(
			user_id,refresh_token_hash,issued_at,expires_at,ip_address,user_agent,updated_at
		) VALUES($1,$2,$3,$4,$5,$6,$3) RETURNING id`,
		userID, refreshHash, now, refreshExpiresAt, nullableString(client.IPAddress),
		nullableString(truncate(client.UserAgent, 1000)),
	).Scan(&sessionID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("create trusted session: %w", err)
	}
	accessToken, accessExpiresAt, err := service.tokens.IssueAccess(userID, sessionID, tokenVersion)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, fmt.Errorf("commit trusted session: %w", err)
	}
	return TokenPair{
		AccessToken: accessToken, RefreshToken: rawRefresh, TokenType: "Bearer",
		AccessExpiresAt: accessExpiresAt, RefreshExpiresAt: refreshExpiresAt,
		MustChange: mustChange,
	}, nil
}

func (service *Service) LogoutAll(ctx context.Context, principal Principal) error {
	tx, err := service.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin logout all: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := service.now().UTC()
	var lockedUserID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, principal.UserID).Scan(&lockedUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionInvalid
		}
		return fmt.Errorf("lock user for logout all: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=$2, revoke_reason='USER_LOGOUT_ALL', updated_at=$2
		WHERE user_id=$1 AND revoked_at IS NULL`, principal.UserID, now); err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET token_version=token_version+1, updated_at=$2, version=version+1
		WHERE id=$1`, principal.UserID, now); err != nil {
		return fmt.Errorf("invalidate access tokens: %w", err)
	}
	if err := insertAudit(ctx, tx, principal.UserID, "LOGOUT_ALL", "USER", principal.UserID, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit logout all: %w", err)
	}
	return nil
}

func (service *Service) ChangePassword(
	ctx context.Context,
	principal Principal,
	currentPassword string,
	newPassword string,
	client ClientInfo,
) (TokenPair, error) {
	if !validPasswordLength(newPassword) {
		return TokenPair{}, ErrPasswordPolicy
	}
	tx, err := service.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TokenPair{}, fmt.Errorf("begin password change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentHash string
	var accountType AccountType
	var tokenVersion int64
	err = tx.QueryRow(ctx, `
		SELECT password_hash, account_type::text, token_version
		FROM users WHERE id=$1 AND status='ACTIVE' AND is_deleted=false FOR UPDATE`,
		principal.UserID,
	).Scan(&currentHash, &accountType, &tokenVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenPair{}, ErrSessionInvalid
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("load password account: %w", err)
	}
	if !service.passwords.Matches(currentHash, currentPassword) {
		return TokenPair{}, ErrCurrentPasswordInvalid
	}
	if service.passwords.Matches(currentHash, newPassword) {
		return TokenPair{}, ErrPasswordPolicy
	}
	newHash, err := service.passwords.Hash(newPassword)
	if err != nil {
		return TokenPair{}, err
	}
	now := service.now().UTC()
	newTokenVersion := tokenVersion + 1
	if _, err := tx.Exec(ctx, `
		UPDATE users SET password_hash=$2, must_change_password=false,
		       token_version=$3, password_changed_at=$4, updated_at=$4, version=version+1
		WHERE id=$1`, principal.UserID, newHash, newTokenVersion, now); err != nil {
		return TokenPair{}, fmt.Errorf("change password: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at=$2, revoke_reason='PASSWORD_CHANGED', updated_at=$2
		WHERE user_id=$1 AND revoked_at IS NULL`, principal.UserID, now); err != nil {
		return TokenPair{}, fmt.Errorf("revoke sessions after password change: %w", err)
	}

	rawRefresh, refreshHash, err := GenerateRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	refreshExpiresAt := now.Add(service.refreshTTL(accountType))
	var sessionID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO auth_sessions(
			user_id, refresh_token_hash, issued_at, expires_at, ip_address, user_agent, updated_at
		) VALUES($1,$2,$3,$4,$5,$6,$3) RETURNING id`,
		principal.UserID, refreshHash, now, refreshExpiresAt, nullableString(client.IPAddress),
		nullableString(truncate(client.UserAgent, 1000)),
	).Scan(&sessionID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("create replacement session: %w", err)
	}
	metadata, _ := json.Marshal(map[string]any{"revoked_all_sessions": true})
	if err := insertAudit(ctx, tx, principal.UserID, "PASSWORD_CHANGED", "USER", principal.UserID, metadata); err != nil {
		return TokenPair{}, err
	}
	accessToken, accessExpiresAt, err := service.tokens.IssueAccess(principal.UserID, sessionID, newTokenVersion)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, fmt.Errorf("commit password change: %w", err)
	}
	return TokenPair{
		AccessToken: accessToken, RefreshToken: rawRefresh, TokenType: "Bearer",
		AccessExpiresAt: accessExpiresAt, RefreshExpiresAt: refreshExpiresAt,
		MustChange: false,
	}, nil
}

func normalizeIdentifier(accountType AccountType, value string) (string, error) {
	switch accountType {
	case AccountTypeStaff:
		return NormalizeStaffUsername(value)
	case AccountTypePatient:
		return NormalizeVietnamPhone(value)
	default:
		return "", ErrInvalidCredentials
	}
}

func (service *Service) refreshTTL(accountType AccountType) time.Duration {
	if accountType == AccountTypePatient {
		return service.config.PatientRefreshTTL
	}
	return service.config.StaffRefreshTTL
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func truncate(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}
