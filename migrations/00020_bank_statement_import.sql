-- +goose Up
-- +goose StatementBegin
-- Manual bank-statement import + a unified reconciliation inbox.
--
-- Before this migration statement lines could only come from an Open Banking
-- feed (`bank_feed_statement_lines`, one row per provider transaction, keyed on
-- feed_account_id). Xero, however, treats feed lines and manually imported
-- statement lines identically: both land in the reconcile inbox, both show a
-- `Source` column, and both can be matched, created, or ignored.
--
-- So we rename the table to what it now is — `bank_statement_lines` — and let a
-- line hang off *either* a feed account (source FEED) or a ledger bank account
-- (source IMPORT), with the import batch it came from.
ALTER TABLE bank_feed_statement_lines RENAME TO bank_statement_lines;

ALTER TABLE bank_statement_lines ALTER COLUMN feed_account_id DROP NOT NULL;

-- Import batches. The parsed rows are staged in `payload` after the wizard's
-- upload step and only materialised into statement lines on commit, so the user
-- can change column mapping without re-uploading the file.
CREATE TABLE bank_statement_imports (
    import_id       UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    bank_account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    filename        VARCHAR(255),
    format          VARCHAR(20) NOT NULL DEFAULT 'CSV',    -- CSV / OFX / QFX / QIF / QBO
    status          VARCHAR(20) NOT NULL DEFAULT 'STAGED', -- STAGED / IMPORTED / FAILED
    line_count      INTEGER NOT NULL DEFAULT 0,
    imported_count  INTEGER NOT NULL DEFAULT 0,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    currency_code   VARCHAR(10),
    statement_start DATE,
    statement_end   DATE,
    opening_balance NUMERIC(18,4),
    closing_balance NUMERIC(18,4),
    mapping         JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload         JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    committed_at    TIMESTAMPTZ
);

CREATE INDEX idx_bank_stmt_imports_org
    ON bank_statement_imports(organisation_id, created_at DESC);
CREATE INDEX idx_bank_stmt_imports_account
    ON bank_statement_imports(bank_account_id, created_at DESC);

ALTER TABLE bank_statement_lines
    ADD COLUMN bank_account_id UUID REFERENCES accounts(account_id) ON DELETE CASCADE,
    ADD COLUMN import_id       UUID REFERENCES bank_statement_imports(import_id) ON DELETE CASCADE,
    ADD COLUMN source          VARCHAR(20) NOT NULL DEFAULT 'FEED',  -- FEED / IMPORT
    ADD COLUMN payee           VARCHAR(255),
    ADD COLUMN cheque_number   VARCHAR(50),
    ADD COLUMN balance         NUMERIC(18,4),   -- running balance printed on the statement
    ADD COLUMN coded_at        TIMESTAMPTZ,
    ADD COLUMN coded_by        UUID REFERENCES users(user_id) ON DELETE SET NULL;

-- A line belongs to exactly one of the two parents.
ALTER TABLE bank_statement_lines
    ADD CONSTRAINT bank_statement_lines_parent_chk
    CHECK (feed_account_id IS NOT NULL OR bank_account_id IS NOT NULL);

CREATE INDEX idx_bank_stmt_lines_bank_account
    ON bank_statement_lines(bank_account_id, posted_at DESC);
CREATE INDEX idx_bank_stmt_lines_import
    ON bank_statement_lines(import_id);
-- Duplicate detection on manual imports: same account + date + amount + payee.
CREATE INDEX idx_bank_stmt_lines_fingerprint
    ON bank_statement_lines(bank_account_id, posted_at, amount, payee)
    WHERE bank_account_id IS NOT NULL;

-- Reconcile periods lock a date range on one bank account so only authorised
-- users can change already-reconciled data (mirrors Xero's Reconcile period tab).
CREATE TABLE bank_reconcile_periods (
    period_id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id   UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    bank_account_id   UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    start_date        DATE NOT NULL,
    end_date          DATE NOT NULL,
    statement_balance NUMERIC(18,4) NOT NULL DEFAULT 0,
    created_by        UUID REFERENCES users(user_id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_date >= start_date),
    UNIQUE (bank_account_id, start_date, end_date)
);

CREATE INDEX idx_bank_reconcile_periods_org
    ON bank_reconcile_periods(organisation_id, bank_account_id, start_date DESC);

-- Xero evaluates bank rules top-down in a user-controlled order.
ALTER TABLE bank_rules ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_bank_rules_org_order ON bank_rules(organisation_id, rule_type, sort_order);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bank_reconcile_periods;

DROP INDEX IF EXISTS idx_bank_rules_org_order;
ALTER TABLE bank_rules DROP COLUMN IF EXISTS sort_order;

DROP INDEX IF EXISTS idx_bank_stmt_lines_fingerprint;
DROP INDEX IF EXISTS idx_bank_stmt_lines_import;
DROP INDEX IF EXISTS idx_bank_stmt_lines_bank_account;

ALTER TABLE bank_statement_lines DROP CONSTRAINT IF EXISTS bank_statement_lines_parent_chk;
ALTER TABLE bank_statement_lines
    DROP COLUMN IF EXISTS coded_by,
    DROP COLUMN IF EXISTS coded_at,
    DROP COLUMN IF EXISTS balance,
    DROP COLUMN IF EXISTS cheque_number,
    DROP COLUMN IF EXISTS payee,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS import_id,
    DROP COLUMN IF EXISTS bank_account_id;

DROP TABLE IF EXISTS bank_statement_imports;

ALTER TABLE bank_statement_lines ALTER COLUMN feed_account_id SET NOT NULL;
ALTER TABLE bank_statement_lines RENAME TO bank_feed_statement_lines;
-- +goose StatementEnd
