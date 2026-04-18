-- +goose Up
-- +goose StatementBegin

-- Prepayments / Overpayments: signed credit balances that behave like
-- credit notes — they sit on the contact and can be allocated to invoices.
CREATE TABLE prepayments (
    prepayment_id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id    UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    contact_id         UUID          REFERENCES contacts(contact_id) ON DELETE SET NULL,
    type               TEXT NOT NULL CHECK (type IN ('RECEIVE-PREPAYMENT','SPEND-PREPAYMENT')),
    status             TEXT NOT NULL DEFAULT 'AUTHORISED'
                       CHECK (status IN ('AUTHORISED','PAID','VOIDED','DELETED')),
    currency_code      TEXT NOT NULL DEFAULT 'USD',
    date               DATE,
    reference          TEXT,
    sub_total          NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_tax          NUMERIC(18,4) NOT NULL DEFAULT 0,
    total              NUMERIC(18,4) NOT NULL DEFAULT 0,
    remaining_credit   NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX prepayments_org_idx ON prepayments(organisation_id);
CREATE INDEX prepayments_contact_idx ON prepayments(contact_id);

CREATE TABLE overpayments (
    overpayment_id     UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id    UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    contact_id         UUID          REFERENCES contacts(contact_id) ON DELETE SET NULL,
    type               TEXT NOT NULL CHECK (type IN ('RECEIVE-OVERPAYMENT','SPEND-OVERPAYMENT')),
    status             TEXT NOT NULL DEFAULT 'AUTHORISED'
                       CHECK (status IN ('AUTHORISED','PAID','VOIDED','DELETED')),
    currency_code      TEXT NOT NULL DEFAULT 'USD',
    date               DATE,
    reference          TEXT,
    total              NUMERIC(18,4) NOT NULL DEFAULT 0,
    remaining_credit   NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX overpayments_org_idx ON overpayments(organisation_id);
CREATE INDEX overpayments_contact_idx ON overpayments(contact_id);

-- Repeating invoices: template + schedule → generates invoices on cadence.
CREATE TABLE repeating_invoices (
    repeating_invoice_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id      UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    contact_id           UUID          REFERENCES contacts(contact_id) ON DELETE SET NULL,
    type                 TEXT NOT NULL CHECK (type IN ('ACCREC','ACCPAY')),
    status               TEXT NOT NULL DEFAULT 'DRAFT'
                         CHECK (status IN ('DRAFT','AUTHORISED','DELETED')),
    reference            TEXT,
    line_amount_types    TEXT NOT NULL DEFAULT 'Exclusive',
    currency_code        TEXT NOT NULL DEFAULT 'USD',
    branding_theme_id    UUID,
    -- Schedule
    period               INT  NOT NULL DEFAULT 1,
    unit                 TEXT NOT NULL DEFAULT 'MONTHLY'
                         CHECK (unit IN ('WEEKLY','MONTHLY','YEARLY')),
    due_date             INT  NOT NULL DEFAULT 0,
    due_date_type        TEXT NOT NULL DEFAULT 'DAYSAFTERBILLDATE',
    start_date           DATE,
    next_scheduled_date  DATE,
    end_date             DATE,
    -- Cached totals
    sub_total            NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_tax            NUMERIC(18,4) NOT NULL DEFAULT 0,
    total                NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX repeating_invoices_org_idx ON repeating_invoices(organisation_id);

CREATE TABLE repeating_invoice_line_items (
    line_item_id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repeating_invoice_id UUID NOT NULL REFERENCES repeating_invoices(repeating_invoice_id) ON DELETE CASCADE,
    description          TEXT,
    quantity             NUMERIC(18,4) NOT NULL DEFAULT 0,
    unit_amount          NUMERIC(18,4) NOT NULL DEFAULT 0,
    account_code         TEXT,
    tax_type             TEXT,
    tax_amount           NUMERIC(18,4) NOT NULL DEFAULT 0,
    line_amount          NUMERIC(18,4) NOT NULL DEFAULT 0,
    item_code            TEXT,
    discount_rate        NUMERIC(18,4)
);
CREATE INDEX repeating_invoice_lines_ri_idx ON repeating_invoice_line_items(repeating_invoice_id);

-- Batch payments: group multiple child payments under one bank reference.
CREATE TABLE batch_payments (
    batch_payment_id   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id    UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    account_id         UUID NOT NULL REFERENCES accounts(account_id) ON DELETE RESTRICT,
    date               DATE,
    reference          TEXT,
    narrative          TEXT,
    details            TEXT,
    status             TEXT NOT NULL DEFAULT 'AUTHORISED'
                       CHECK (status IN ('AUTHORISED','DELETED')),
    total_amount       NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX batch_payments_org_idx ON batch_payments(organisation_id);

-- Wire payments to the parent batch (nullable so plain payments still work).
ALTER TABLE payments ADD COLUMN IF NOT EXISTS batch_payment_id UUID
    REFERENCES batch_payments(batch_payment_id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS payments_batch_idx ON payments(batch_payment_id);

-- Linked transactions: connect a bill line to a sales invoice (rechargeable
-- expenses / billable time).
CREATE TABLE linked_transactions (
    linked_transaction_id     UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id           UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    source_transaction_id     UUID NOT NULL,
    source_line_item_id       UUID,
    target_transaction_id     UUID,
    target_line_item_id       UUID,
    contact_id                UUID REFERENCES contacts(contact_id) ON DELETE SET NULL,
    type                      TEXT NOT NULL DEFAULT 'BILLABLE_EXPENSE',
    status                    TEXT NOT NULL DEFAULT 'DRAFT'
                              CHECK (status IN ('DRAFT','APPROVED','ONDRAFT','ONHOLD','BILLED')),
    updated_date_utc          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX linked_transactions_org_idx ON linked_transactions(organisation_id);
CREATE INDEX linked_transactions_source_idx ON linked_transactions(source_transaction_id);

-- Employees: used by payroll-lite / expense claims.
CREATE TABLE employees (
    employee_id       UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id   UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    first_name        TEXT NOT NULL,
    last_name         TEXT,
    email             TEXT,
    status            TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','ARCHIVED')),
    phone             TEXT,
    updated_date_utc  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX employees_org_idx ON employees(organisation_id);

-- Receipts: expense-claim receipts (cash / credit card purchases).
CREATE TABLE receipts (
    receipt_id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id   UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    user_id           UUID REFERENCES users(user_id) ON DELETE SET NULL,
    contact_id        UUID REFERENCES contacts(contact_id) ON DELETE SET NULL,
    date              DATE,
    reference         TEXT,
    status            TEXT NOT NULL DEFAULT 'DRAFT'
                      CHECK (status IN ('DRAFT','SUBMITTED','AUTHORISED','DECLINED','VOIDED')),
    line_amount_types TEXT NOT NULL DEFAULT 'Exclusive',
    sub_total         NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_tax         NUMERIC(18,4) NOT NULL DEFAULT 0,
    total             NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX receipts_org_idx ON receipts(organisation_id);

CREATE TABLE receipt_line_items (
    line_item_id   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    receipt_id     UUID NOT NULL REFERENCES receipts(receipt_id) ON DELETE CASCADE,
    description    TEXT,
    quantity       NUMERIC(18,4) NOT NULL DEFAULT 0,
    unit_amount    NUMERIC(18,4) NOT NULL DEFAULT 0,
    account_code   TEXT,
    tax_type       TEXT,
    tax_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    line_amount    NUMERIC(18,4) NOT NULL DEFAULT 0,
    discount_rate  NUMERIC(18,4)
);
CREATE INDEX receipt_lines_rid_idx ON receipt_line_items(receipt_id);

-- Expense claims (groups of receipts).
CREATE TABLE expense_claims (
    expense_claim_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id  UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    user_id          UUID REFERENCES users(user_id) ON DELETE SET NULL,
    status           TEXT NOT NULL DEFAULT 'SUBMITTED'
                     CHECK (status IN ('SUBMITTED','AUTHORISED','PAID','DELETED')),
    payment_due_date DATE,
    reporting_date   DATE,
    total            NUMERIC(18,4) NOT NULL DEFAULT 0,
    amount_due       NUMERIC(18,4) NOT NULL DEFAULT 0,
    amount_paid      NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX expense_claims_org_idx ON expense_claims(organisation_id);

CREATE TABLE expense_claim_receipts (
    expense_claim_id UUID NOT NULL REFERENCES expense_claims(expense_claim_id) ON DELETE CASCADE,
    receipt_id       UUID NOT NULL REFERENCES receipts(receipt_id) ON DELETE CASCADE,
    PRIMARY KEY (expense_claim_id, receipt_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS expense_claim_receipts;
DROP TABLE IF EXISTS expense_claims;
DROP TABLE IF EXISTS receipt_line_items;
DROP TABLE IF EXISTS receipts;
DROP TABLE IF EXISTS employees;
DROP TABLE IF EXISTS linked_transactions;
ALTER TABLE payments DROP COLUMN IF EXISTS batch_payment_id;
DROP TABLE IF EXISTS batch_payments;
DROP TABLE IF EXISTS repeating_invoice_line_items;
DROP TABLE IF EXISTS repeating_invoices;
DROP TABLE IF EXISTS overpayments;
DROP TABLE IF EXISTS prepayments;
-- +goose StatementEnd
