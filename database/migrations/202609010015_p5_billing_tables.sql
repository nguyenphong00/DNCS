-- P5 immutable invoice snapshots and one-payment aggregate.
CREATE TYPE invoice_status_enum AS ENUM ('UNPAID', 'PAID');
CREATE TYPE invoice_item_type_enum AS ENUM ('SERVICE', 'MEDICINE');
CREATE TYPE payment_method_enum AS ENUM ('CASH', 'BANK_TRANSFER');
CREATE TYPE payment_status_enum AS ENUM ('PENDING_CONFIRMATION', 'CONFIRMED');
CREATE SEQUENCE invoice_code_seq AS bigint NO CYCLE;
CREATE SEQUENCE payment_code_seq AS bigint NO CYCLE;

CREATE TABLE invoices (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    invoice_code varchar(32) NOT NULL UNIQUE,
    encounter_id bigint NOT NULL REFERENCES medical_encounters(id) ON DELETE RESTRICT,
    patient_id bigint NOT NULL REFERENCES patients(id) ON DELETE RESTRICT,
    patient_code_at_invoice varchar(32) NOT NULL,
    patient_name_at_invoice varchar(150) NOT NULL,
    status invoice_status_enum NOT NULL DEFAULT 'UNPAID',
    subtotal_vnd numeric(14,0) NOT NULL,
    total_vnd numeric(14,0) NOT NULL,
    created_by_user_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    paid_at timestamptz,
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT ck_invoices_snapshot CHECK (
        char_length(btrim(patient_code_at_invoice)) BETWEEN 1 AND 32
        AND char_length(btrim(patient_name_at_invoice)) BETWEEN 1 AND 150
    ),
    CONSTRAINT ck_invoices_amount CHECK (
        subtotal_vnd >= 0 AND total_vnd >= 0 AND subtotal_vnd = total_vnd
    ),
    CONSTRAINT ck_invoices_state CHECK (
        (status = 'UNPAID' AND paid_at IS NULL)
        OR (status = 'PAID' AND paid_at IS NOT NULL)
    ),
    CONSTRAINT ck_invoices_version CHECK (version >= 1)
);
CREATE UNIQUE INDEX uq_invoices_one_unpaid_encounter
    ON invoices(encounter_id) WHERE status = 'UNPAID';

CREATE TABLE invoice_items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    invoice_id bigint NOT NULL REFERENCES invoices(id) ON DELETE RESTRICT,
    item_type invoice_item_type_enum NOT NULL,
    encounter_service_id bigint REFERENCES encounter_services(id) ON DELETE RESTRICT,
    prescription_item_id bigint REFERENCES prescription_items(id) ON DELETE RESTRICT,
    prescription_id bigint REFERENCES prescriptions(id) ON DELETE RESTRICT,
    service_price_version_id bigint REFERENCES service_price_versions(id) ON DELETE RESTRICT,
    medicine_price_version_id bigint REFERENCES medicine_price_versions(id) ON DELETE RESTRICT,
    item_code_at_invoice varchar(34) NOT NULL,
    item_name_at_invoice varchar(200) NOT NULL,
    dosage_form_at_invoice varchar(200),
    strength_at_invoice varchar(200),
    unit_label_at_invoice varchar(50) NOT NULL,
    quantity smallint NOT NULL,
    unit_price_snapshot_vnd numeric(14,0) NOT NULL,
    line_total_vnd numeric(14,0) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_invoice_items_source CHECK (
        (item_type = 'SERVICE' AND encounter_service_id IS NOT NULL AND service_price_version_id IS NOT NULL
            AND prescription_item_id IS NULL AND prescription_id IS NULL AND medicine_price_version_id IS NULL
            AND dosage_form_at_invoice IS NULL AND strength_at_invoice IS NULL)
        OR
        (item_type = 'MEDICINE' AND encounter_service_id IS NULL AND service_price_version_id IS NULL
            AND prescription_item_id IS NOT NULL AND prescription_id IS NOT NULL AND medicine_price_version_id IS NOT NULL)
    ),
    CONSTRAINT ck_invoice_items_snapshot CHECK (
        char_length(btrim(item_code_at_invoice)) BETWEEN 1 AND 34
        AND char_length(btrim(item_name_at_invoice)) BETWEEN 1 AND 200
        AND char_length(btrim(unit_label_at_invoice)) BETWEEN 1 AND 50
        AND quantity BETWEEN 1 AND 999
        AND unit_price_snapshot_vnd >= 0
        AND line_total_vnd = unit_price_snapshot_vnd * quantity
    )
);
CREATE UNIQUE INDEX uq_invoice_items_service_source
    ON invoice_items(encounter_service_id) WHERE encounter_service_id IS NOT NULL;
CREATE UNIQUE INDEX uq_invoice_items_medicine_source
    ON invoice_items(prescription_item_id) WHERE prescription_item_id IS NOT NULL;

CREATE TABLE payments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payment_code varchar(32) NOT NULL UNIQUE,
    invoice_id bigint NOT NULL UNIQUE REFERENCES invoices(id) ON DELETE RESTRICT,
    payment_method payment_method_enum NOT NULL,
    status payment_status_enum NOT NULL DEFAULT 'PENDING_CONFIRMATION',
    expected_amount_vnd numeric(14,0) NOT NULL,
    bank_account_id_at_creation bigint REFERENCES bank_accounts(id) ON DELETE RESTRICT,
    bank_bin_at_creation char(6),
    account_number_at_creation varchar(30),
    account_name_at_creation varchar(200),
    transfer_content varchar(100),
    qr_payload text,
    bank_transaction_reference varchar(100),
    bank_transaction_reference_normalized varchar(100),
    external_reference varchar(100),
    confirmed_amount_vnd numeric(14,0),
    confirmed_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    confirmed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT ck_payments_amount CHECK (expected_amount_vnd >= 0 AND version >= 1),
    CONSTRAINT ck_payments_method_snapshot CHECK (
        (payment_method = 'CASH' AND bank_account_id_at_creation IS NULL AND bank_bin_at_creation IS NULL
            AND account_number_at_creation IS NULL AND account_name_at_creation IS NULL
            AND transfer_content IS NULL AND qr_payload IS NULL)
        OR
        (payment_method = 'BANK_TRANSFER' AND bank_account_id_at_creation IS NOT NULL
            AND bank_bin_at_creation IS NOT NULL AND account_number_at_creation IS NOT NULL
            AND account_name_at_creation IS NOT NULL AND transfer_content IS NOT NULL AND qr_payload IS NOT NULL)
    ),
    CONSTRAINT ck_payments_confirmation CHECK (
        (status = 'PENDING_CONFIRMATION' AND bank_transaction_reference IS NULL
            AND bank_transaction_reference_normalized IS NULL AND external_reference IS NULL
            AND confirmed_amount_vnd IS NULL AND confirmed_by_user_id IS NULL AND confirmed_at IS NULL)
        OR
        (status = 'CONFIRMED' AND confirmed_amount_vnd = expected_amount_vnd
            AND confirmed_by_user_id IS NOT NULL AND confirmed_at IS NOT NULL
            AND ((payment_method = 'CASH' AND bank_transaction_reference IS NULL
                    AND bank_transaction_reference_normalized IS NULL)
                OR (payment_method = 'BANK_TRANSFER' AND bank_transaction_reference IS NOT NULL
                    AND bank_transaction_reference_normalized IS NOT NULL)))
    )
);

CREATE UNIQUE INDEX uq_payments_confirmed_bank_reference
    ON payments(bank_account_id_at_creation, bank_transaction_reference_normalized)
    WHERE status = 'CONFIRMED' AND payment_method = 'BANK_TRANSFER';

CREATE FUNCTION assign_invoice_code() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.invoice_code := 'INV-' || to_char(NEW.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh','YYYYMMDD')
        || '-' || lpad(nextval('invoice_code_seq')::text, 8, '0');
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_invoices_code BEFORE INSERT ON invoices FOR EACH ROW EXECUTE FUNCTION assign_invoice_code();

CREATE FUNCTION assign_payment_code() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.payment_code := 'PAY-' || to_char(NEW.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh','YYYYMMDD')
        || '-' || lpad(nextval('payment_code_seq')::text, 8, '0');
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_payments_code BEFORE INSERT ON payments FOR EACH ROW EXECUTE FUNCTION assign_payment_code();
