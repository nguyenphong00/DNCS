-- Complete P4 prescription cycles and enforce immutable non-draft history.
ALTER TABLE prescriptions ADD CONSTRAINT fk_prescriptions_source_same_series
    FOREIGN KEY (source_prescription_id, series_id) REFERENCES prescriptions(id, series_id)
    ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE prescription_series ADD CONSTRAINT fk_prescription_series_current_same_series
    FOREIGN KEY (current_issued_prescription_id, id) REFERENCES prescriptions(id, series_id)
    ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION enforce_prescription_series_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.prescription_code <> OLD.prescription_code OR NEW.encounter_id <> OLD.encounter_id
       OR NEW.created_by_user_id <> OLD.created_by_user_id OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'prescription series identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at <= OLD.updated_at THEN
        RAISE EXCEPTION 'prescription series update must increment version' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_prescription_series_update BEFORE UPDATE ON prescription_series
FOR EACH ROW EXECUTE FUNCTION enforce_prescription_series_update();
CREATE TRIGGER trg_prescription_series_no_delete BEFORE DELETE ON prescription_series
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();

CREATE FUNCTION enforce_prescription_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.series_id <> OLD.series_id OR NEW.version_number <> OLD.version_number
       OR NEW.version_code <> OLD.version_code OR NEW.source_prescription_id IS DISTINCT FROM OLD.source_prescription_id
       OR NEW.created_by_user_id <> OLD.created_by_user_id OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'prescription identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'DRAFT' AND NOT (
        OLD.status = 'ISSUED' AND NEW.status = 'SUPERSEDED'
        AND NEW.overall_note IS NOT DISTINCT FROM OLD.overall_note
        AND NEW.issued_by_user_id IS NOT DISTINCT FROM OLD.issued_by_user_id
        AND NEW.issued_at IS NOT DISTINCT FROM OLD.issued_at
        AND NEW.discarded_by_user_id IS NULL AND NEW.discarded_at IS NULL AND NEW.discard_reason IS NULL
        AND NEW.superseded_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'non-draft prescription is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at <= OLD.updated_at THEN
        RAISE EXCEPTION 'prescription update must increment version' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_prescriptions_update BEFORE UPDATE ON prescriptions
FOR EACH ROW EXECUTE FUNCTION enforce_prescription_update();
CREATE TRIGGER trg_prescriptions_no_delete BEFORE DELETE ON prescriptions
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();

CREATE FUNCTION enforce_prescription_item_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent_status prescription_status_enum;
BEGIN
    SELECT status INTO parent_status FROM prescriptions WHERE id = COALESCE(NEW.prescription_id, OLD.prescription_id);
    IF parent_status <> 'DRAFT' THEN
        RAISE EXCEPTION 'items of non-draft prescription are immutable' USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'prescription items cannot be deleted' USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id <> OLD.id OR NEW.prescription_id <> OLD.prescription_id OR NEW.medicine_id <> OLD.medicine_id
           OR NEW.medicine_code_at_prescription <> OLD.medicine_code_at_prescription
           OR NEW.medicine_name_at_prescription <> OLD.medicine_name_at_prescription
           OR NEW.dosage_form_at_prescription IS DISTINCT FROM OLD.dosage_form_at_prescription
           OR NEW.strength_at_prescription IS DISTINCT FROM OLD.strength_at_prescription
           OR NEW.dispense_unit_at_prescription <> OLD.dispense_unit_at_prescription OR NEW.created_at <> OLD.created_at THEN
            RAISE EXCEPTION 'prescription item identity and snapshot are immutable' USING ERRCODE = '55000';
        END IF;
        IF OLD.is_removed OR NEW.version <> OLD.version + 1 OR NEW.updated_at <= OLD.updated_at THEN
            RAISE EXCEPTION 'invalid prescription item update' USING ERRCODE = '55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_prescription_items_mutation BEFORE INSERT OR UPDATE OR DELETE ON prescription_items
FOR EACH ROW EXECUTE FUNCTION enforce_prescription_item_mutation();
