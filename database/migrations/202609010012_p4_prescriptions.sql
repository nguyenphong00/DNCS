-- P4 prescription series, version lifecycle and immutable medicine snapshots.
CREATE TYPE prescription_series_status_enum AS ENUM ('DRAFT','ISSUED','DISCARDED');
CREATE TYPE prescription_status_enum AS ENUM ('DRAFT','ISSUED','SUPERSEDED','DISCARDED');
CREATE SEQUENCE prescription_code_seq AS bigint NO CYCLE;

CREATE TABLE prescription_series (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    prescription_code varchar(32) NOT NULL UNIQUE,
    encounter_id bigint NOT NULL REFERENCES medical_encounters(id) ON DELETE RESTRICT,
    status prescription_series_status_enum NOT NULL DEFAULT 'DRAFT',
    current_issued_prescription_id bigint,
    created_by_user_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT uq_prescription_series_id_pair UNIQUE (id, current_issued_prescription_id),
    CONSTRAINT ck_prescription_series_state CHECK (
        (status IN ('DRAFT','DISCARDED') AND current_issued_prescription_id IS NULL)
        OR (status = 'ISSUED' AND current_issued_prescription_id IS NOT NULL)
    ),
    CONSTRAINT ck_prescription_series_version CHECK (version >= 1)
);

CREATE TABLE prescriptions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    series_id bigint NOT NULL REFERENCES prescription_series(id) ON DELETE RESTRICT,
    version_number integer NOT NULL,
    version_code varchar(48) NOT NULL UNIQUE,
    source_prescription_id bigint,
    status prescription_status_enum NOT NULL DEFAULT 'DRAFT',
    overall_note varchar(1000),
    created_by_user_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    issued_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    issued_at timestamptz,
    superseded_at timestamptz,
    discarded_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    discarded_at timestamptz,
    discard_reason varchar(500),
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT uq_prescriptions_series_version UNIQUE (series_id, version_number),
    CONSTRAINT uq_prescriptions_id_series UNIQUE (id, series_id),
    CONSTRAINT ck_prescriptions_version_number CHECK (
        version_number >= 1 AND ((version_number = 1 AND source_prescription_id IS NULL)
        OR (version_number > 1 AND source_prescription_id IS NOT NULL))
    ),
    CONSTRAINT ck_prescriptions_state CHECK (
        (status = 'DRAFT' AND issued_by_user_id IS NULL AND issued_at IS NULL AND superseded_at IS NULL
            AND discarded_by_user_id IS NULL AND discarded_at IS NULL AND discard_reason IS NULL)
        OR (status = 'ISSUED' AND issued_by_user_id IS NOT NULL AND issued_at IS NOT NULL AND superseded_at IS NULL
            AND discarded_by_user_id IS NULL AND discarded_at IS NULL AND discard_reason IS NULL)
        OR (status = 'SUPERSEDED' AND issued_by_user_id IS NOT NULL AND issued_at IS NOT NULL AND superseded_at IS NOT NULL
            AND discarded_by_user_id IS NULL AND discarded_at IS NULL AND discard_reason IS NULL)
        OR (status = 'DISCARDED' AND issued_by_user_id IS NULL AND issued_at IS NULL AND superseded_at IS NULL
            AND discarded_by_user_id IS NOT NULL AND discarded_at IS NOT NULL
            AND char_length(btrim(discard_reason)) BETWEEN 1 AND 500)
    ),
    CONSTRAINT ck_prescriptions_note CHECK (overall_note IS NULL OR char_length(overall_note) BETWEEN 1 AND 1000),
    CONSTRAINT ck_prescriptions_version CHECK (version >= 1)
);

CREATE UNIQUE INDEX uq_prescriptions_one_draft ON prescriptions(series_id) WHERE status = 'DRAFT';
CREATE UNIQUE INDEX uq_prescriptions_one_issued ON prescriptions(series_id) WHERE status = 'ISSUED';

CREATE TABLE prescription_items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    prescription_id bigint NOT NULL REFERENCES prescriptions(id) ON DELETE RESTRICT,
    medicine_id bigint NOT NULL REFERENCES medicines(id) ON DELETE RESTRICT,
    medicine_code_at_prescription varchar(34) NOT NULL,
    medicine_name_at_prescription varchar(200) NOT NULL,
    dosage_form_at_prescription varchar(200),
    strength_at_prescription varchar(200),
    dispense_unit_at_prescription varchar(50) NOT NULL,
    quantity smallint NOT NULL,
    usage_instruction varchar(1000) NOT NULL,
    notes varchar(500),
    sort_order smallint NOT NULL,
    is_removed boolean NOT NULL DEFAULT false,
    removed_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    removed_at timestamptz,
    removal_reason varchar(500),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT ck_prescription_items_values CHECK (
        char_length(btrim(medicine_code_at_prescription)) BETWEEN 1 AND 34
        AND char_length(btrim(medicine_name_at_prescription)) BETWEEN 1 AND 200
        AND char_length(btrim(dispense_unit_at_prescription)) BETWEEN 1 AND 50
        AND quantity BETWEEN 1 AND 999 AND sort_order >= 1
        AND char_length(btrim(usage_instruction)) BETWEEN 1 AND 1000
        AND (notes IS NULL OR char_length(notes) BETWEEN 1 AND 500)
        AND version >= 1
    ),
    CONSTRAINT ck_prescription_items_removal CHECK (
        (NOT is_removed AND removed_by_user_id IS NULL AND removed_at IS NULL AND removal_reason IS NULL)
        OR (is_removed AND removed_by_user_id IS NOT NULL AND removed_at IS NOT NULL
            AND char_length(btrim(removal_reason)) BETWEEN 1 AND 500)
    )
);

CREATE UNIQUE INDEX uq_prescription_items_active_medicine
    ON prescription_items(prescription_id, medicine_id) WHERE NOT is_removed;
CREATE UNIQUE INDEX uq_prescription_items_active_sort
    ON prescription_items(prescription_id, sort_order) WHERE NOT is_removed;
CREATE INDEX ix_prescription_series_encounter ON prescription_series(encounter_id, created_at DESC, id DESC);
CREATE INDEX ix_prescription_series_status ON prescription_series(status, created_at DESC, id DESC);
CREATE INDEX ix_prescriptions_status_issued ON prescriptions(status, issued_at DESC, id DESC);
CREATE INDEX ix_prescription_items_list ON prescription_items(prescription_id, is_removed, sort_order, id);
CREATE INDEX ix_prescription_items_medicine_history ON prescription_items(medicine_id, created_at DESC, id DESC);

CREATE FUNCTION assign_prescription_code() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.prescription_code := 'RX-' || to_char(NEW.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh','YYYYMMDD')
        || '-' || lpad(nextval('prescription_code_seq')::text, 8, '0');
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_prescription_series_code BEFORE INSERT ON prescription_series FOR EACH ROW EXECUTE FUNCTION assign_prescription_code();
