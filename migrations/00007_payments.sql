-- +goose Up
-- +goose StatementBegin
-- Payments - https://developer.xero.com/documentation/api/accounting/payments
CREATE TABLE payments (
    payment_id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    invoice_id      UUID REFERENCES invoices(invoice_id) ON DELETE SET NULL,
    credit_note_id  UUID REFERENCES credit_notes(credit_note_id) ON DELETE SET NULL,
    account_id      UUID REFERENCES accounts(account_id) ON DELETE SET NULL,
    payment_type    VARCHAR(30) NOT NULL DEFAULT 'ACCRECPAYMENT',
    status          VARCHAR(20) NOT NULL DEFAULT 'AUTHORISED',
    date            DATE NOT NULL,
    currency_rate   NUMERIC(18,6),
    amount          NUMERIC(18,4) NOT NULL,
    reference       VARCHAR(255),
    is_reconciled   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_payments_organisation_id ON payments(organisation_id);
CREATE INDEX idx_payments_invoice_id ON payments(invoice_id);
CREATE INDEX idx_payments_credit_note_id ON payments(credit_note_id);
CREATE INDEX idx_payments_date ON payments(organisation_id, date DESC);

-- Bank transactions
CREATE TABLE bank_transactions (
    bank_transaction_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id     UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    contact_id          UUID REFERENCES contacts(contact_id) ON DELETE SET NULL,
    bank_account_id     UUID REFERENCES accounts(account_id) ON DELETE SET NULL,
    type                VARCHAR(30) NOT NULL,
    is_reconciled       BOOLEAN NOT NULL DEFAULT FALSE,
    date                DATE NOT NULL,
    reference           VARCHAR(255),
    currency_code       VARCHAR(3),
    currency_rate       NUMERIC(18,6),
    url                 VARCHAR(500),
    status              VARCHAR(20) NOT NULL DEFAULT 'AUTHORISED',
    line_amount_types   VARCHAR(20) NOT NULL DEFAULT 'Exclusive',
    sub_total           NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_tax           NUMERIC(18,4) NOT NULL DEFAULT 0,
    total               NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bank_transactions_organisation_id ON bank_transactions(organisation_id);
CREATE INDEX idx_bank_transactions_bank_account_id ON bank_transactions(bank_account_id);

CREATE TABLE bank_transaction_line_items (
    line_item_id   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    bank_transaction_id UUID NOT NULL REFERENCES bank_transactions(bank_transaction_id) ON DELETE CASCADE,
    description    TEXT,
    quantity       NUMERIC(18,4) NOT NULL DEFAULT 1,
    unit_amount    NUMERIC(18,4) NOT NULL DEFAULT 0,
    account_code   VARCHAR(10),
    account_id     UUID REFERENCES accounts(account_id) ON DELETE SET NULL,
    tax_type       VARCHAR(50),
    tax_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    line_amount    NUMERIC(18,4) NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bank_transaction_line_items;
DROP TABLE IF EXISTS bank_transactions;
DROP TABLE IF EXISTS payments;
-- +goose StatementEnd
