package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"dncs/backend/internal/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (store *Store) Pool() *pgxpool.Pool { return store.pool }

func (store *Store) LoadPrincipal(ctx context.Context, claims AccessClaims) (Principal, error) {
	userID, err := claims.UserID()
	if err != nil {
		return Principal{}, ErrSessionInvalid
	}
	sessionID, err := claims.SessionIDValue()
	if err != nil {
		return Principal{}, ErrSessionInvalid
	}
	var principal Principal
	err = store.pool.QueryRow(ctx, `
		SELECT u.id, s.id, u.role_id, r.code, u.account_type::text,
		       u.token_version, u.must_change_password
		FROM users u
		JOIN roles r ON r.id = u.role_id
		JOIN auth_sessions s ON s.user_id = u.id
		WHERE u.id = $1 AND s.id = $2
		  AND u.token_version = $3
		  AND u.status = 'ACTIVE' AND u.is_deleted = false
		  AND r.status = 'ACTIVE' AND r.is_deleted = false
		  AND s.revoked_at IS NULL AND s.expires_at > now()`,
		userID, sessionID, claims.TokenVersion,
	).Scan(
		&principal.UserID, &principal.SessionID, &principal.RoleID, &principal.RoleCode,
		&principal.AccountType, &principal.TokenVersion, &principal.MustChangePassword,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrSessionInvalid
	}
	if err != nil {
		return Principal{}, fmt.Errorf("load authenticated principal: %w", err)
	}
	return principal, nil
}

type loginUser struct {
	ID                 int64
	RoleID             int64
	PasswordHash       string
	Status             string
	MustChangePassword bool
	FailedLoginCount   int
	LockedUntil        *time.Time
	TokenVersion       int64
	IsDeleted          bool
	RoleCode           string
	RoleStatus         string
	RoleDeleted        bool
	AccountType        AccountType
}

func loadLoginUser(ctx context.Context, tx pgx.Tx, accountType AccountType, identifier string) (loginUser, error) {
	query := `
		SELECT u.id, u.role_id, u.password_hash, u.status::text, u.must_change_password,
		       u.failed_login_count, u.locked_until, u.token_version, u.is_deleted,
		       r.code, r.status::text, r.is_deleted, u.account_type::text
		FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.account_type = $1 AND u.normalized_username = $2
		FOR UPDATE OF u`
	if accountType == AccountTypePatient {
		query = `
			SELECT u.id, u.role_id, u.password_hash, u.status::text, u.must_change_password,
			       u.failed_login_count, u.locked_until, u.token_version, u.is_deleted,
			       r.code, r.status::text, r.is_deleted, u.account_type::text
			FROM users u JOIN roles r ON r.id = u.role_id
			WHERE u.account_type = $1 AND u.normalized_phone_number = $2
			FOR UPDATE OF u`
	}
	var user loginUser
	err := tx.QueryRow(ctx, query, accountType, identifier).Scan(
		&user.ID, &user.RoleID, &user.PasswordHash, &user.Status, &user.MustChangePassword,
		&user.FailedLoginCount, &user.LockedUntil, &user.TokenVersion, &user.IsDeleted,
		&user.RoleCode, &user.RoleStatus, &user.RoleDeleted, &user.AccountType,
	)
	return user, err
}

type refreshSession struct {
	SessionID          int64
	UserID             int64
	ExpiresAt          time.Time
	RevokedAt          *time.Time
	RoleID             int64
	AccountType        AccountType
	Status             string
	MustChangePassword bool
	TokenVersion       int64
	UserDeleted        bool
	RoleCode           string
	RoleStatus         string
	RoleDeleted        bool
}

func loadRefreshSession(ctx context.Context, tx pgx.Tx, refreshHash string) (refreshSession, error) {
	var session refreshSession
	err := tx.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.expires_at, s.revoked_at,
		       u.role_id, u.account_type::text, u.status::text, u.must_change_password,
		       u.token_version, u.is_deleted, r.code, r.status::text, r.is_deleted
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		JOIN roles r ON r.id = u.role_id
		WHERE s.refresh_token_hash = $1
		FOR UPDATE OF s`, refreshHash,
	).Scan(
		&session.SessionID, &session.UserID, &session.ExpiresAt, &session.RevokedAt,
		&session.RoleID, &session.AccountType, &session.Status, &session.MustChangePassword,
		&session.TokenVersion, &session.UserDeleted, &session.RoleCode, &session.RoleStatus,
		&session.RoleDeleted,
	)
	return session, err
}

func insertAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorUserID int64,
	actionCode string,
	targetType string,
	targetID int64,
	metadata []byte,
) error {
	event := audit.UserEvent(actorUserID, "AUTHENTICATION", actionCode, targetType, targetID)
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
			return fmt.Errorf("decode authentication audit metadata: %w", err)
		}
	}
	return audit.Insert(ctx, tx, event)
}
