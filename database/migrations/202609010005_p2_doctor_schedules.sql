-- P2 Doctor Scheduling with database-enforced overlap protection.
CREATE TABLE doctor_schedules (
    id bigint GENERATED ALWAYS AS IDENTITY,
    doctor_profile_id bigint NOT NULL,
    room_id bigint NOT NULL,
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    schedule_range tstzrange GENERATED ALWAYS AS (tstzrange(start_at, end_at, '[)')) STORED,
    slot_duration_minutes smallint NOT NULL,
    status doctor_schedule_status_enum NOT NULL DEFAULT 'DRAFT',
    notes varchar(1000),
    cancellation_reason varchar(1000),
    cancelled_by bigint,
    cancelled_at timestamptz,
    created_by bigint NOT NULL,
    updated_by bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    deleted_by bigint,
    deleted_at timestamptz,
    CONSTRAINT pk_doctor_schedules PRIMARY KEY (id),
    CONSTRAINT uq_doctor_schedules_snapshot UNIQUE (id, doctor_profile_id, room_id),
    CONSTRAINT fk_doctor_schedules_doctor FOREIGN KEY (doctor_profile_id) REFERENCES doctor_profiles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_schedules_room FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_schedules_cancelled_by FOREIGN KEY (cancelled_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_schedules_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_schedules_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_doctor_schedules_deleted_by FOREIGN KEY (deleted_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_doctor_schedules_time CHECK (start_at < end_at),
    CONSTRAINT ck_doctor_schedules_duration CHECK (slot_duration_minutes IN (15,20,30,45,60)),
    CONSTRAINT ck_doctor_schedules_divisible CHECK (
        extract(epoch FROM (end_at - start_at))::bigint % (slot_duration_minutes * 60) = 0
    ),
    CONSTRAINT ck_doctor_schedules_cancel_metadata CHECK (
        (status <> 'CANCELLED' AND cancelled_by IS NULL AND cancelled_at IS NULL AND cancellation_reason IS NULL)
        OR (status = 'CANCELLED' AND cancelled_by IS NOT NULL AND cancelled_at IS NOT NULL
            AND char_length(btrim(cancellation_reason)) BETWEEN 1 AND 1000)
    ),
    CONSTRAINT ck_doctor_schedules_delete_metadata CHECK (
        (is_deleted = false AND deleted_by IS NULL AND deleted_at IS NULL)
        OR (is_deleted = true AND deleted_by IS NOT NULL AND deleted_at IS NOT NULL)
    ),
    CONSTRAINT ck_doctor_schedules_active_not_deleted CHECK (status <> 'ACTIVE' OR is_deleted = false),
    CONSTRAINT ck_doctor_schedules_version CHECK (version >= 1),
    CONSTRAINT ex_doctor_schedules_doctor_overlap EXCLUDE USING gist (
        doctor_profile_id WITH =, schedule_range WITH &&
    ) WHERE (status = 'ACTIVE'),
    CONSTRAINT ex_doctor_schedules_room_overlap EXCLUDE USING gist (
        room_id WITH =, schedule_range WITH &&
    ) WHERE (status = 'ACTIVE')
);

CREATE INDEX ix_doctor_schedules_doctor_start ON doctor_schedules(doctor_profile_id, start_at, id);
CREATE INDEX ix_doctor_schedules_room_start ON doctor_schedules(room_id, start_at, id);
CREATE INDEX ix_doctor_schedules_admin ON doctor_schedules(status, is_deleted, start_at, end_at, id);
