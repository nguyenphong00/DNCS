-- Prerequisites shared by P1 profiles/master data and P2 scheduling/appointment.
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TYPE employment_status_enum AS ENUM ('ACTIVE', 'INACTIVE', 'TERMINATED');
CREATE TYPE doctor_status_enum AS ENUM ('ACTIVE', 'INACTIVE');
CREATE TYPE catalog_status_enum AS ENUM ('ACTIVE', 'INACTIVE');
CREATE TYPE room_type_enum AS ENUM ('CONSULTATION', 'FUNCTIONAL', 'OTHER');
CREATE TYPE patient_created_source_enum AS ENUM ('SELF_REGISTRATION', 'COUNTER_ADMIN', 'COUNTER_CASHIER');
CREATE TYPE doctor_schedule_status_enum AS ENUM ('DRAFT', 'ACTIVE', 'CANCELLED');
CREATE TYPE appointment_status_enum AS ENUM (
    'PENDING_APPROVAL', 'CONFIRMED', 'REJECTED', 'CANCELLED',
    'CHECKED_IN', 'NO_SHOW', 'IN_PROGRESS', 'COMPLETED'
);
CREATE TYPE appointment_source_enum AS ENUM ('PATIENT_SELF', 'COUNTER', 'FOLLOW_UP');
CREATE TYPE appointment_approval_mode_enum AS ENUM ('AUTO', 'MANUAL', 'DIRECT_FOLLOW_UP');
CREATE TYPE appointment_actor_type_enum AS ENUM ('USER', 'SYSTEM');
CREATE TYPE setting_value_type_enum AS ENUM ('BOOLEAN', 'INTEGER');
CREATE TYPE idempotency_status_enum AS ENUM ('PROCESSING', 'COMPLETED');

CREATE TABLE gender_codes (
    code varchar(20) NOT NULL,
    display_name_vi varchar(50) NOT NULL,
    sort_order smallint NOT NULL,
    is_active boolean NOT NULL DEFAULT true,
    CONSTRAINT pk_gender_codes PRIMARY KEY (code),
    CONSTRAINT uq_gender_codes_sort_order UNIQUE (sort_order),
    CONSTRAINT ck_gender_codes_code CHECK (code ~ '^[A-Z][A-Z0-9_]{1,19}$'),
    CONSTRAINT ck_gender_codes_name CHECK (char_length(btrim(display_name_vi)) BETWEEN 1 AND 50),
    CONSTRAINT ck_gender_codes_sort_order CHECK (sort_order >= 0)
);

INSERT INTO gender_codes(code, display_name_vi, sort_order)
VALUES
    ('MALE', 'Nam', 10),
    ('FEMALE', 'Nữ', 20),
    ('OTHER', 'Khác', 30),
    ('UNSPECIFIED', 'Không cung cấp', 40)
ON CONFLICT (code) DO UPDATE SET
    display_name_vi = EXCLUDED.display_name_vi,
    sort_order = EXCLUDED.sort_order,
    is_active = true;

CREATE TABLE system_settings (
    id bigint GENERATED ALWAYS AS IDENTITY,
    setting_key varchar(100) NOT NULL,
    value_type setting_value_type_enum NOT NULL,
    boolean_value boolean,
    integer_value integer,
    version bigint NOT NULL DEFAULT 1,
    updated_by_user_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_system_settings PRIMARY KEY (id),
    CONSTRAINT uq_system_settings_key UNIQUE (setting_key),
    CONSTRAINT fk_system_settings_updated_by FOREIGN KEY (updated_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_system_settings_key CHECK (setting_key ~ '^[a-z][a-z0-9_]{2,99}$'),
    CONSTRAINT ck_system_settings_typed_value CHECK (
        (value_type = 'BOOLEAN' AND boolean_value IS NOT NULL AND integer_value IS NULL)
        OR (value_type = 'INTEGER' AND boolean_value IS NULL AND integer_value IS NOT NULL)
    ),
    CONSTRAINT ck_system_settings_version CHECK (version >= 1),
    CONSTRAINT ck_system_settings_catalog CHECK (
        (setting_key = 'appointment_auto_approval_enabled' AND value_type = 'BOOLEAN')
        OR (setting_key = 'appointment_booking_min_lead_minutes' AND value_type = 'INTEGER' AND integer_value BETWEEN 0 AND 1440)
        OR (setting_key = 'appointment_booking_max_advance_days' AND value_type = 'INTEGER' AND integer_value BETWEEN 1 AND 365)
        OR (setting_key = 'appointment_check_in_early_minutes' AND value_type = 'INTEGER' AND integer_value BETWEEN 0 AND 240)
        OR (setting_key = 'appointment_check_in_late_minutes' AND value_type = 'INTEGER' AND integer_value BETWEEN 0 AND 240)
        OR (setting_key = 'appointment_no_show_grace_minutes' AND value_type = 'INTEGER' AND integer_value BETWEEN 0 AND 240)
        OR (setting_key = 'appointment_max_holding_future_per_patient' AND value_type = 'INTEGER' AND integer_value BETWEEN 1 AND 20)
        OR (setting_key = 'default_slot_duration_minutes' AND value_type = 'INTEGER' AND integer_value IN (15,20,30,45,60))
        OR (setting_key = 'inventory_expiry_alert_days' AND value_type = 'INTEGER' AND integer_value BETWEEN 1 AND 365)
    )
);

INSERT INTO system_settings(setting_key, value_type, boolean_value, integer_value)
VALUES
    ('appointment_auto_approval_enabled', 'BOOLEAN', false, NULL),
    ('appointment_booking_min_lead_minutes', 'INTEGER', NULL, 30),
    ('appointment_booking_max_advance_days', 'INTEGER', NULL, 60),
    ('appointment_check_in_early_minutes', 'INTEGER', NULL, 30),
    ('appointment_check_in_late_minutes', 'INTEGER', NULL, 15),
    ('appointment_no_show_grace_minutes', 'INTEGER', NULL, 10),
    ('appointment_max_holding_future_per_patient', 'INTEGER', NULL, 5),
    ('default_slot_duration_minutes', 'INTEGER', NULL, 30),
    ('inventory_expiry_alert_days', 'INTEGER', NULL, 30)
ON CONFLICT (setting_key) DO UPDATE SET
    value_type = EXCLUDED.value_type,
    boolean_value = EXCLUDED.boolean_value,
    integer_value = EXCLUDED.integer_value,
    updated_at = now();

CREATE TABLE idempotency_records (
    id bigint GENERATED ALWAYS AS IDENTITY,
    principal_key varchar(200) NOT NULL,
    actor_user_id bigint,
    operation_scope varchar(200) NOT NULL,
    idempotency_key varchar(128) NOT NULL,
    request_hash char(64) NOT NULL,
    status idempotency_status_enum NOT NULL,
    resource_type varchar(80),
    resource_id bigint,
    http_status smallint,
    response_body jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at timestamptz NOT NULL,
    CONSTRAINT pk_idempotency_records PRIMARY KEY (id),
    CONSTRAINT fk_idempotency_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT uq_idempotency_scope UNIQUE (principal_key, operation_scope, idempotency_key),
    CONSTRAINT ck_idempotency_principal CHECK (char_length(btrim(principal_key)) BETWEEN 1 AND 200),
    CONSTRAINT ck_idempotency_operation CHECK (char_length(btrim(operation_scope)) BETWEEN 1 AND 200),
    CONSTRAINT ck_idempotency_key CHECK (
        char_length(idempotency_key) BETWEEN 16 AND 128
        AND octet_length(idempotency_key) = char_length(idempotency_key)
        AND idempotency_key ~ '^[ -~]+$'
        AND idempotency_key = btrim(idempotency_key)
    ),
    CONSTRAINT ck_idempotency_hash CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_idempotency_expiry CHECK (expires_at > created_at),
    CONSTRAINT ck_idempotency_state CHECK (
        (status = 'PROCESSING' AND resource_type IS NULL AND resource_id IS NULL
            AND http_status IS NULL AND response_body IS NULL AND completed_at IS NULL)
        OR
        (status = 'COMPLETED' AND http_status BETWEEN 200 AND 299 AND completed_at IS NOT NULL)
    )
);

CREATE INDEX ix_idempotency_expiry ON idempotency_records(expires_at, id);
CREATE INDEX ix_idempotency_resource ON idempotency_records(resource_type, resource_id) WHERE resource_id IS NOT NULL;
CREATE INDEX ix_idempotency_processing ON idempotency_records(created_at, id) WHERE status = 'PROCESSING';

CREATE SEQUENCE appointment_code_seq AS bigint NO CYCLE;
