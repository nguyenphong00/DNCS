-- P5 cross-aggregate relations that cannot be expressed by the base row checks alone.
ALTER TABLE medical_encounters
    ADD CONSTRAINT uq_medical_encounters_id_patient UNIQUE (id, patient_id);
ALTER TABLE invoices
    ADD CONSTRAINT fk_invoices_encounter_patient
    FOREIGN KEY (encounter_id, patient_id) REFERENCES medical_encounters(id, patient_id) ON DELETE RESTRICT;

ALTER TABLE invoices
    ADD CONSTRAINT uq_invoices_id_total UNIQUE (id, total_vnd);
ALTER TABLE payments
    ADD CONSTRAINT fk_payments_invoice_amount
    FOREIGN KEY (invoice_id, expected_amount_vnd) REFERENCES invoices(id, total_vnd) ON DELETE RESTRICT;

CREATE FUNCTION enforce_invoice_insert_eligibility() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    encounter_patient bigint;
    encounter_state encounter_status_enum;
BEGIN
    SELECT patient_id, status INTO encounter_patient, encounter_state
      FROM medical_encounters WHERE id = NEW.encounter_id;
    IF NOT FOUND OR encounter_patient <> NEW.patient_id OR encounter_state <> 'FINALIZED' THEN
        RAISE EXCEPTION 'invoice requires matching finalized encounter and patient' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_invoices_insert_eligibility BEFORE INSERT ON invoices
FOR EACH ROW EXECUTE FUNCTION enforce_invoice_insert_eligibility();

CREATE FUNCTION enforce_invoice_item_source_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    invoice_encounter bigint;
    invoice_state invoice_status_enum;
    source_encounter bigint;
    source_state text;
    source_price bigint;
    source_prescription bigint;
    current_prescription bigint;
    source_medicine bigint;
    price_medicine bigint;
    item_removed boolean;
    source_found boolean;
    price_found boolean;
BEGIN
    SELECT encounter_id, status INTO invoice_encounter, invoice_state
      FROM invoices WHERE id = NEW.invoice_id;
    IF NOT FOUND OR invoice_state <> 'UNPAID' THEN
        RAISE EXCEPTION 'invoice item requires an unpaid invoice' USING ERRCODE = '23514';
    END IF;

    IF NEW.item_type = 'SERVICE' THEN
        SELECT encounter_id, status::text, service_price_version_id_at_order
          INTO source_encounter, source_state, source_price
          FROM encounter_services WHERE id = NEW.encounter_service_id;
		source_found := FOUND;
        IF NOT source_found OR source_encounter <> invoice_encounter OR source_state <> 'ACTIVE'
           OR source_price <> NEW.service_price_version_id THEN
            RAISE EXCEPTION 'invalid service source for invoice' USING ERRCODE = '23514';
        END IF;
    ELSE
        SELECT ps.encounter_id, p.status::text, pi.prescription_id,
               ps.current_issued_prescription_id, pi.medicine_id, pi.is_removed
          INTO source_encounter, source_state, source_prescription,
               current_prescription, source_medicine, item_removed
          FROM prescription_items pi
          JOIN prescriptions p ON p.id = pi.prescription_id
          JOIN prescription_series ps ON ps.id = p.series_id
         WHERE pi.id = NEW.prescription_item_id;
		source_found := FOUND;
        SELECT medicine_id INTO price_medicine
          FROM medicine_price_versions WHERE id = NEW.medicine_price_version_id;
		price_found := FOUND;
        IF NOT source_found OR NOT price_found OR source_encounter <> invoice_encounter OR source_state <> 'ISSUED'
           OR source_prescription <> NEW.prescription_id
           OR current_prescription <> NEW.prescription_id OR item_removed
           OR price_medicine IS DISTINCT FROM source_medicine THEN
            RAISE EXCEPTION 'invalid medicine source for invoice' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_invoice_items_source_scope BEFORE INSERT ON invoice_items
FOR EACH ROW EXECUTE FUNCTION enforce_invoice_item_source_scope();

CREATE FUNCTION enforce_invoice_aggregate_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    item_count integer;
    payment_count integer;
BEGIN
    SELECT count(*) INTO item_count FROM invoice_items WHERE invoice_id = NEW.id;
    SELECT count(*) INTO payment_count FROM payments WHERE invoice_id = NEW.id;
    IF item_count < 1 OR payment_count <> 1 THEN
        RAISE EXCEPTION 'invoice must commit with items and exactly one payment' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_invoice_aggregate_complete
AFTER INSERT ON invoices DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_invoice_aggregate_complete();
