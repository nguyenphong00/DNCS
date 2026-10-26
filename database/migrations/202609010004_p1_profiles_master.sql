-- P1 Staff, Patient and Clinic Master Data.
CREATE TABLE staff_profiles (
    id bigint GENERATED ALWAYS AS IDENTITY,
    user_id bigint NOT NULL,
    staff_code varchar(32) NOT NULL,
    full_name varchar(150) NOT NULL,
    phone_number varchar(20),
    normalized_phone_number varchar(20),
    email varchar(254),
    normalized_email varchar(254),
    date_of_birth date,
    gender_code varchar(20),
    address varchar(500),
    employment_status employment_status_enum NOT NULL DEFAULT 'ACTIVE',
    created_by bigint,
    updated_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_staff_profiles PRIMARY KEY (id),
    CONSTRAINT uq_staff_profiles_user UNIQUE (user_id),
    CONSTRAINT uq_staff_profiles_code UNIQUE (staff_code),
    CONSTRAINT uq_staff_profiles_phone UNIQUE (normalized_phone_number),
    CONSTRAINT uq_staff_profiles_email UNIQUE (normalized_email),
    CONSTRAINT fk_staff_profiles_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_staff_profiles_gender FOREIGN KEY (gender_code) REFERENCES gender_codes(code) ON DELETE RESTRICT,
    CONSTRAINT fk_staff_profiles_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_staff_profiles_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_staff_profiles_code CHECK (staff_code ~ '^STF-[0-9]{8,}$'),
    CONSTRAINT ck_staff_profiles_name CHECK (char_length(btrim(full_name)) BETWEEN 1 AND 150),
    CONSTRAINT ck_staff_profiles_phone CHECK (
        (phone_number IS NULL AND normalized_phone_number IS NULL)
        OR (phone_number IS NOT NULL AND normalized_phone_number ~ '^\+84[0-9]{9,10}$')
    ),
    CONSTRAINT ck_staff_profiles_email CHECK (
        (email IS NULL AND normalized_email IS NULL)
        OR (email IS NOT NULL AND char_length(normalized_email) BETWEEN 3 AND 254)
    ),
    CONSTRAINT ck_staff_profiles_version CHECK (version >= 1)
);

CREATE TABLE doctor_profiles (
    id bigint GENERATED ALWAYS AS IDENTITY,
    staff_profile_id bigint NOT NULL,
    doctor_code varchar(32) NOT NULL,
    status doctor_status_enum NOT NULL DEFAULT 'ACTIVE',
    created_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_doctor_profiles PRIMARY KEY (id),
    CONSTRAINT uq_doctor_profiles_staff UNIQUE (staff_profile_id),
    CONSTRAINT uq_doctor_profiles_code UNIQUE (doctor_code),
    CONSTRAINT fk_doctor_profiles_staff FOREIGN KEY (staff_profile_id) REFERENCES staff_profiles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_profiles_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_doctor_profiles_code CHECK (doctor_code ~ '^DOC-[0-9]{8,}$'),
    CONSTRAINT ck_doctor_profiles_version CHECK (version >= 1)
);

CREATE TABLE patients (
    id bigint GENERATED ALWAYS AS IDENTITY,
    user_id bigint NOT NULL,
    patient_code varchar(32) NOT NULL,
    full_name varchar(150) NOT NULL,
    normalized_full_name varchar(150) NOT NULL,
    date_of_birth date,
    gender_code varchar(20),
    address varchar(500),
    cccd varchar(12),
    health_insurance_number varchar(15),
    medical_history varchar(4000),
    allergies varchar(2000),
    created_source patient_created_source_enum NOT NULL,
    created_by_user_id bigint,
    updated_by_user_id bigint,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    is_deleted boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    restored_at timestamptz,
    deleted_by_user_id bigint,
    restored_by_user_id bigint,
    deletion_reason varchar(1000),
    CONSTRAINT pk_patients PRIMARY KEY (id),
    CONSTRAINT uq_patients_user UNIQUE (user_id),
    CONSTRAINT uq_patients_code UNIQUE (patient_code),
    CONSTRAINT uq_patients_cccd UNIQUE (cccd),
    CONSTRAINT uq_patients_health_insurance UNIQUE (health_insurance_number),
    CONSTRAINT fk_patients_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_patients_gender FOREIGN KEY (gender_code) REFERENCES gender_codes(code) ON DELETE RESTRICT,
    CONSTRAINT fk_patients_created_by FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_patients_updated_by FOREIGN KEY (updated_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_patients_deleted_by FOREIGN KEY (deleted_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_patients_restored_by FOREIGN KEY (restored_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_patients_code CHECK (patient_code ~ '^PAT-[0-9]{8,}$'),
    CONSTRAINT ck_patients_name CHECK (char_length(btrim(full_name)) BETWEEN 1 AND 150 AND char_length(normalized_full_name) BETWEEN 1 AND 150),
    CONSTRAINT ck_patients_cccd CHECK (cccd IS NULL OR cccd ~ '^[0-9]{12}$'),
    CONSTRAINT ck_patients_health_insurance CHECK (health_insurance_number IS NULL OR health_insurance_number ~ '^[A-Z0-9]{15}$'),
    CONSTRAINT ck_patients_version CHECK (version >= 1),
    CONSTRAINT ck_patients_deletion CHECK (
        NOT is_deleted OR (deleted_at IS NOT NULL AND deleted_by_user_id IS NOT NULL
            AND char_length(btrim(deletion_reason)) BETWEEN 1 AND 1000)
    ),
    CONSTRAINT ck_patients_restore CHECK (
        (restored_at IS NULL AND restored_by_user_id IS NULL)
        OR (restored_at IS NOT NULL AND restored_by_user_id IS NOT NULL)
    )
);

CREATE TABLE specialties (
    id bigint GENERATED ALWAYS AS IDENTITY,
    code varchar(34) NOT NULL,
    name varchar(200) NOT NULL,
    normalized_name varchar(200) NOT NULL,
    description varchar(1000),
    status catalog_status_enum NOT NULL DEFAULT 'ACTIVE',
    created_by bigint,
    updated_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_specialties PRIMARY KEY (id),
    CONSTRAINT uq_specialties_code UNIQUE (code),
    CONSTRAINT uq_specialties_normalized_name UNIQUE (normalized_name),
    CONSTRAINT fk_specialties_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_specialties_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_specialties_code CHECK (code ~ '^SPC_[A-Z0-9_]{2,30}$'),
    CONSTRAINT ck_specialties_name CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    CONSTRAINT ck_specialties_version CHECK (version >= 1)
);

CREATE TABLE rooms (
    id bigint GENERATED ALWAYS AS IDENTITY,
    code varchar(23) NOT NULL,
    name varchar(200) NOT NULL,
    normalized_name varchar(200) NOT NULL,
    room_type room_type_enum NOT NULL,
    floor varchar(100),
    location_note varchar(500),
    description varchar(1000),
    status catalog_status_enum NOT NULL DEFAULT 'ACTIVE',
    created_by bigint,
    updated_by bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_rooms PRIMARY KEY (id),
    CONSTRAINT uq_rooms_code UNIQUE (code),
    CONSTRAINT uq_rooms_normalized_name UNIQUE (normalized_name),
    CONSTRAINT fk_rooms_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_rooms_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_rooms_code CHECK (code ~ '^RM-[A-Z0-9-]{2,20}$'),
    CONSTRAINT ck_rooms_name CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    CONSTRAINT ck_rooms_version CHECK (version >= 1)
);

CREATE TABLE doctor_specialties (
    id bigint GENERATED ALWAYS AS IDENTITY,
    doctor_profile_id bigint NOT NULL,
    specialty_id bigint NOT NULL,
    assigned_by bigint NOT NULL,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    removed_by bigint,
    removed_at timestamptz,
    removal_reason varchar(1000),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_doctor_specialties PRIMARY KEY (id),
    CONSTRAINT fk_doctor_specialties_doctor FOREIGN KEY (doctor_profile_id) REFERENCES doctor_profiles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_specialties_specialty FOREIGN KEY (specialty_id) REFERENCES specialties(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_specialties_assigned_by FOREIGN KEY (assigned_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_specialties_removed_by FOREIGN KEY (removed_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_doctor_specialties_version CHECK (version >= 1),
    CONSTRAINT ck_doctor_specialties_removal CHECK (
        (is_deleted = false AND removed_by IS NULL AND removed_at IS NULL AND removal_reason IS NULL)
        OR (is_deleted = true AND removed_by IS NOT NULL AND removed_at IS NOT NULL
            AND char_length(btrim(removal_reason)) BETWEEN 1 AND 1000)
    )
);

CREATE UNIQUE INDEX uq_doctor_specialties_active
    ON doctor_specialties(doctor_profile_id, specialty_id) WHERE is_deleted = false;
CREATE INDEX ix_staff_profiles_status_created ON staff_profiles(employment_status, created_at DESC, id DESC) WHERE is_deleted = false;
CREATE INDEX ix_doctor_profiles_status ON doctor_profiles(status, id) WHERE is_deleted = false;
CREATE INDEX ix_patients_list ON patients(is_deleted, created_at DESC, id DESC);
CREATE INDEX ix_patients_name_trgm ON patients USING gin(normalized_full_name gin_trgm_ops);
CREATE INDEX ix_patients_date_of_birth ON patients(date_of_birth, id) WHERE date_of_birth IS NOT NULL;
CREATE INDEX ix_specialties_active_name ON specialties(status, is_deleted, normalized_name, id);
CREATE INDEX ix_rooms_active_type_name ON rooms(status, is_deleted, room_type, normalized_name, id);
CREATE INDEX ix_doctor_specialties_specialty_active ON doctor_specialties(specialty_id, doctor_profile_id) WHERE is_deleted = false;
CREATE INDEX ix_doctor_specialties_history ON doctor_specialties(doctor_profile_id, assigned_at DESC, id DESC);

CREATE FUNCTION assign_profile_code() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'staff_profiles' THEN
        NEW.staff_code := 'STF-' || lpad(NEW.id::text, 8, '0');
    ELSIF TG_TABLE_NAME = 'doctor_profiles' THEN
        NEW.doctor_code := 'DOC-' || lpad(NEW.id::text, 8, '0');
    ELSIF TG_TABLE_NAME = 'patients' THEN
        NEW.patient_code := 'PAT-' || lpad(NEW.id::text, 8, '0');
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_staff_profiles_code BEFORE INSERT ON staff_profiles
FOR EACH ROW EXECUTE FUNCTION assign_profile_code();
CREATE TRIGGER trg_doctor_profiles_code BEFORE INSERT ON doctor_profiles
FOR EACH ROW EXECUTE FUNCTION assign_profile_code();
CREATE TRIGGER trg_patients_code BEFORE INSERT ON patients
FOR EACH ROW EXECUTE FUNCTION assign_profile_code();

CREATE FUNCTION enforce_profile_account_type() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE actual_type account_type_enum;
BEGIN
    SELECT account_type INTO actual_type FROM users WHERE id = NEW.user_id;
    IF TG_TABLE_NAME = 'staff_profiles' AND actual_type IS DISTINCT FROM 'STAFF' THEN
        RAISE EXCEPTION 'staff profile requires STAFF user' USING ERRCODE = '23514';
    END IF;
    IF TG_TABLE_NAME = 'patients' AND actual_type IS DISTINCT FROM 'PATIENT' THEN
        RAISE EXCEPTION 'patient profile requires PATIENT user' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_staff_profiles_account_type BEFORE INSERT OR UPDATE OF user_id ON staff_profiles
FOR EACH ROW EXECUTE FUNCTION enforce_profile_account_type();
CREATE TRIGGER trg_patients_account_type BEFORE INSERT OR UPDATE OF user_id ON patients
FOR EACH ROW EXECUTE FUNCTION enforce_profile_account_type();

CREATE FUNCTION reject_profile_identity_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'staff_profiles' AND (NEW.user_id <> OLD.user_id OR NEW.staff_code <> OLD.staff_code) THEN
        RAISE EXCEPTION 'staff profile identity is immutable' USING ERRCODE = '55000';
    ELSIF TG_TABLE_NAME = 'doctor_profiles' AND (NEW.staff_profile_id <> OLD.staff_profile_id OR NEW.doctor_code <> OLD.doctor_code) THEN
        RAISE EXCEPTION 'doctor profile identity is immutable' USING ERRCODE = '55000';
    ELSIF TG_TABLE_NAME = 'patients' AND (NEW.user_id <> OLD.user_id OR NEW.patient_code <> OLD.patient_code) THEN
        RAISE EXCEPTION 'patient identity is immutable' USING ERRCODE = '55000';
    ELSIF TG_TABLE_NAME = 'specialties' AND NEW.code <> OLD.code THEN
        RAISE EXCEPTION 'specialty code is immutable' USING ERRCODE = '55000';
    ELSIF TG_TABLE_NAME = 'rooms' AND NEW.code <> OLD.code THEN
        RAISE EXCEPTION 'room code is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_staff_profiles_identity BEFORE UPDATE ON staff_profiles FOR EACH ROW EXECUTE FUNCTION reject_profile_identity_change();
CREATE TRIGGER trg_doctor_profiles_identity BEFORE UPDATE ON doctor_profiles FOR EACH ROW EXECUTE FUNCTION reject_profile_identity_change();
CREATE TRIGGER trg_patients_identity BEFORE UPDATE ON patients FOR EACH ROW EXECUTE FUNCTION reject_profile_identity_change();
CREATE TRIGGER trg_specialties_code BEFORE UPDATE ON specialties FOR EACH ROW EXECUTE FUNCTION reject_profile_identity_change();
CREATE TRIGGER trg_rooms_code BEFORE UPDATE ON rooms FOR EACH ROW EXECUTE FUNCTION reject_profile_identity_change();
