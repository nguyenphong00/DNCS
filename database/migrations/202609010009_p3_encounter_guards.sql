-- Complete the Appointment/Encounter cycle and cross-table P3 invariants.
ALTER TABLE appointments
    ADD CONSTRAINT fk_appointments_follow_up_encounter
    FOREIGN KEY (follow_up_from_encounter_id) REFERENCES medical_encounters(id) ON DELETE RESTRICT;

CREATE FUNCTION enforce_service_price_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.service_id <> OLD.service_id
       OR NEW.effective_from <> OLD.effective_from OR NEW.unit_price_vnd <> OLD.unit_price_vnd
       OR NEW.currency <> OLD.currency OR NEW.created_by_user_id <> OLD.created_by_user_id
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'service price version is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.cancelled_at IS NOT NULL OR NEW.cancelled_at IS NULL
       OR OLD.effective_from <= now()
       OR EXISTS (SELECT 1 FROM encounter_services es
                  WHERE es.service_price_version_id_at_order = OLD.id) THEN
        RAISE EXCEPTION 'service price version is not cancellable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_service_price_versions_update
BEFORE UPDATE ON service_price_versions
FOR EACH ROW EXECUTE FUNCTION enforce_service_price_update();

CREATE FUNCTION validate_encounter_service_assignment() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    encounter_status encounter_status_enum;
    encounter_specialty bigint;
    encounter_doctor bigint;
    catalog_type service_type_enum;
    catalog_specialty bigint;
    catalog_status catalog_status_enum;
    catalog_deleted boolean;
    price_effective timestamptz;
    price_cancelled timestamptz;
    actor_doctor bigint;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.service_id <> OLD.service_id
           OR NEW.service_price_version_id_at_order <> OLD.service_price_version_id_at_order
           OR NEW.service_code_at_order <> OLD.service_code_at_order
           OR NEW.service_name_at_order <> OLD.service_name_at_order
           OR NEW.unit_label_at_order <> OLD.unit_label_at_order
           OR NEW.source <> OLD.source OR NEW.assigned_by_user_id IS DISTINCT FROM OLD.assigned_by_user_id
           OR NEW.assigned_at <> OLD.assigned_at THEN
            RAISE EXCEPTION 'encounter service order identity is immutable' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;

    SELECT status, specialty_id, doctor_profile_id
      INTO encounter_status, encounter_specialty, encounter_doctor
      FROM medical_encounters WHERE id = NEW.encounter_id;
    SELECT service_type, specialty_id, status, is_deleted
      INTO catalog_type, catalog_specialty, catalog_status, catalog_deleted
      FROM services WHERE id = NEW.service_id;
    SELECT effective_from, cancelled_at INTO price_effective, price_cancelled
      FROM service_price_versions
      WHERE id = NEW.service_price_version_id_at_order AND service_id = NEW.service_id;

    IF encounter_status IS DISTINCT FROM 'DRAFT' OR catalog_status IS DISTINCT FROM 'ACTIVE'
       OR catalog_deleted OR price_effective IS NULL OR price_effective > NEW.assigned_at
       OR price_cancelled IS NOT NULL THEN
        RAISE EXCEPTION 'invalid encounter service assignment' USING ERRCODE = '23514';
    END IF;
    IF NEW.source = 'SYSTEM_CONSULTATION' THEN
        IF catalog_type <> 'CONSULTATION' OR catalog_specialty IS DISTINCT FROM encounter_specialty THEN
            RAISE EXCEPTION 'consultation service does not match encounter specialty' USING ERRCODE = '23514';
        END IF;
    ELSE
        IF catalog_type = 'CONSULTATION' THEN
            RAISE EXCEPTION 'doctor order cannot assign consultation service' USING ERRCODE = '23514';
        END IF;
        SELECT d.id INTO actor_doctor
          FROM doctor_profiles d JOIN staff_profiles sp ON sp.id = d.staff_profile_id
          WHERE d.id = encounter_doctor AND sp.user_id = NEW.assigned_by_user_id;
        IF actor_doctor IS NULL THEN
            RAISE EXCEPTION 'service order actor is not assigned doctor' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_encounter_services_assignment
BEFORE INSERT OR UPDATE ON encounter_services
FOR EACH ROW EXECUTE FUNCTION validate_encounter_service_assignment();
