-- Consolidated vertical-slice migration for P0 Authentication/Authorization.
CREATE TYPE account_type_enum AS ENUM ('STAFF', 'PATIENT');
CREATE TYPE user_status_enum AS ENUM ('ACTIVE', 'INACTIVE', 'LOCKED');
CREATE TYPE role_type_enum AS ENUM ('SYSTEM', 'CUSTOM');
CREATE TYPE account_scope_enum AS ENUM ('STAFF', 'PATIENT');
CREATE TYPE role_status_enum AS ENUM ('ACTIVE', 'INACTIVE');
CREATE TYPE permission_risk_enum AS ENUM ('NORMAL', 'SENSITIVE', 'PROTECTED');
CREATE TYPE permission_status_enum AS ENUM ('ACTIVE', 'INACTIVE');
CREATE TYPE permission_effect_enum AS ENUM ('ALLOW', 'DENY');
CREATE TYPE audit_actor_type_enum AS ENUM ('USER', 'SERVICE', 'SYSTEM');
CREATE TYPE audit_outcome_enum AS ENUM ('SUCCESS', 'FAILURE', 'DENIED');

CREATE TABLE roles (
    id bigint GENERATED ALWAYS AS IDENTITY,
    code varchar(64) NOT NULL,
    name varchar(150) NOT NULL,
    description varchar(1000),
    role_type role_type_enum NOT NULL,
    account_scope account_scope_enum NOT NULL,
    is_protected boolean NOT NULL DEFAULT false,
    status role_status_enum NOT NULL DEFAULT 'ACTIVE',
    created_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_roles PRIMARY KEY (id),
    CONSTRAINT uq_roles_code UNIQUE (code),
    CONSTRAINT ck_roles_code CHECK (code ~ '^[A-Z][A-Z0-9_]{1,63}$'),
    CONSTRAINT ck_roles_name CHECK (char_length(btrim(name)) BETWEEN 1 AND 150),
    CONSTRAINT ck_roles_version CHECK (version >= 1),
    CONSTRAINT ck_roles_protection CHECK (role_type = 'SYSTEM' OR is_protected = false)
);

CREATE TABLE permissions (
    id bigint GENERATED ALWAYS AS IDENTITY,
    code varchar(150) NOT NULL,
    module varchar(80) NOT NULL,
    name varchar(150) NOT NULL,
    description varchar(1000),
    risk_level permission_risk_enum NOT NULL DEFAULT 'NORMAL',
    status permission_status_enum NOT NULL DEFAULT 'ACTIVE',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_permissions PRIMARY KEY (id),
    CONSTRAINT uq_permissions_code UNIQUE (code),
    CONSTRAINT ck_permissions_code CHECK (code ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    CONSTRAINT ck_permissions_module CHECK (module ~ '^[A-Z][A-Z0-9_]{1,79}$'),
    CONSTRAINT ck_permissions_name CHECK (char_length(btrim(name)) BETWEEN 1 AND 150),
    CONSTRAINT ck_permissions_version CHECK (version >= 1)
);

CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY,
    role_id bigint NOT NULL,
    account_type account_type_enum NOT NULL,
    username varchar(100),
    normalized_username varchar(100),
    phone_number varchar(20),
    normalized_phone_number varchar(20),
    password_hash varchar(255) NOT NULL,
    status user_status_enum NOT NULL DEFAULT 'ACTIVE',
    must_change_password boolean NOT NULL DEFAULT false,
    failed_login_count integer NOT NULL DEFAULT 0,
    locked_until timestamptz,
    last_login_at timestamptz,
    password_changed_at timestamptz,
    token_version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_users PRIMARY KEY (id),
    CONSTRAINT fk_users_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE RESTRICT,
    CONSTRAINT uq_users_normalized_username UNIQUE (normalized_username),
    CONSTRAINT uq_users_normalized_phone UNIQUE (normalized_phone_number),
    CONSTRAINT ck_users_identifier_by_type CHECK (
        (account_type = 'STAFF' AND username IS NOT NULL AND normalized_username IS NOT NULL
            AND phone_number IS NULL AND normalized_phone_number IS NULL)
        OR
        (account_type = 'PATIENT' AND username IS NULL AND normalized_username IS NULL
            AND phone_number IS NOT NULL AND normalized_phone_number IS NOT NULL)
    ),
    CONSTRAINT ck_users_username CHECK (
        normalized_username IS NULL OR normalized_username ~ '^[a-z0-9][a-z0-9._-]{2,99}$'
    ),
    CONSTRAINT ck_users_phone_e164 CHECK (
        normalized_phone_number IS NULL OR normalized_phone_number ~ '^\+84[0-9]{9,10}$'
    ),
    CONSTRAINT ck_users_login_state CHECK (
        failed_login_count >= 0 AND token_version >= 1 AND version >= 1
    ),
    CONSTRAINT ck_users_password_hash CHECK (char_length(password_hash) BETWEEN 20 AND 255)
);

ALTER TABLE roles
    ADD CONSTRAINT fk_roles_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT;

CREATE TABLE auth_sessions (
    id bigint GENERATED ALWAYS AS IDENTITY,
    user_id bigint NOT NULL,
    refresh_token_hash varchar(255) NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason varchar(500),
    ip_address inet,
    user_agent varchar(1000),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_auth_sessions PRIMARY KEY (id),
    CONSTRAINT fk_auth_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT uq_auth_sessions_refresh_hash UNIQUE (refresh_token_hash),
    CONSTRAINT ck_auth_sessions_period CHECK (expires_at > issued_at),
    CONSTRAINT ck_auth_sessions_revocation CHECK (
        (revoked_at IS NULL AND revoke_reason IS NULL)
        OR
        (revoked_at IS NOT NULL AND char_length(btrim(revoke_reason)) BETWEEN 1 AND 500)
    )
);

CREATE TABLE role_permissions (
    role_id bigint NOT NULL,
    permission_id bigint NOT NULL,
    granted_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_role_permissions PRIMARY KEY (role_id, permission_id),
    CONSTRAINT fk_role_permissions_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_role_permissions_permission FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE RESTRICT,
    CONSTRAINT fk_role_permissions_granted_by FOREIGN KEY (granted_by) REFERENCES users(id) ON DELETE RESTRICT
);

CREATE TABLE user_permission_overrides (
    id bigint GENERATED ALWAYS AS IDENTITY,
    user_id bigint NOT NULL,
    permission_id bigint NOT NULL,
    effect permission_effect_enum NOT NULL,
    reason varchar(1000) NOT NULL,
    granted_by bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_user_permission_overrides PRIMARY KEY (id),
    CONSTRAINT fk_user_overrides_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_user_overrides_permission FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE RESTRICT,
    CONSTRAINT fk_user_overrides_granted_by FOREIGN KEY (granted_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_user_override_reason CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    CONSTRAINT ck_user_override_version CHECK (version >= 1)
);

CREATE TABLE user_role_histories (
    id bigint GENERATED ALWAYS AS IDENTITY,
    user_id bigint NOT NULL,
    from_role_id bigint,
    to_role_id bigint NOT NULL,
    changed_by_user_id bigint NOT NULL,
    reason varchar(1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_user_role_histories PRIMARY KEY (id),
    CONSTRAINT fk_user_role_history_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_user_role_history_from_role FOREIGN KEY (from_role_id) REFERENCES roles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_user_role_history_to_role FOREIGN KEY (to_role_id) REFERENCES roles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_user_role_history_changed_by FOREIGN KEY (changed_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_user_role_history_reason CHECK (
        reason IS NULL OR char_length(btrim(reason)) BETWEEN 1 AND 1000
    )
);

CREATE TABLE audit_logs (
    id bigint GENERATED ALWAYS AS IDENTITY,
    occurred_at timestamptz NOT NULL,
    actor_type audit_actor_type_enum NOT NULL,
    actor_user_id bigint,
    actor_service varchar(100),
    module varchar(80) NOT NULL,
    action_code varchar(150) NOT NULL,
    outcome audit_outcome_enum NOT NULL,
    target_type varchar(80),
    target_id bigint,
    target_code varchar(100),
    correlation_id uuid,
    request_id uuid,
    ip_hash varchar(128),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    before_snapshot jsonb,
    after_snapshot jsonb,
    retention_until timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_audit_logs PRIMARY KEY (id),
    CONSTRAINT fk_audit_logs_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_audit_logs_actor CHECK (
        (actor_type = 'USER' AND actor_user_id IS NOT NULL AND actor_service IS NULL)
        OR (actor_type = 'SERVICE' AND actor_user_id IS NULL AND char_length(btrim(actor_service)) BETWEEN 1 AND 100)
        OR (actor_type = 'SYSTEM' AND actor_user_id IS NULL AND actor_service IS NULL)
    ),
    CONSTRAINT ck_audit_logs_module CHECK (module ~ '^[A-Z][A-Z0-9_]{1,79}$'),
    CONSTRAINT ck_audit_logs_action CHECK (action_code ~ '^[A-Z][A-Z0-9_]{1,149}$'),
    CONSTRAINT ck_audit_logs_json_size CHECK (
        pg_column_size(metadata) <= 16384
        AND (before_snapshot IS NULL OR pg_column_size(before_snapshot) <= 16384)
        AND (after_snapshot IS NULL OR pg_column_size(after_snapshot) <= 16384)
    ),
    CONSTRAINT ck_audit_logs_retention CHECK (retention_until >= occurred_at + interval '7 years')
);

CREATE UNIQUE INDEX uq_user_permission_override_active
    ON user_permission_overrides(user_id, permission_id) WHERE is_deleted = false;
CREATE INDEX ix_roles_scope_status ON roles(account_scope, status, id) WHERE is_deleted = false;
CREATE INDEX ix_permissions_module_status ON permissions(module, status, code) WHERE is_deleted = false;
CREATE INDEX ix_users_role_active ON users(role_id, status, id) WHERE is_deleted = false;
CREATE INDEX ix_users_status_created ON users(status, created_at DESC, id DESC) WHERE is_deleted = false;
CREATE INDEX ix_auth_sessions_user_active ON auth_sessions(user_id, expires_at DESC, id DESC) WHERE revoked_at IS NULL;
CREATE INDEX ix_auth_sessions_expiry ON auth_sessions(expires_at, id) WHERE revoked_at IS NULL;
CREATE INDEX ix_role_permissions_permission ON role_permissions(permission_id, role_id);
CREATE INDEX ix_user_overrides_permission_active ON user_permission_overrides(permission_id, user_id) WHERE is_deleted = false;
CREATE INDEX ix_user_role_history_user_time ON user_role_histories(user_id, created_at DESC, id DESC);
CREATE INDEX ix_audit_logs_time ON audit_logs(occurred_at DESC, id DESC);
CREATE INDEX ix_audit_logs_module_time ON audit_logs(module, occurred_at DESC, id DESC);
CREATE INDEX ix_audit_logs_action_time ON audit_logs(action_code, occurred_at DESC, id DESC);
CREATE INDEX ix_audit_logs_actor_time ON audit_logs(actor_user_id, occurred_at DESC, id DESC) WHERE actor_user_id IS NOT NULL;
CREATE INDEX ix_audit_logs_target_time ON audit_logs(target_type, target_id, occurred_at DESC, id DESC) WHERE target_id IS NOT NULL;
CREATE INDEX ix_audit_logs_outcome_time ON audit_logs(outcome, occurred_at DESC, id DESC);
CREATE INDEX ix_audit_logs_retention ON audit_logs(retention_until, id);

CREATE FUNCTION enforce_user_role_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    role_scope account_scope_enum;
    role_state role_status_enum;
    role_deleted boolean;
BEGIN
    SELECT account_scope, status, is_deleted
      INTO role_scope, role_state, role_deleted
      FROM roles WHERE id = NEW.role_id;
    IF NOT FOUND OR role_scope::text <> NEW.account_type::text OR role_state <> 'ACTIVE' OR role_deleted THEN
        RAISE EXCEPTION 'user role must be active and match account type' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_users_role_scope
BEFORE INSERT OR UPDATE OF role_id, account_type ON users
FOR EACH ROW EXECUTE FUNCTION enforce_user_role_scope();

CREATE FUNCTION enforce_staff_permission_override() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    target_type account_type_enum;
    permission_risk permission_risk_enum;
BEGIN
    SELECT account_type INTO target_type FROM users WHERE id = NEW.user_id AND is_deleted = false;
    SELECT risk_level INTO permission_risk FROM permissions
      WHERE id = NEW.permission_id AND status = 'ACTIVE' AND is_deleted = false;
    IF target_type IS DISTINCT FROM 'STAFF' THEN
        RAISE EXCEPTION 'permission override target must be an active STAFF user' USING ERRCODE = '23514';
    END IF;
    IF permission_risk IS NULL OR permission_risk = 'PROTECTED' THEN
        RAISE EXCEPTION 'inactive or protected permission cannot be overridden' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_user_overrides_staff_only
BEFORE INSERT OR UPDATE OF user_id, permission_id, is_deleted ON user_permission_overrides
FOR EACH ROW WHEN (NEW.is_deleted = false)
EXECUTE FUNCTION enforce_staff_permission_override();

CREATE FUNCTION reject_immutable_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER trg_user_role_histories_immutable
BEFORE UPDATE OR DELETE ON user_role_histories
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();

CREATE TRIGGER trg_audit_logs_immutable
BEFORE UPDATE OR DELETE ON audit_logs
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
