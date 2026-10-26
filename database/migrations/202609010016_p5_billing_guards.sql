-- P5 financial mutation guards and read-path indexes.
CREATE TRIGGER trg_invoice_items_immutable BEFORE UPDATE OR DELETE ON invoice_items
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();

CREATE FUNCTION enforce_invoice_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.invoice_code <> OLD.invoice_code OR NEW.encounter_id <> OLD.encounter_id
       OR NEW.patient_id <> OLD.patient_id OR NEW.patient_code_at_invoice <> OLD.patient_code_at_invoice
       OR NEW.patient_name_at_invoice <> OLD.patient_name_at_invoice
       OR NEW.subtotal_vnd <> OLD.subtotal_vnd OR NEW.total_vnd <> OLD.total_vnd
       OR NEW.created_by_user_id <> OLD.created_by_user_id OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'invoice financial snapshot is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'UNPAID' OR NEW.status <> 'PAID' OR NEW.paid_at IS NULL
       OR NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'invalid invoice transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_invoices_update BEFORE UPDATE ON invoices
FOR EACH ROW EXECUTE FUNCTION enforce_invoice_update();
CREATE TRIGGER trg_invoices_no_delete BEFORE DELETE ON invoices
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();

CREATE FUNCTION enforce_payment_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.payment_code <> OLD.payment_code OR NEW.invoice_id <> OLD.invoice_id
       OR NEW.payment_method <> OLD.payment_method OR NEW.expected_amount_vnd <> OLD.expected_amount_vnd
       OR NEW.bank_account_id_at_creation IS DISTINCT FROM OLD.bank_account_id_at_creation
       OR NEW.bank_bin_at_creation IS DISTINCT FROM OLD.bank_bin_at_creation
       OR NEW.account_number_at_creation IS DISTINCT FROM OLD.account_number_at_creation
       OR NEW.account_name_at_creation IS DISTINCT FROM OLD.account_name_at_creation
       OR NEW.transfer_content IS DISTINCT FROM OLD.transfer_content
       OR NEW.qr_payload IS DISTINCT FROM OLD.qr_payload OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'payment financial snapshot is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'PENDING_CONFIRMATION' OR NEW.status <> 'CONFIRMED'
       OR NEW.version <> OLD.version + 1 OR NEW.updated_at <= OLD.updated_at THEN
        RAISE EXCEPTION 'invalid payment transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_payments_update BEFORE UPDATE ON payments
FOR EACH ROW EXECUTE FUNCTION enforce_payment_update();
CREATE TRIGGER trg_payments_no_delete BEFORE DELETE ON payments
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();

CREATE INDEX ix_invoices_created ON invoices(created_at DESC, id DESC);
CREATE INDEX ix_invoices_patient_created ON invoices(patient_id, created_at DESC, id DESC);
CREATE INDEX ix_invoices_encounter_created ON invoices(encounter_id, created_at DESC, id DESC);
CREATE INDEX ix_invoices_status_created ON invoices(status, created_at DESC, id DESC);
CREATE INDEX ix_invoice_items_invoice ON invoice_items(invoice_id, id);
CREATE INDEX ix_payments_method_status_created ON payments(payment_method, status, created_at DESC, id DESC);
CREATE INDEX ix_payments_confirmed_time ON payments(confirmed_at DESC, id DESC) WHERE status = 'CONFIRMED';
