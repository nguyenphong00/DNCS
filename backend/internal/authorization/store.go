package authorization

import (
	"context"
	"errors"
	"fmt"

	"dncs/backend/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Decision struct {
	Allowed bool
	Source  string
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (store *Store) Allowed(ctx context.Context, principal auth.Principal, permissionCode string) (Decision, error) {
	var decision Decision
	err := store.pool.QueryRow(ctx, `
		SELECT
			CASE
				WHEN $4 THEN true
				WHEN permission_override.effect = 'ALLOW' THEN true
				WHEN permission_override.effect = 'DENY' THEN false
				WHEN role_grant.permission_id IS NOT NULL THEN true
				ELSE false
			END AS allowed,
			CASE
				WHEN $4 THEN 'SUPER_ADMIN'
				WHEN permission_override.effect IS NOT NULL THEN 'USER_OVERRIDE'
				WHEN role_grant.permission_id IS NOT NULL THEN 'ROLE'
				ELSE 'DEFAULT_DENY'
			END AS source
		FROM permissions p
		LEFT JOIN LATERAL (
			SELECT upo.effect
			FROM user_permission_overrides upo
			WHERE upo.user_id = $1 AND upo.permission_id = p.id AND upo.is_deleted = false
			LIMIT 1
		) permission_override ON true
		LEFT JOIN role_permissions role_grant
			ON role_grant.role_id = $2 AND role_grant.permission_id = p.id
		WHERE p.code = $3 AND p.status = 'ACTIVE' AND p.is_deleted = false`,
		principal.UserID, principal.RoleID, permissionCode, principal.RoleCode == "SUPER_ADMIN",
	).Scan(&decision.Allowed, &decision.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return Decision{Allowed: false, Source: "UNKNOWN_PERMISSION"}, nil
	}
	if err != nil {
		return Decision{}, fmt.Errorf("evaluate permission %s: %w", permissionCode, err)
	}
	return decision, nil
}

func (store *Store) ListEffective(ctx context.Context, principal auth.Principal) ([]string, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT p.code
		FROM permissions p
		WHERE p.status = 'ACTIVE' AND p.is_deleted = false
		  AND (
			$3
			OR COALESCE(
				(
					SELECT upo.effect = 'ALLOW'
					FROM user_permission_overrides upo
					WHERE upo.user_id = $1 AND upo.permission_id = p.id AND upo.is_deleted = false
					LIMIT 1
				),
				EXISTS(
					SELECT 1 FROM role_permissions rp
					WHERE rp.role_id = $2 AND rp.permission_id = p.id
				)
			)
		  )
		ORDER BY p.code`, principal.UserID, principal.RoleID, principal.RoleCode == "SUPER_ADMIN")
	if err != nil {
		return nil, fmt.Errorf("list effective permissions: %w", err)
	}
	defer rows.Close()
	permissions := make([]string, 0)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan effective permission: %w", err)
		}
		permissions = append(permissions, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate effective permissions: %w", err)
	}
	return permissions, nil
}
