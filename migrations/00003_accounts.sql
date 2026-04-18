-- +goose Up
-- +goose StatementBegin
-- Chart of accounts - follows Xero Accounts endpoint schema.
-- https://developer.xero.com/documentation/api/accounting/accounts
CREATE TABLE accounts (
    account_id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    code            VARCHAR(10) NOT NULL,
    name            VARCHAR(150) NOT NULL,
    type            VARCHAR(50) NOT NULL,
    bank_account_number VARCHAR(50),
    bank_account_type   VARCHAR(20),
    currency_code       VARCHAR(3),
    status          VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    description     TEXT,
    tax_type        VARCHAR(50),
    enable_payments_to_account BOOLEAN NOT NULL DEFAULT FALSE,
    show_in_expense_claims     BOOLEAN NOT NULL DEFAULT FALSE,
    class           VARCHAR(20),
    system_account  VARCHAR(50),
    reporting_code  VARCHAR(20),
    reporting_code_name VARCHAR(100),
    has_attachments BOOLEAN NOT NULL DEFAULT FALSE,
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, code)
);

CREATE INDEX idx_accounts_organisation_id ON accounts(organisation_id);
CREATE INDEX idx_accounts_type ON accounts(type);
CREATE INDEX idx_accounts_status ON accounts(status);

-- Tax rates - Xero TaxRates endpoint.
CREATE TABLE tax_rates (
    tax_rate_id     UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    name            VARCHAR(100) NOT NULL,
    tax_type        VARCHAR(50) NOT NULL,
    report_tax_type VARCHAR(50),
    can_apply_to_assets      BOOLEAN NOT NULL DEFAULT TRUE,
    can_apply_to_equity      BOOLEAN NOT NULL DEFAULT TRUE,
    can_apply_to_expenses    BOOLEAN NOT NULL DEFAULT TRUE,
    can_apply_to_liabilities BOOLEAN NOT NULL DEFAULT TRUE,
    can_apply_to_revenue     BOOLEAN NOT NULL DEFAULT TRUE,
    display_tax_rate NUMERIC(9,4) NOT NULL DEFAULT 0,
    effective_rate   NUMERIC(9,4) NOT NULL DEFAULT 0,
    status           VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, name)
);

CREATE INDEX idx_tax_rates_organisation_id ON tax_rates(organisation_id);

-- Currencies.
CREATE TABLE currencies (
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    code            VARCHAR(3) NOT NULL,
    description     VARCHAR(100),
    PRIMARY KEY (organisation_id, code)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS currencies;
DROP TABLE IF EXISTS tax_rates;
DROP TABLE IF EXISTS accounts;
-- +goose StatementEnd
