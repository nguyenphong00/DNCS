package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const RequiredSchemaMigration int64 = 202610090020

const requiredSchemaBaseline = "DNCS MVP-BL-01 / P6 hardening baseline / PostgreSQL 16 / 2026-09-01"

var ErrSchemaNotReady = errors.New("database schema is not ready")

type readinessDatabase interface {
	Ping(context.Context) error
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CheckReadiness verifies both database connectivity and the schema baseline
// installed by migration 202609010018 plus the permission added in 202610090020.
// The baseline is read from
// pg_catalog because the least-privileged application role intentionally cannot
// SELECT from schema_migrations.
func CheckReadiness(ctx context.Context, database readinessDatabase) error {
	if err := database.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	var schemaReady bool
	if err := database.QueryRow(ctx, `
		SELECT COALESCE(
			obj_description('public'::regnamespace, 'pg_namespace') = $1 AND EXISTS (SELECT 1 FROM permissions WHERE code='billing.pending_encounter.view' AND status='ACTIVE' AND NOT is_deleted),
			false
		)`, requiredSchemaBaseline).Scan(&schemaReady); err != nil {
		return fmt.Errorf("check required schema migration %d: %w", RequiredSchemaMigration, err)
	}
	if !schemaReady {
		return fmt.Errorf("%w: required migration %d", ErrSchemaNotReady, RequiredSchemaMigration)
	}
	return nil
}
