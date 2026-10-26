-- P4 Medicine catalog and immutable time-versioned VND sale prices.
CREATE TABLE medicines (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code varchar(34) NOT NULL CONSTRAINT uq_medicines_code UNIQUE,
    name varchar(200) NOT NULL,
    normalized_name varchar(200) NOT NULL CONSTRAINT uq_medicines_normalized_name UNIQUE,
    active_ingredient varchar(200) NOT NULL,
    dosage_form varchar(200) NOT NULL,
    strength varchar(200) NOT NULL,
    dispense_unit varchar(50) NOT NULL,
    description varchar(1000),
    low_stock_threshold numeric(14,3) NOT NULL DEFAULT 0,
    status catalog_status_enum NOT NULL DEFAULT 'INACTIVE',
    created_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    updated_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    deleted_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    archive_reason varchar(1000),
    CONSTRAINT uq_medicines_id_pair UNIQUE (id, code),
    CONSTRAINT ck_medicines_code CHECK (code ~ '^MED-[A-Z0-9-]{2,30}$'),
    CONSTRAINT ck_medicines_fields CHECK (
        char_length(btrim(name)) BETWEEN 1 AND 200
        AND char_length(btrim(normalized_name)) BETWEEN 1 AND 200
        AND char_length(btrim(active_ingredient)) BETWEEN 1 AND 200
        AND char_length(btrim(dosage_form)) BETWEEN 1 AND 200
        AND char_length(btrim(strength)) BETWEEN 1 AND 200
        AND char_length(btrim(dispense_unit)) BETWEEN 1 AND 50
        AND (description IS NULL OR char_length(description) BETWEEN 1 AND 1000)
    ),
    CONSTRAINT ck_medicines_threshold CHECK (low_stock_threshold BETWEEN 0 AND 9999999999.999),
    CONSTRAINT ck_medicines_version CHECK (version >= 1),
    CONSTRAINT ck_medicines_archive CHECK (
        (NOT is_deleted AND deleted_at IS NULL AND deleted_by_user_id IS NULL AND archive_reason IS NULL)
        OR (is_deleted AND status = 'INACTIVE' AND deleted_at IS NOT NULL
            AND deleted_by_user_id IS NOT NULL AND char_length(btrim(archive_reason)) BETWEEN 1 AND 1000)
    )
);

CREATE INDEX ix_medicines_active ON medicines(status, id) WHERE NOT is_deleted;

CREATE TABLE medicine_price_versions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    medicine_id bigint NOT NULL REFERENCES medicines(id) ON DELETE RESTRICT,
    effective_from timestamptz NOT NULL,
    unit_sale_price_vnd numeric(14,0) NOT NULL,
    currency char(3) NOT NULL DEFAULT 'VND',
    created_by_user_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    cancellation_reason varchar(500),
    cancelled_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    cancelled_at timestamptz,
    CONSTRAINT uq_medicine_price_effective_from UNIQUE (medicine_id, effective_from),
    CONSTRAINT uq_medicine_price_id_medicine UNIQUE (id, medicine_id),
    CONSTRAINT ck_medicine_price_value CHECK (unit_sale_price_vnd >= 0 AND currency = 'VND'),
    CONSTRAINT ck_medicine_price_cancel CHECK (
        (cancelled_at IS NULL AND cancelled_by_user_id IS NULL AND cancellation_reason IS NULL)
        OR (cancelled_at IS NOT NULL AND cancelled_by_user_id IS NOT NULL
            AND char_length(btrim(cancellation_reason)) BETWEEN 1 AND 500)
    )
);

CREATE INDEX ix_medicine_prices_current
    ON medicine_price_versions(medicine_id, effective_from DESC, id DESC) WHERE cancelled_at IS NULL;

CREATE FUNCTION enforce_medicine_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.code <> OLD.code OR NEW.created_at <> OLD.created_at OR NEW.created_by_user_id IS DISTINCT FROM OLD.created_by_user_id THEN
        RAISE EXCEPTION 'medicine identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.is_deleted THEN
        RAISE EXCEPTION 'archived medicine is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_medicines_identity BEFORE UPDATE ON medicines
FOR EACH ROW EXECUTE FUNCTION enforce_medicine_identity();

CREATE FUNCTION enforce_medicine_price_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.medicine_id <> OLD.medicine_id OR NEW.effective_from <> OLD.effective_from
       OR NEW.unit_sale_price_vnd <> OLD.unit_sale_price_vnd OR NEW.currency <> OLD.currency
       OR NEW.created_by_user_id <> OLD.created_by_user_id OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'medicine price identity and value are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.cancelled_at IS NOT NULL OR OLD.effective_from <= now() OR NEW.cancelled_at IS NULL THEN
        RAISE EXCEPTION 'only a scheduled medicine price can be cancelled' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_medicine_prices_update BEFORE UPDATE ON medicine_price_versions
FOR EACH ROW EXECUTE FUNCTION enforce_medicine_price_update();
CREATE TRIGGER trg_medicine_prices_no_delete BEFORE DELETE ON medicine_price_versions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
