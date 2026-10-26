-- P4 immutable inventory documents, batch balances and stock ledger.
CREATE TYPE inventory_adjustment_reason_enum AS ENUM ('COUNT_CORRECTION','DAMAGE','EXPIRED_DISPOSAL','OTHER');
CREATE TYPE inventory_direction_enum AS ENUM ('IN','OUT');
CREATE TYPE stock_movement_type_enum AS ENUM ('RECEIPT_IN','SALE_OUT','ADJUSTMENT_IN','ADJUSTMENT_OUT');
CREATE TYPE stock_reference_type_enum AS ENUM ('RECEIPT_ITEM','INVOICE_ITEM','ADJUSTMENT_ITEM');
CREATE SEQUENCE stock_receipt_code_seq AS bigint NO CYCLE;
CREATE SEQUENCE inventory_adjustment_code_seq AS bigint NO CYCLE;

CREATE TABLE stock_receipts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    receipt_code varchar(32) NOT NULL UNIQUE,
    supplier_name varchar(200),
    supplier_document_ref varchar(100),
    received_at timestamptz NOT NULL,
    note varchar(1000),
    received_by_user_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_stock_receipt_fields CHECK (
        (supplier_name IS NULL OR char_length(supplier_name) BETWEEN 1 AND 200)
        AND (supplier_document_ref IS NULL OR char_length(supplier_document_ref) BETWEEN 1 AND 100)
        AND (note IS NULL OR char_length(note) BETWEEN 1 AND 1000)
        AND received_at <= created_at
    )
);

CREATE TABLE medicine_batches (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    medicine_id bigint NOT NULL REFERENCES medicines(id) ON DELETE RESTRICT,
    lot_number varchar(100) NOT NULL,
    manufacture_date date,
    expires_on date NOT NULL,
    initial_received_quantity numeric(14,3) NOT NULL,
    unit_purchase_cost_vnd numeric(14,0) NOT NULL,
    quantity_on_hand numeric(14,3) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT uq_medicine_batches_lot UNIQUE (medicine_id, lot_number),
    CONSTRAINT uq_medicine_batches_id_medicine UNIQUE (id, medicine_id),
    CONSTRAINT ck_medicine_batches_dates CHECK (manufacture_date IS NULL OR manufacture_date <= expires_on),
    CONSTRAINT ck_medicine_batches_quantity CHECK (
        initial_received_quantity > 0 AND unit_purchase_cost_vnd >= 0 AND quantity_on_hand >= 0 AND version >= 1
    )
);

CREATE TABLE stock_receipt_items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    stock_receipt_id bigint NOT NULL REFERENCES stock_receipts(id) ON DELETE RESTRICT,
    medicine_id bigint NOT NULL REFERENCES medicines(id) ON DELETE RESTRICT,
    medicine_batch_id bigint NOT NULL UNIQUE,
    lot_number varchar(100) NOT NULL,
    manufacture_date date,
    expires_on date NOT NULL,
    received_quantity numeric(14,3) NOT NULL,
    unit_purchase_cost_vnd numeric(14,0) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT fk_stock_receipt_item_batch FOREIGN KEY (medicine_batch_id, medicine_id)
        REFERENCES medicine_batches(id, medicine_id) ON DELETE RESTRICT,
    CONSTRAINT ck_stock_receipt_item_values CHECK (
        char_length(btrim(lot_number)) BETWEEN 1 AND 100
        AND (manufacture_date IS NULL OR manufacture_date <= expires_on)
        AND received_quantity > 0 AND unit_purchase_cost_vnd >= 0
    )
);

CREATE TABLE inventory_adjustments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    adjustment_code varchar(32) NOT NULL UNIQUE,
    reason_code inventory_adjustment_reason_enum NOT NULL,
    reason_detail varchar(500) NOT NULL,
    correction_of_adjustment_id bigint REFERENCES inventory_adjustments(id) ON DELETE RESTRICT,
    adjusted_by_user_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    adjusted_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_inventory_adjustment_reason CHECK (char_length(btrim(reason_detail)) BETWEEN 1 AND 500),
    CONSTRAINT ck_inventory_adjustment_correction CHECK (correction_of_adjustment_id IS NULL OR correction_of_adjustment_id <> id)
);

CREATE TABLE inventory_adjustment_items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    inventory_adjustment_id bigint NOT NULL REFERENCES inventory_adjustments(id) ON DELETE RESTRICT,
    medicine_batch_id bigint NOT NULL REFERENCES medicine_batches(id) ON DELETE RESTRICT,
    direction inventory_direction_enum NOT NULL,
    quantity numeric(14,3) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_adjustment_item_quantity CHECK (quantity > 0)
);

CREATE TABLE stock_movements (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    medicine_batch_id bigint NOT NULL,
    medicine_id bigint NOT NULL REFERENCES medicines(id) ON DELETE RESTRICT,
    movement_type stock_movement_type_enum NOT NULL,
    quantity_delta numeric(14,3) NOT NULL,
    quantity_before numeric(14,3) NOT NULL,
    quantity_after numeric(14,3) NOT NULL,
    reference_type stock_reference_type_enum NOT NULL,
    reference_id bigint NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    created_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT fk_stock_movement_batch FOREIGN KEY (medicine_batch_id, medicine_id)
        REFERENCES medicine_batches(id, medicine_id) ON DELETE RESTRICT,
    CONSTRAINT uq_stock_movements_allocation UNIQUE (medicine_batch_id, reference_type, reference_id),
    CONSTRAINT ck_stock_movements_sign CHECK (
        (movement_type IN ('RECEIPT_IN','ADJUSTMENT_IN') AND quantity_delta > 0)
        OR (movement_type IN ('SALE_OUT','ADJUSTMENT_OUT') AND quantity_delta < 0)
    ),
    CONSTRAINT ck_stock_movements_balance CHECK (
        quantity_before >= 0 AND quantity_after >= 0 AND quantity_after = quantity_before + quantity_delta
    ),
    CONSTRAINT ck_stock_movements_reference_type CHECK (
        (movement_type = 'RECEIPT_IN' AND reference_type = 'RECEIPT_ITEM')
        OR (movement_type = 'SALE_OUT' AND reference_type = 'INVOICE_ITEM')
        OR (movement_type IN ('ADJUSTMENT_IN','ADJUSTMENT_OUT') AND reference_type = 'ADJUSTMENT_ITEM')
    )
);

CREATE INDEX ix_batches_fefo ON medicine_batches(medicine_id, expires_on, id) WHERE quantity_on_hand > 0;
CREATE INDEX ix_batches_expiry ON medicine_batches(expires_on, id) WHERE quantity_on_hand > 0;
CREATE INDEX ix_receipts_time ON stock_receipts(received_at DESC, id DESC);
CREATE INDEX ix_receipt_items_header ON stock_receipt_items(stock_receipt_id, id);
CREATE INDEX ix_receipt_items_medicine_expiry ON stock_receipt_items(medicine_id, expires_on, id);
CREATE INDEX ix_adjustments_time ON inventory_adjustments(adjusted_at DESC, id DESC);
CREATE INDEX ix_adjustment_items_header ON inventory_adjustment_items(inventory_adjustment_id, id);
CREATE INDEX ix_adjustment_items_batch ON inventory_adjustment_items(medicine_batch_id, id);
CREATE INDEX ix_stock_movements_batch_time ON stock_movements(medicine_batch_id, occurred_at DESC, id DESC);
CREATE INDEX ix_stock_movements_medicine_time ON stock_movements(medicine_id, occurred_at DESC, id DESC);
CREATE INDEX ix_stock_movements_reference ON stock_movements(reference_type, reference_id, id);

CREATE FUNCTION assign_inventory_document_code() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'stock_receipts' THEN
        NEW.receipt_code := 'RCV-' || to_char(NEW.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh','YYYYMMDD')
            || '-' || lpad(nextval('stock_receipt_code_seq')::text, 8, '0');
    ELSE
        NEW.adjustment_code := 'ADJ-' || to_char(NEW.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh','YYYYMMDD')
            || '-' || lpad(nextval('inventory_adjustment_code_seq')::text, 8, '0');
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_stock_receipts_code BEFORE INSERT ON stock_receipts FOR EACH ROW EXECUTE FUNCTION assign_inventory_document_code();
CREATE TRIGGER trg_inventory_adjustments_code BEFORE INSERT ON inventory_adjustments FOR EACH ROW EXECUTE FUNCTION assign_inventory_document_code();

CREATE TRIGGER trg_stock_receipts_immutable BEFORE UPDATE OR DELETE ON stock_receipts FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
CREATE TRIGGER trg_stock_receipt_items_immutable BEFORE UPDATE OR DELETE ON stock_receipt_items FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
CREATE TRIGGER trg_inventory_adjustments_immutable BEFORE UPDATE OR DELETE ON inventory_adjustments FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
CREATE TRIGGER trg_inventory_adjustment_items_immutable BEFORE UPDATE OR DELETE ON inventory_adjustment_items FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
CREATE TRIGGER trg_stock_movements_immutable BEFORE UPDATE OR DELETE ON stock_movements FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();

CREATE FUNCTION enforce_batch_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.medicine_id <> OLD.medicine_id OR NEW.lot_number <> OLD.lot_number
       OR NEW.manufacture_date IS DISTINCT FROM OLD.manufacture_date OR NEW.expires_on <> OLD.expires_on
       OR NEW.initial_received_quantity <> OLD.initial_received_quantity
       OR NEW.unit_purchase_cost_vnd <> OLD.unit_purchase_cost_vnd OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'batch identity and receipt snapshot are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at <= OLD.updated_at OR NEW.quantity_on_hand = OLD.quantity_on_hand THEN
        RAISE EXCEPTION 'batch balance update must change quantity and increment version' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_medicine_batches_update BEFORE UPDATE ON medicine_batches FOR EACH ROW EXECUTE FUNCTION enforce_batch_update();
CREATE TRIGGER trg_medicine_batches_no_delete BEFORE DELETE ON medicine_batches FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
