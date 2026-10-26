package database_test

import (
	"context"
	"errors"
	"testing"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/platform/requestmeta"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestP6LeastPrivilegeRolesAndTraceIndexes(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	checks := []struct {
		role, table, privilege string
		want                   bool
	}{
		{"dncs_app", "users", "SELECT", true},
		{"dncs_app", "users", "INSERT", true},
		{"dncs_app", "users", "DELETE", false},
		{"dncs_app", "audit_logs", "INSERT", true},
		{"dncs_app", "audit_logs", "UPDATE", false},
		{"dncs_app", "schema_migrations", "SELECT", false},
		{"dncs_audit_reader", "audit_logs", "SELECT", true},
		{"dncs_audit_reader", "audit_logs", "INSERT", false},
		{"dncs_audit_reader", "users", "SELECT", false},
	}
	for _, check := range checks {
		var got bool
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege($1, $2, $3)`, check.role, check.table, check.privilege).Scan(&got); err != nil {
			t.Fatalf("check %s %s on %s: %v", check.role, check.privilege, check.table, err)
		}
		if got != check.want {
			t.Errorf("%s %s on %s: got %v want %v", check.role, check.privilege, check.table, got, check.want)
		}
	}

	var traceIndexes int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE schemaname='public'
		  AND indexname IN ('ix_audit_logs_request_id','ix_audit_logs_correlation_id')`).Scan(&traceIndexes); err != nil {
		t.Fatal(err)
	}
	if traceIndexes != 2 {
		t.Fatalf("trace index count=%d want 2", traceIndexes)
	}
	var baseline string
	if err := pool.QueryRow(ctx, `SELECT obj_description('public'::regnamespace, 'pg_namespace')`).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	if baseline != "DNCS MVP-BL-01 / P6 hardening baseline / PostgreSQL 16 / 2026-09-01" {
		t.Fatalf("unexpected schema baseline: %q", baseline)
	}
}

func TestP6ApplicationRoleCanAppendButCannotDelete(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE dncs_app`); err != nil {
		t.Fatalf("set application role: %v", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO audit_logs(
			occurred_at,actor_type,module,action_code,outcome,target_type,metadata,retention_until
		) VALUES(now(),'SYSTEM','AUTHENTICATION','P6_ROLE_PROBE','SUCCESS','SYSTEM','{}',now()+interval '7 years')`); err != nil {
		t.Fatalf("application role could not append audit: %v", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE dncs_app`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `DELETE FROM auth_sessions WHERE false`)
	var databaseError *pgconn.PgError
	if err == nil || !errors.As(err, &databaseError) || databaseError.Code != "42501" {
		t.Fatalf("generic DELETE was not privilege denied: %v", err)
	}
}

func TestP6AuditWriterPersistsRequestCorrelationAndHashedIP(t *testing.T) {
	f := newFixture(t)
	ctx := requestmeta.WithContext(context.Background(), requestmeta.Metadata{
		RequestID:     "550e8400-e29b-41d4-a716-446655440000",
		CorrelationID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		IPHash:        "4bd8f1ddcc316f9520493ef061eb615f749f738ebe08940dadaa8b78c2f4c67c",
	})
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	event := audit.UserEvent(f.admin.UserID, "AUTHENTICATION", "P6_TRACE_PROBE", "USER", f.admin.UserID)
	if err = audit.Insert(ctx, tx, event); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var requestID, correlationID, ipHash string
	if err = f.pool.QueryRow(context.Background(), `
		SELECT request_id::text,correlation_id::text,ip_hash
		FROM audit_logs
		WHERE actor_user_id=$1 AND action_code='P6_TRACE_PROBE'
		ORDER BY id DESC LIMIT 1`, f.admin.UserID).Scan(&requestID, &correlationID, &ipHash); err != nil {
		t.Fatal(err)
	}
	if requestID != "550e8400-e29b-41d4-a716-446655440000" || correlationID != "6ba7b810-9dad-11d1-80b4-00c04fd430c8" || ipHash == "" {
		t.Fatalf("persisted trace request=%q correlation=%q ip_hash=%q", requestID, correlationID, ipHash)
	}
}

func TestP6SensitiveAuditPayloadRollsBackBusinessMutation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var before int64
	if err := f.pool.QueryRow(ctx, `SELECT version FROM system_settings WHERE setting_key='inventory_expiry_alert_days'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE system_settings
		SET version=version+1,updated_by_user_id=$1,updated_at=now()
		WHERE setting_key='inventory_expiry_alert_days'`, f.admin.UserID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	event := audit.UserEvent(f.admin.UserID, "AUDIT_SETTINGS", "P6_REDACTION_PROBE", "SYSTEM_SETTING", 1)
	event.Metadata = map[string]any{"password": "must-never-be-persisted"}
	if err = audit.Insert(ctx, tx, event); err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("sensitive audit payload was accepted")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err = f.pool.QueryRow(ctx, `SELECT version FROM system_settings WHERE setting_key='inventory_expiry_alert_days'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("business mutation escaped failed audit transaction: before=%d after=%d", before, after)
	}
}
