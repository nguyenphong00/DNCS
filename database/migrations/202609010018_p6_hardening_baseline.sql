-- P6 security baseline. These are group roles only; environment-specific LOGIN
-- roles are provisioned by the DBA and granted membership separately.
DO $$
BEGIN
    BEGIN
        CREATE ROLE dncs_owner NOLOGIN NOINHERIT;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        CREATE ROLE dncs_migrator NOLOGIN NOINHERIT;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        CREATE ROLE dncs_app NOLOGIN NOINHERIT;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        CREATE ROLE dncs_audit_reader NOLOGIN NOINHERIT;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
END;
$$;

GRANT dncs_owner TO dncs_migrator;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO dncs_app, dncs_audit_reader;
GRANT USAGE, CREATE ON SCHEMA public TO dncs_migrator;

GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO dncs_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO dncs_app;
REVOKE DELETE ON ALL TABLES IN SCHEMA public FROM dncs_app;
REVOKE ALL PRIVILEGES ON TABLE schema_migrations FROM dncs_app;
REVOKE INSERT, UPDATE ON TABLE
    roles,
    permissions,
    role_permissions,
    user_permission_overrides,
    gender_codes
FROM dncs_app;
REVOKE UPDATE ON TABLE
    user_role_histories,
    audit_logs,
    appointment_status_histories,
    medical_encounter_status_histories,
    medical_encounter_amendments,
    stock_receipts,
    stock_receipt_items,
    inventory_adjustments,
    inventory_adjustment_items,
    stock_movements,
    invoice_items
FROM dncs_app;

GRANT SELECT ON TABLE audit_logs TO dncs_audit_reader;

GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO dncs_migrator;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO dncs_migrator;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO dncs_migrator;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE ON TABLES TO dncs_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO dncs_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT ALL PRIVILEGES ON TABLES TO dncs_migrator;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT ALL PRIVILEGES ON SEQUENCES TO dncs_migrator;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT EXECUTE ON FUNCTIONS TO dncs_migrator;

CREATE INDEX ix_audit_logs_request_id
    ON audit_logs(request_id) WHERE request_id IS NOT NULL;
CREATE INDEX ix_audit_logs_correlation_id
    ON audit_logs(correlation_id, occurred_at DESC, id DESC) WHERE correlation_id IS NOT NULL;

COMMENT ON SCHEMA public IS
    'DNCS MVP-BL-01 / P6 hardening baseline / PostgreSQL 16 / 2026-09-01';
COMMENT ON TABLE audit_logs IS
    'Append-only seven-year audit trail; snapshots are size-limited and API projections are redacted.';
COMMENT ON COLUMN audit_logs.request_id IS
    'Server-validated UUID for one HTTP request; copied from request context.';
COMMENT ON COLUMN audit_logs.correlation_id IS
    'Server-validated UUID used to correlate related requests; defaults to request_id.';
COMMENT ON COLUMN audit_logs.ip_hash IS
    'HMAC-SHA256 of the direct peer IP using the environment audit hash key; raw IP is not stored here.';
