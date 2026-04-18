-- +goose Up
-- +goose StatementBegin
-- General Ledger: every posted business event writes a journal here.
-- Xero exposes the same via the Journals API; Trial Balance / P&L / Balance Sheet
-- all derive from these rows.
CREATE TABLE gl_journals (
    journal_id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id    UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    journal_number     BIGSERIAL NOT NULL,
    reference          VARCHAR(500),
    source_type        VARCHAR(40) NOT NULL,           -- INVOICE / PAYMENT / BANKTRANSACTION / MANUALJOURNAL / CREDITNOTE
    source_id          UUID,
    journal_date       DATE NOT NULL,
    created_date_utc   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_gl_journals_org_date ON gl_journals(organisation_id, journal_date DESC);
CREATE INDEX idx_gl_journals_source ON gl_journals(organisation_id, source_type, source_id);

CREATE TABLE gl_journal_lines (
    line_id       UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    journal_id    UUID NOT NULL REFERENCES gl_journals(journal_id) ON DELETE CASCADE,
    account_id    UUID NOT NULL REFERENCES accounts(account_id) ON DELETE RESTRICT,
    description   TEXT,
    tax_type      VARCHAR(50),
    tax_amount    NUMERIC(18,4) NOT NULL DEFAULT 0,
    net_amount    NUMERIC(18,4) NOT NULL,   -- signed: +debit / -credit (Xero convention)
    gross_amount  NUMERIC(18,4) NOT NULL,
    tracking      JSONB
);

CREATE INDEX idx_gl_journal_lines_journal_id ON gl_journal_lines(journal_id);
CREATE INDEX idx_gl_journal_lines_account_id ON gl_journal_lines(account_id);

-- Credit note allocations — let a credit note pay down one or more invoices.
CREATE TABLE credit_note_allocations (
    allocation_id  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    credit_note_id UUID NOT NULL REFERENCES credit_notes(credit_note_id) ON DELETE CASCADE,
    invoice_id     UUID NOT NULL REFERENCES invoices(invoice_id) ON DELETE CASCADE,
    amount         NUMERIC(18,4) NOT NULL,
    date           DATE NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_credit_note_allocations_invoice ON credit_note_allocations(invoice_id);
CREATE INDEX idx_credit_note_allocations_credit ON credit_note_allocations(credit_note_id);

-- Bank transfers move money between two bank accounts in a single record.
CREATE TABLE bank_transfers (
    bank_transfer_id     UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id      UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    from_bank_account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE RESTRICT,
    to_bank_account_id   UUID NOT NULL REFERENCES accounts(account_id) ON DELETE RESTRICT,
    amount               NUMERIC(18,4) NOT NULL,
    date                 DATE NOT NULL,
    reference            VARCHAR(255),
    currency_rate        NUMERIC(18,6),
    has_attachments      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bank_transfers_org ON bank_transfers(organisation_id);

-- Branding themes (used on invoices and quotes as BrandingThemeID).
CREATE TABLE branding_themes (
    branding_theme_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id   UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    name              VARCHAR(255) NOT NULL,
    sort_order        INT NOT NULL DEFAULT 0,
    logo_url          VARCHAR(500),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, name)
);

-- Tracking categories and options.
CREATE TABLE tracking_categories (
    tracking_category_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id      UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    name                 VARCHAR(100) NOT NULL,
    status               VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, name)
);

CREATE TABLE tracking_options (
    tracking_option_id   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tracking_category_id UUID NOT NULL REFERENCES tracking_categories(tracking_category_id) ON DELETE CASCADE,
    name                 VARCHAR(100) NOT NULL,
    status               VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    UNIQUE(tracking_category_id, name)
);

-- Generic attachments table (polymorphic over subject_type/subject_id).
CREATE TABLE attachments (
    attachment_id    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id  UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    subject_type     VARCHAR(40) NOT NULL,
    subject_id       UUID NOT NULL,
    file_name        VARCHAR(255) NOT NULL,
    mime_type        VARCHAR(100),
    size_bytes       BIGINT,
    content          BYTEA,
    include_online   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_attachments_subject ON attachments(organisation_id, subject_type, subject_id);

-- History / notes log (matches Xero's History and Notes endpoint).
CREATE TABLE history_records (
    history_id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    subject_type    VARCHAR(40) NOT NULL,
    subject_id      UUID NOT NULL,
    changes         VARCHAR(40) NOT NULL DEFAULT 'Note',
    details         TEXT NOT NULL,
    user_id         UUID REFERENCES users(user_id) ON DELETE SET NULL,
    date_utc        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_history_records_subject ON history_records(organisation_id, subject_type, subject_id);

-- Tracking columns on invoice / quote / PO / bank transaction line items
-- already exist as JSONB (tracking). Nothing to add there for v1.

-- Seed two default branding themes for the demo org so UI can render
-- a non-empty list without forcing every test to create one.
INSERT INTO branding_themes (organisation_id, name, sort_order)
VALUES
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'Standard', 0),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'Invoice',  1)
ON CONFLICT DO NOTHING;

-- Seed default currencies for the demo org (USD already the base, add a few).
INSERT INTO currencies (organisation_id, code, description) VALUES
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'USD', 'United States Dollar'),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'EUR', 'Euro'),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'GBP', 'Pound Sterling')
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS history_records;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS tracking_options;
DROP TABLE IF EXISTS tracking_categories;
DROP TABLE IF EXISTS branding_themes;
DROP TABLE IF EXISTS bank_transfers;
DROP TABLE IF EXISTS credit_note_allocations;
DROP TABLE IF EXISTS gl_journal_lines;
DROP TABLE IF EXISTS gl_journals;
-- +goose StatementEnd
