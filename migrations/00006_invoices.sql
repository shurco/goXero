-- +goose Up
-- +goose StatementBegin
-- Invoices - https://developer.xero.com/documentation/api/accounting/invoices
-- Type: ACCREC (sales) / ACCPAY (bills)
CREATE TABLE invoices (
    invoice_id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    type            VARCHAR(10) NOT NULL,
    contact_id      UUID REFERENCES contacts(contact_id) ON DELETE SET NULL,
    invoice_number  VARCHAR(255),
    reference       VARCHAR(255),
    branding_theme_id UUID,
    url             VARCHAR(500),
    currency_code   VARCHAR(3),
    currency_rate   NUMERIC(18,6),
    status          VARCHAR(20) NOT NULL DEFAULT 'DRAFT',
    line_amount_types VARCHAR(20) NOT NULL DEFAULT 'Exclusive',
    date            DATE,
    due_date        DATE,
    expected_payment_date DATE,
    planned_payment_date  DATE,
    fully_paid_on_date    DATE,
    sub_total       NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_tax       NUMERIC(18,4) NOT NULL DEFAULT 0,
    total           NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_discount  NUMERIC(18,4) NOT NULL DEFAULT 0,
    amount_due      NUMERIC(18,4) NOT NULL DEFAULT 0,
    amount_paid     NUMERIC(18,4) NOT NULL DEFAULT 0,
    amount_credited NUMERIC(18,4) NOT NULL DEFAULT 0,
    has_attachments BOOLEAN NOT NULL DEFAULT FALSE,
    sent_to_contact BOOLEAN NOT NULL DEFAULT FALSE,
    is_discounted   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, invoice_number)
);

CREATE INDEX idx_invoices_organisation_id ON invoices(organisation_id);
CREATE INDEX idx_invoices_contact_id ON invoices(contact_id);
CREATE INDEX idx_invoices_status ON invoices(organisation_id, status);
CREATE INDEX idx_invoices_type ON invoices(organisation_id, type);
CREATE INDEX idx_invoices_date ON invoices(organisation_id, date DESC);

CREATE TABLE invoice_line_items (
    line_item_id    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    invoice_id      UUID NOT NULL REFERENCES invoices(invoice_id) ON DELETE CASCADE,
    sort_order      INT NOT NULL DEFAULT 0,
    description     TEXT,
    quantity        NUMERIC(18,4) NOT NULL DEFAULT 1,
    unit_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    item_code       VARCHAR(30),
    account_code    VARCHAR(10),
    item_id         UUID REFERENCES items(item_id) ON DELETE SET NULL,
    account_id      UUID REFERENCES accounts(account_id) ON DELETE SET NULL,
    tax_type        VARCHAR(50),
    tax_amount      NUMERIC(18,4) NOT NULL DEFAULT 0,
    line_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    discount_rate   NUMERIC(9,4),
    discount_amount NUMERIC(18,4),
    tracking        JSONB
);

CREATE INDEX idx_invoice_line_items_invoice_id ON invoice_line_items(invoice_id);

-- Credit notes
CREATE TABLE credit_notes (
    credit_note_id  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    type            VARCHAR(20) NOT NULL,
    contact_id      UUID REFERENCES contacts(contact_id) ON DELETE SET NULL,
    credit_note_number VARCHAR(255),
    reference       VARCHAR(255),
    status          VARCHAR(20) NOT NULL DEFAULT 'DRAFT',
    date            DATE,
    due_date        DATE,
    currency_code   VARCHAR(3),
    currency_rate   NUMERIC(18,6),
    line_amount_types VARCHAR(20) NOT NULL DEFAULT 'Exclusive',
    sub_total       NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_tax       NUMERIC(18,4) NOT NULL DEFAULT 0,
    total           NUMERIC(18,4) NOT NULL DEFAULT 0,
    remaining_credit NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, credit_note_number)
);

CREATE INDEX idx_credit_notes_organisation_id ON credit_notes(organisation_id);
CREATE INDEX idx_credit_notes_contact_id ON credit_notes(contact_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS credit_notes;
DROP TABLE IF EXISTS invoice_line_items;
DROP TABLE IF EXISTS invoices;
-- +goose StatementEnd
