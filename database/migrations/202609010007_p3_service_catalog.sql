-- P3 Service Catalog: immutable service identity and time-versioned VND prices.
CREATE TYPE service_type_enum AS ENUM ('CONSULTATION', 'PROCEDURE', 'OTHER');

CREATE TABLE services (
    id bigint GENERATED ALWAYS AS IDENTITY,
    code varchar(34) NOT NULL,
    name varchar(200) NOT NULL,
    normalized_name varchar(200) NOT NULL,
    service_type service_type_enum NOT NULL,
    specialty_id bigint,
    unit_label varchar(50) NOT NULL DEFAULT 'Lần',
    description varchar(1000),
    status catalog_status_enum NOT NULL DEFAULT 'INACTIVE',
    created_by_user_id bigint,
    updated_by_user_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    deleted_by_user_id bigint,
    archive_reason varchar(1000),
    CONSTRAINT pk_services PRIMARY KEY (id),
    CONSTRAINT uq_services_code UNIQUE (code),
    CONSTRAINT uq_services_normalized_name UNIQUE (normalized_name),
    CONSTRAINT fk_services_specialty FOREIGN KEY (specialty_id) REFERENCES specialties(id) ON DELETE RESTRICT,
    CONSTRAINT fk_services_created_by FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_services_updated_by FOREIGN KEY (updated_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_services_deleted_by FOREIGN KEY (deleted_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_services_code CHECK (code ~ '^SVC-[A-Z0-9-]{2,30}$'),
    CONSTRAINT ck_services_name CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    CONSTRAINT ck_services_normalized_name CHECK (char_length(btrim(normalized_name)) BETWEEN 1 AND 200),
    CONSTRAINT ck_services_unit_label CHECK (char_length(btrim(unit_label)) BETWEEN 1 AND 50),
    CONSTRAINT ck_services_description CHECK (description IS NULL OR char_length(description) <= 1000),
    CONSTRAINT ck_services_type_specialty CHECK (
        (service_type = 'CONSULTATION' AND specialty_id IS NOT NULL)
        OR (service_type IN ('PROCEDURE','OTHER') AND specialty_id IS NULL)
    ),
    CONSTRAINT ck_services_version CHECK (version >= 1),
    CONSTRAINT ck_services_archive CHECK (
        (is_deleted = false AND deleted_at IS NULL AND deleted_by_user_id IS NULL AND archive_reason IS NULL)
        OR (is_deleted = true AND status = 'INACTIVE' AND deleted_at IS NOT NULL
            AND deleted_by_user_id IS NOT NULL
            AND char_length(btrim(archive_reason)) BETWEEN 1 AND 1000)
    )
);

CREATE UNIQUE INDEX uq_services_active_consultation_specialty
    ON services(specialty_id)
    WHERE service_type = 'CONSULTATION' AND status = 'ACTIVE' AND is_deleted = false;
CREATE INDEX ix_services_active_type ON services(status, service_type, id) WHERE is_deleted = false;
CREATE INDEX ix_services_specialty_active ON services(specialty_id, id)
    WHERE is_deleted = false AND specialty_id IS NOT NULL;

CREATE TABLE service_price_versions (
    id bigint GENERATED ALWAYS AS IDENTITY,
    service_id bigint NOT NULL,
    effective_from timestamptz NOT NULL,
    unit_price_vnd numeric(14,0) NOT NULL,
    currency char(3) NOT NULL DEFAULT 'VND',
    created_by_user_id bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    cancellation_reason varchar(500),
    cancelled_by_user_id bigint,
    cancelled_at timestamptz,
    CONSTRAINT pk_service_price_versions PRIMARY KEY (id),
    CONSTRAINT uq_service_price_effective_from UNIQUE (service_id, effective_from),
    CONSTRAINT uq_service_price_id_service UNIQUE (id, service_id),
    CONSTRAINT fk_service_prices_service FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE RESTRICT,
    CONSTRAINT fk_service_prices_created_by FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_service_prices_cancelled_by FOREIGN KEY (cancelled_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_service_price_value CHECK (unit_price_vnd >= 0 AND currency = 'VND'),
    CONSTRAINT ck_service_price_cancellation CHECK (
        (cancellation_reason IS NULL AND cancelled_by_user_id IS NULL AND cancelled_at IS NULL)
        OR (char_length(btrim(cancellation_reason)) BETWEEN 1 AND 500
            AND cancelled_by_user_id IS NOT NULL AND cancelled_at IS NOT NULL)
    )
);

CREATE INDEX ix_service_prices_current
    ON service_price_versions(service_id, effective_from DESC, id DESC)
    WHERE cancelled_at IS NULL;

CREATE FUNCTION enforce_service_identity_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.code <> OLD.code
       OR NEW.service_type <> OLD.service_type
       OR NEW.specialty_id IS DISTINCT FROM OLD.specialty_id THEN
        RAISE EXCEPTION 'service identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.is_deleted THEN
        RAISE EXCEPTION 'archived service is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_services_identity_immutable
BEFORE UPDATE ON services FOR EACH ROW EXECUTE FUNCTION enforce_service_identity_immutable();

CREATE TRIGGER trg_service_price_versions_no_delete
BEFORE DELETE ON service_price_versions FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
