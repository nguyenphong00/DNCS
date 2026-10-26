-- P3 Medical Encounter aggregate and encounter service contract.
CREATE TYPE encounter_status_enum AS ENUM ('DRAFT', 'FINALIZED');
CREATE TYPE encounter_amendment_type_enum AS ENUM (
    'CLINICAL_ADDENDUM', 'CLINICAL_CORRECTION', 'ADMINISTRATIVE_NOTE'
);
CREATE TYPE encounter_service_source_enum AS ENUM ('SYSTEM_CONSULTATION', 'DOCTOR_ORDER');
CREATE TYPE encounter_service_status_enum AS ENUM ('ACTIVE', 'REMOVED');
CREATE SEQUENCE encounter_code_seq AS bigint NO CYCLE;

ALTER TABLE appointments
    ADD CONSTRAINT uq_appointments_encounter_snapshot
    UNIQUE (id, patient_id, doctor_profile_id, specialty_id);

CREATE TABLE medical_encounters (
    id bigint GENERATED ALWAYS AS IDENTITY,
    encounter_code varchar(32) NOT NULL,
    appointment_id bigint NOT NULL,
    patient_id bigint NOT NULL,
    doctor_profile_id bigint NOT NULL,
    specialty_id bigint NOT NULL,
    patient_code_at_start varchar(32) NOT NULL,
    patient_name_at_start varchar(150) NOT NULL,
    patient_date_of_birth_at_start date,
    patient_gender_at_start varchar(20),
    doctor_code_at_start varchar(32) NOT NULL,
    doctor_name_at_start varchar(150) NOT NULL,
    specialty_code_at_start varchar(34) NOT NULL,
    specialty_name_at_start varchar(200) NOT NULL,
    status encounter_status_enum NOT NULL DEFAULT 'DRAFT',
    chief_complaint text,
    symptoms text,
    diagnosis text,
    conclusion text,
    clinical_notes text,
    follow_up_recommendation text,
    started_at timestamptz NOT NULL DEFAULT now(),
    finalized_at timestamptz,
    finalized_by_user_id bigint,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_medical_encounters PRIMARY KEY (id),
    CONSTRAINT uq_medical_encounters_code UNIQUE (encounter_code),
    CONSTRAINT uq_medical_encounters_appointment UNIQUE (appointment_id),
    CONSTRAINT fk_medical_encounters_appointment_snapshot
        FOREIGN KEY (appointment_id, patient_id, doctor_profile_id, specialty_id)
        REFERENCES appointments(id, patient_id, doctor_profile_id, specialty_id) ON DELETE RESTRICT,
    CONSTRAINT fk_medical_encounters_patient FOREIGN KEY (patient_id) REFERENCES patients(id) ON DELETE RESTRICT,
    CONSTRAINT fk_medical_encounters_doctor FOREIGN KEY (doctor_profile_id) REFERENCES doctor_profiles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_medical_encounters_specialty FOREIGN KEY (specialty_id) REFERENCES specialties(id) ON DELETE RESTRICT,
    CONSTRAINT fk_medical_encounters_finalized_by FOREIGN KEY (finalized_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_medical_encounters_finalize CHECK (
        (status = 'DRAFT' AND finalized_at IS NULL AND finalized_by_user_id IS NULL)
        OR (status = 'FINALIZED' AND finalized_at IS NOT NULL AND finalized_by_user_id IS NOT NULL
            AND started_at <= finalized_at)
    ),
    CONSTRAINT ck_medical_encounters_not_deleted CHECK (is_deleted = false),
    CONSTRAINT ck_medical_encounters_version CHECK (version >= 1),
    CONSTRAINT ck_medical_encounters_text_length CHECK (
        (chief_complaint IS NULL OR char_length(chief_complaint) BETWEEN 1 AND 1000)
        AND (symptoms IS NULL OR char_length(symptoms) BETWEEN 1 AND 4000)
        AND (diagnosis IS NULL OR char_length(diagnosis) BETWEEN 1 AND 4000)
        AND (conclusion IS NULL OR char_length(conclusion) BETWEEN 1 AND 4000)
        AND (clinical_notes IS NULL OR char_length(clinical_notes) BETWEEN 1 AND 8000)
        AND (follow_up_recommendation IS NULL OR char_length(follow_up_recommendation) BETWEEN 1 AND 2000)
    )
);

CREATE INDEX ix_encounters_patient_started ON medical_encounters(patient_id, started_at DESC, id DESC);
CREATE INDEX ix_encounters_doctor_started ON medical_encounters(doctor_profile_id, started_at DESC, id DESC);
CREATE INDEX ix_encounters_status_started ON medical_encounters(status, started_at DESC, id DESC);
CREATE INDEX ix_encounters_specialty_started ON medical_encounters(specialty_id, started_at DESC, id DESC);

CREATE TABLE encounter_vital_signs (
    id bigint GENERATED ALWAYS AS IDENTITY,
    encounter_id bigint NOT NULL,
    temperature_c numeric(4,1),
    systolic_bp smallint,
    diastolic_bp smallint,
    heart_rate_bpm smallint,
    respiratory_rate_bpm smallint,
    spo2_percent numeric(5,2),
    weight_kg numeric(7,3),
    height_cm numeric(6,2),
    pain_score smallint,
    notes varchar(1000),
    measured_at timestamptz NOT NULL,
    recorded_by_user_id bigint NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_encounter_vital_signs PRIMARY KEY (id),
    CONSTRAINT fk_vital_signs_encounter FOREIGN KEY (encounter_id) REFERENCES medical_encounters(id) ON DELETE RESTRICT,
    CONSTRAINT fk_vital_signs_actor FOREIGN KEY (recorded_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_vital_signs_values CHECK (
        (temperature_c IS NULL OR temperature_c >= 0)
        AND (systolic_bp IS NULL OR systolic_bp >= 0)
        AND (diastolic_bp IS NULL OR diastolic_bp >= 0)
        AND (heart_rate_bpm IS NULL OR heart_rate_bpm >= 0)
        AND (respiratory_rate_bpm IS NULL OR respiratory_rate_bpm >= 0)
        AND (spo2_percent IS NULL OR spo2_percent BETWEEN 0 AND 100)
        AND (weight_kg IS NULL OR weight_kg >= 0)
        AND (height_cm IS NULL OR height_cm >= 0)
        AND (pain_score IS NULL OR pain_score BETWEEN 0 AND 10)
        AND (systolic_bp IS NULL OR diastolic_bp IS NULL OR systolic_bp > diastolic_bp)
        AND (temperature_c IS NOT NULL OR systolic_bp IS NOT NULL OR diastolic_bp IS NOT NULL
             OR heart_rate_bpm IS NOT NULL OR respiratory_rate_bpm IS NOT NULL OR spo2_percent IS NOT NULL
             OR weight_kg IS NOT NULL OR height_cm IS NOT NULL OR pain_score IS NOT NULL)
        AND (notes IS NULL OR char_length(notes) BETWEEN 1 AND 1000)
    ),
    CONSTRAINT ck_vital_signs_version CHECK (version >= 1)
);
CREATE INDEX ix_vital_signs_timeline ON encounter_vital_signs(encounter_id, measured_at, id);

CREATE TABLE medical_encounter_status_histories (
    id bigint GENERATED ALWAYS AS IDENTITY,
    encounter_id bigint NOT NULL,
    from_status encounter_status_enum,
    to_status encounter_status_enum NOT NULL,
    actor_user_id bigint NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_medical_encounter_status_histories PRIMARY KEY (id),
    CONSTRAINT fk_encounter_history_encounter FOREIGN KEY (encounter_id) REFERENCES medical_encounters(id) ON DELETE RESTRICT,
    CONSTRAINT fk_encounter_history_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_encounter_history_metadata CHECK (pg_column_size(metadata) <= 16384)
);
CREATE INDEX ix_encounter_status_timeline
    ON medical_encounter_status_histories(encounter_id, created_at, id);

CREATE TABLE medical_encounter_amendments (
    id bigint GENERATED ALWAYS AS IDENTITY,
    encounter_id bigint NOT NULL,
    sequence_number integer NOT NULL,
    amendment_type encounter_amendment_type_enum NOT NULL,
    reason text NOT NULL,
    content text NOT NULL,
    created_by_user_id bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_medical_encounter_amendments PRIMARY KEY (id),
    CONSTRAINT uq_encounter_amendments_sequence UNIQUE (encounter_id, sequence_number),
    CONSTRAINT fk_encounter_amendment_encounter FOREIGN KEY (encounter_id) REFERENCES medical_encounters(id) ON DELETE RESTRICT,
    CONSTRAINT fk_encounter_amendment_actor FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_encounter_amendment_sequence CHECK (sequence_number >= 1),
    CONSTRAINT ck_encounter_amendment_text CHECK (
        char_length(btrim(reason)) BETWEEN 1 AND 1000
        AND char_length(btrim(content)) BETWEEN 1 AND 8000
    )
);

CREATE TABLE encounter_services (
    id bigint GENERATED ALWAYS AS IDENTITY,
    encounter_id bigint NOT NULL,
    service_id bigint NOT NULL,
    service_price_version_id_at_order bigint NOT NULL,
    service_code_at_order varchar(34) NOT NULL,
    service_name_at_order varchar(200) NOT NULL,
    unit_label_at_order varchar(50) NOT NULL,
    source encounter_service_source_enum NOT NULL,
    quantity smallint NOT NULL,
    status encounter_service_status_enum NOT NULL DEFAULT 'ACTIVE',
    assigned_by_user_id bigint,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    removed_by_user_id bigint,
    removed_at timestamptz,
    removal_reason varchar(500),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT pk_encounter_services PRIMARY KEY (id),
    CONSTRAINT fk_encounter_services_encounter FOREIGN KEY (encounter_id) REFERENCES medical_encounters(id) ON DELETE RESTRICT,
    CONSTRAINT fk_encounter_services_service FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE RESTRICT,
    CONSTRAINT fk_encounter_services_price FOREIGN KEY (service_price_version_id_at_order, service_id)
        REFERENCES service_price_versions(id, service_id) ON DELETE RESTRICT,
    CONSTRAINT fk_encounter_services_assigned_by FOREIGN KEY (assigned_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_encounter_services_removed_by FOREIGN KEY (removed_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_encounter_services_snapshot CHECK (
        char_length(btrim(service_code_at_order)) BETWEEN 1 AND 34
        AND char_length(btrim(service_name_at_order)) BETWEEN 1 AND 200
        AND char_length(btrim(unit_label_at_order)) BETWEEN 1 AND 50
    ),
    CONSTRAINT ck_encounter_services_source CHECK (
        (source = 'SYSTEM_CONSULTATION' AND quantity = 1 AND assigned_by_user_id IS NULL AND status = 'ACTIVE')
        OR (source = 'DOCTOR_ORDER' AND assigned_by_user_id IS NOT NULL)
    ),
    CONSTRAINT ck_encounter_services_quantity CHECK (quantity BETWEEN 1 AND 999),
    CONSTRAINT ck_encounter_services_removal CHECK (
        (status = 'ACTIVE' AND removed_by_user_id IS NULL AND removed_at IS NULL AND removal_reason IS NULL)
        OR (status = 'REMOVED' AND source = 'DOCTOR_ORDER' AND removed_by_user_id IS NOT NULL
            AND removed_at IS NOT NULL AND char_length(btrim(removal_reason)) BETWEEN 1 AND 500)
    ),
    CONSTRAINT ck_encounter_services_version CHECK (version >= 1)
);

CREATE UNIQUE INDEX uq_encounter_services_active
    ON encounter_services(encounter_id, service_id) WHERE status = 'ACTIVE';
CREATE INDEX ix_encounter_services_list ON encounter_services(encounter_id, status, id);
CREATE INDEX ix_encounter_services_service_history ON encounter_services(service_id, assigned_at DESC, id DESC);

CREATE FUNCTION assign_encounter_code() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.encounter_code := 'ENC-' || to_char(NEW.started_at AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYYYMMDD')
        || '-' || lpad(nextval('encounter_code_seq')::text, 8, '0');
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_medical_encounters_code BEFORE INSERT ON medical_encounters
FOR EACH ROW EXECUTE FUNCTION assign_encounter_code();

CREATE FUNCTION enforce_encounter_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.encounter_code <> OLD.encounter_code OR NEW.appointment_id <> OLD.appointment_id
       OR NEW.patient_id <> OLD.patient_id OR NEW.doctor_profile_id <> OLD.doctor_profile_id
       OR NEW.specialty_id <> OLD.specialty_id OR NEW.patient_code_at_start <> OLD.patient_code_at_start
       OR NEW.patient_name_at_start <> OLD.patient_name_at_start
       OR NEW.patient_date_of_birth_at_start IS DISTINCT FROM OLD.patient_date_of_birth_at_start
       OR NEW.patient_gender_at_start IS DISTINCT FROM OLD.patient_gender_at_start
       OR NEW.doctor_code_at_start <> OLD.doctor_code_at_start OR NEW.doctor_name_at_start <> OLD.doctor_name_at_start
       OR NEW.specialty_code_at_start <> OLD.specialty_code_at_start OR NEW.specialty_name_at_start <> OLD.specialty_name_at_start
       OR NEW.started_at <> OLD.started_at OR NEW.is_deleted <> OLD.is_deleted THEN
        RAISE EXCEPTION 'encounter identity and snapshots are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'FINALIZED' THEN
        RAISE EXCEPTION 'finalized encounter is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_medical_encounters_update
BEFORE UPDATE ON medical_encounters FOR EACH ROW EXECUTE FUNCTION enforce_encounter_update();

CREATE FUNCTION enforce_draft_encounter_child() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE parent_status encounter_status_enum;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'encounter child rows cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.encounter_id <> OLD.encounter_id THEN
        RAISE EXCEPTION 'encounter child ownership is immutable' USING ERRCODE = '55000';
    END IF;
    SELECT status INTO parent_status FROM medical_encounters WHERE id = NEW.encounter_id;
    IF parent_status IS DISTINCT FROM 'DRAFT' THEN
        RAISE EXCEPTION 'encounter child mutation requires DRAFT encounter' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_vital_signs_draft_guard
BEFORE INSERT OR UPDATE OR DELETE ON encounter_vital_signs
FOR EACH ROW EXECUTE FUNCTION enforce_draft_encounter_child();
CREATE TRIGGER trg_encounter_services_draft_guard
BEFORE INSERT OR UPDATE OR DELETE ON encounter_services
FOR EACH ROW EXECUTE FUNCTION enforce_draft_encounter_child();

CREATE TRIGGER trg_encounter_status_histories_immutable
BEFORE UPDATE OR DELETE ON medical_encounter_status_histories
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
CREATE TRIGGER trg_encounter_amendments_immutable
BEFORE UPDATE OR DELETE ON medical_encounter_amendments
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
