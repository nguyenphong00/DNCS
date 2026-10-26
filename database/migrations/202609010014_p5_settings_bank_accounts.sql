-- P5 typed settings guards and receiving bank-account lifecycle.
CREATE TYPE bank_account_status_enum AS ENUM ('ACTIVE', 'INACTIVE');

CREATE TABLE bank_accounts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bank_bin char(6) NOT NULL,
    bank_name varchar(200) NOT NULL,
    account_number varchar(30) NOT NULL,
    account_name varchar(200) NOT NULL,
    status bank_account_status_enum NOT NULL DEFAULT 'INACTIVE',
    created_by_user_id bigint NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    activated_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    activated_at timestamptz,
    deactivated_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    deactivated_at timestamptz,
    version bigint NOT NULL DEFAULT 1,
    is_deleted boolean NOT NULL DEFAULT false,
    deleted_by_user_id bigint REFERENCES users(id) ON DELETE RESTRICT,
    deleted_at timestamptz,
    archive_reason varchar(500),
    CONSTRAINT uq_bank_accounts_identity UNIQUE (bank_bin, account_number),
    CONSTRAINT ck_bank_accounts_identity CHECK (
        bank_bin ~ '^[0-9]{6}$' AND account_number ~ '^[0-9]{6,30}$'
        AND char_length(btrim(bank_name)) BETWEEN 1 AND 200
        AND char_length(btrim(account_name)) BETWEEN 1 AND 200
    ),
    CONSTRAINT ck_bank_accounts_version CHECK (version >= 1),
    CONSTRAINT ck_bank_accounts_activation CHECK (
        (status = 'ACTIVE' AND activated_by_user_id IS NOT NULL AND activated_at IS NOT NULL
            AND deactivated_by_user_id IS NULL AND deactivated_at IS NULL)
        OR
        (status = 'INACTIVE'
            AND ((activated_by_user_id IS NULL AND activated_at IS NULL
                    AND deactivated_by_user_id IS NULL AND deactivated_at IS NULL)
                OR (activated_by_user_id IS NOT NULL AND activated_at IS NOT NULL
                    AND deactivated_by_user_id IS NOT NULL AND deactivated_at IS NOT NULL)))
    ),
    CONSTRAINT ck_bank_accounts_archive CHECK (
        (NOT is_deleted AND deleted_by_user_id IS NULL AND deleted_at IS NULL AND archive_reason IS NULL)
        OR
        (is_deleted AND status = 'INACTIVE' AND deleted_by_user_id IS NOT NULL AND deleted_at IS NOT NULL
            AND char_length(btrim(archive_reason)) BETWEEN 1 AND 500)
    )
);

CREATE UNIQUE INDEX uq_bank_accounts_one_active
    ON bank_accounts ((1)) WHERE status = 'ACTIVE' AND NOT is_deleted;
CREATE INDEX ix_bank_accounts_live_state
    ON bank_accounts(status, id) WHERE NOT is_deleted;
CREATE INDEX ix_bank_accounts_list
    ON bank_accounts(bank_name, id);

CREATE FUNCTION enforce_system_setting_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.setting_key <> OLD.setting_key OR NEW.value_type <> OLD.value_type
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'system setting identity and type are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at <= OLD.updated_at THEN
        RAISE EXCEPTION 'system setting update must increment version' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_system_settings_update BEFORE UPDATE ON system_settings
FOR EACH ROW EXECUTE FUNCTION enforce_system_setting_update();
CREATE TRIGGER trg_system_settings_no_delete BEFORE DELETE ON system_settings
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();

CREATE FUNCTION enforce_bank_account_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.bank_bin <> OLD.bank_bin OR NEW.bank_name <> OLD.bank_name
       OR NEW.account_number <> OLD.account_number OR NEW.account_name <> OLD.account_name
       OR NEW.created_by_user_id <> OLD.created_by_user_id OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'bank account identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.is_deleted THEN
        RAISE EXCEPTION 'archived bank account is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'bank account update must increment version' USING ERRCODE = '55000';
    END IF;
    IF NOT (
        (OLD.status = 'INACTIVE' AND NEW.status = 'ACTIVE' AND NOT NEW.is_deleted)
        OR (OLD.status = 'ACTIVE' AND NEW.status = 'INACTIVE' AND NOT NEW.is_deleted)
        OR (OLD.status = 'INACTIVE' AND NEW.status = 'INACTIVE' AND NOT OLD.is_deleted AND NEW.is_deleted)
    ) THEN
        RAISE EXCEPTION 'invalid bank account transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_bank_accounts_update BEFORE UPDATE ON bank_accounts
FOR EACH ROW EXECUTE FUNCTION enforce_bank_account_update();
CREATE TRIGGER trg_bank_accounts_no_delete BEFORE DELETE ON bank_accounts
FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
