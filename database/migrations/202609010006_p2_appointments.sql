-- P2 Appointment lifecycle, history, idempotency and three-way overlap protection.
CREATE TABLE appointments (
    id bigint GENERATED ALWAYS AS IDENTITY,
    appointment_code varchar(32) NOT NULL,
    doctor_schedule_id bigint NOT NULL,
    patient_id bigint NOT NULL,
    doctor_profile_id bigint NOT NULL,
    specialty_id bigint NOT NULL,
    room_id bigint NOT NULL,
    doctor_code_at_booking varchar(32) NOT NULL,
    doctor_name_at_booking varchar(150) NOT NULL,
    specialty_code_at_booking varchar(34) NOT NULL,
    specialty_name_at_booking varchar(200) NOT NULL,
    room_code_at_booking varchar(23) NOT NULL,
    room_name_at_booking varchar(200) NOT NULL,
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    appointment_range tstzrange GENERATED ALWAYS AS (tstzrange(start_at, end_at, '[)')) STORED,
    status appointment_status_enum NOT NULL,
    source appointment_source_enum NOT NULL,
    approval_mode_at_booking appointment_approval_mode_enum NOT NULL,
    check_in_early_minutes_at_booking smallint NOT NULL,
    check_in_late_minutes_at_booking smallint NOT NULL,
    no_show_grace_minutes_at_booking smallint NOT NULL,
    booked_by_user_id bigint NOT NULL,
    follow_up_from_encounter_id bigint,
    idempotency_key varchar(128) NOT NULL,
    idempotency_request_hash char(64) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    is_deleted boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_appointments PRIMARY KEY (id),
    CONSTRAINT uq_appointments_code UNIQUE (appointment_code),
    CONSTRAINT uq_appointments_booking_idempotency UNIQUE (booked_by_user_id, idempotency_key),
    CONSTRAINT fk_appointments_schedule_snapshot FOREIGN KEY (doctor_schedule_id, doctor_profile_id, room_id)
        REFERENCES doctor_schedules(id, doctor_profile_id, room_id) ON DELETE RESTRICT,
    CONSTRAINT fk_appointments_patient FOREIGN KEY (patient_id) REFERENCES patients(id) ON DELETE RESTRICT,
    CONSTRAINT fk_appointments_doctor FOREIGN KEY (doctor_profile_id) REFERENCES doctor_profiles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_appointments_specialty FOREIGN KEY (specialty_id) REFERENCES specialties(id) ON DELETE RESTRICT,
    CONSTRAINT fk_appointments_room FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE RESTRICT,
    CONSTRAINT fk_appointments_booked_by FOREIGN KEY (booked_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_appointments_time CHECK (start_at < end_at),
    CONSTRAINT ck_appointments_source CHECK (
        (source = 'FOLLOW_UP' AND follow_up_from_encounter_id IS NOT NULL)
        OR (source <> 'FOLLOW_UP' AND follow_up_from_encounter_id IS NULL)
    ),
    CONSTRAINT ck_appointments_window_snapshot CHECK (
        check_in_early_minutes_at_booking BETWEEN 0 AND 240
        AND check_in_late_minutes_at_booking BETWEEN 0 AND 240
        AND no_show_grace_minutes_at_booking BETWEEN 0 AND check_in_late_minutes_at_booking
    ),
    CONSTRAINT ck_appointments_holding_not_deleted CHECK (
        status NOT IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS') OR is_deleted = false
    ),
    CONSTRAINT ck_appointments_idempotency CHECK (
        char_length(idempotency_key) BETWEEN 16 AND 128
        AND octet_length(idempotency_key) = char_length(idempotency_key)
        AND idempotency_key ~ '^[ -~]+$'
        AND idempotency_key = btrim(idempotency_key)
        AND idempotency_request_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT ck_appointments_version CHECK (version >= 1),
    CONSTRAINT ex_appointments_doctor_overlap EXCLUDE USING gist (
        doctor_profile_id WITH =, appointment_range WITH &&
    ) WHERE (status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS')),
    CONSTRAINT ex_appointments_patient_overlap EXCLUDE USING gist (
        patient_id WITH =, appointment_range WITH &&
    ) WHERE (status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS')),
    CONSTRAINT ex_appointments_room_overlap EXCLUDE USING gist (
        room_id WITH =, appointment_range WITH &&
    ) WHERE (status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS'))
);

CREATE UNIQUE INDEX uq_appointments_follow_up_holding
    ON appointments(follow_up_from_encounter_id)
    WHERE follow_up_from_encounter_id IS NOT NULL
      AND status IN ('PENDING_APPROVAL','CONFIRMED','CHECKED_IN','IN_PROGRESS');

CREATE TABLE appointment_status_histories (
    id bigint GENERATED ALWAYS AS IDENTITY,
    appointment_id bigint NOT NULL,
    from_status appointment_status_enum,
    to_status appointment_status_enum NOT NULL,
    actor_user_id bigint,
    actor_type appointment_actor_type_enum NOT NULL,
    reason varchar(1000),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_appointment_status_histories PRIMARY KEY (id),
    CONSTRAINT fk_appointment_history_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id) ON DELETE RESTRICT,
    CONSTRAINT fk_appointment_history_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_appointment_history_actor CHECK (
        (actor_type = 'USER' AND actor_user_id IS NOT NULL)
        OR (actor_type = 'SYSTEM' AND actor_user_id IS NULL)
    ),
    CONSTRAINT ck_appointment_history_reason CHECK (
        reason IS NULL OR char_length(btrim(reason)) BETWEEN 1 AND 1000
    ),
    CONSTRAINT ck_appointment_history_metadata CHECK (pg_column_size(metadata) <= 16384)
);

CREATE INDEX ix_appointments_patient_start ON appointments(patient_id, start_at DESC, id DESC);
CREATE INDEX ix_appointments_doctor_start ON appointments(doctor_profile_id, start_at, id);
CREATE INDEX ix_appointments_schedule_start ON appointments(doctor_schedule_id, start_at, id);
CREATE INDEX ix_appointments_status_start ON appointments(status, start_at, id);
CREATE INDEX ix_appointments_room_start ON appointments(room_id, start_at, id);
CREATE INDEX ix_appointments_specialty_start ON appointments(specialty_id, start_at, id);
CREATE INDEX ix_appointments_follow_up_source ON appointments(follow_up_from_encounter_id, id) WHERE follow_up_from_encounter_id IS NOT NULL;
CREATE INDEX ix_appointment_history_timeline ON appointment_status_histories(appointment_id, created_at, id);

CREATE FUNCTION assign_appointment_code() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.appointment_code := 'APT-' || to_char(NEW.start_at AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYYYMMDD')
        || '-' || lpad(nextval('appointment_code_seq')::text, 8, '0');
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_appointments_code BEFORE INSERT ON appointments
FOR EACH ROW EXECUTE FUNCTION assign_appointment_code();

CREATE TRIGGER trg_appointment_status_histories_immutable
BEFORE UPDATE OR DELETE ON appointment_status_histories
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
