-- +goose Up
-- +goose StatementBegin
-- Manual Journals
CREATE TABLE manual_journals (
    manual_journal_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id   UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    narration         VARCHAR(500) NOT NULL,
    date              DATE NOT NULL,
    line_amount_types VARCHAR(20) NOT NULL DEFAULT 'Exclusive',
    status            VARCHAR(20) NOT NULL DEFAULT 'DRAFT',
    url               VARCHAR(500),
    show_on_cash_basis_reports BOOLEAN NOT NULL DEFAULT FALSE,
    updated_date_utc  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_manual_journals_organisation_id ON manual_journals(organisation_id);

CREATE TABLE manual_journal_lines (
    line_id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    manual_journal_id UUID NOT NULL REFERENCES manual_journals(manual_journal_id) ON DELETE CASCADE,
    description       TEXT,
    account_code      VARCHAR(10),
    account_id        UUID REFERENCES accounts(account_id) ON DELETE SET NULL,
    tax_type          VARCHAR(50),
    tax_amount        NUMERIC(18,4) NOT NULL DEFAULT 0,
    line_amount       NUMERIC(18,4) NOT NULL
);

-- Quotes
CREATE TABLE quotes (
    quote_id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    contact_id      UUID REFERENCES contacts(contact_id) ON DELETE SET NULL,
    quote_number    VARCHAR(255),
    reference       VARCHAR(255),
    title           VARCHAR(255),
    summary         TEXT,
    terms           TEXT,
    date            DATE,
    expiry_date     DATE,
    currency_code   VARCHAR(3),
    currency_rate   NUMERIC(18,6),
    status          VARCHAR(20) NOT NULL DEFAULT 'DRAFT',
    line_amount_types VARCHAR(20) NOT NULL DEFAULT 'Exclusive',
    sub_total       NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_tax       NUMERIC(18,4) NOT NULL DEFAULT 0,
    total           NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_discount  NUMERIC(18,4) NOT NULL DEFAULT 0,
    branding_theme_id UUID,
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, quote_number)
);

CREATE INDEX idx_quotes_organisation_id ON quotes(organisation_id);
CREATE INDEX idx_quotes_contact_id ON quotes(contact_id);

CREATE TABLE quote_line_items (
    line_item_id    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    quote_id        UUID NOT NULL REFERENCES quotes(quote_id) ON DELETE CASCADE,
    sort_order      INT NOT NULL DEFAULT 0,
    description     TEXT,
    quantity        NUMERIC(18,4) NOT NULL DEFAULT 1,
    unit_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    item_code       VARCHAR(30),
    account_code    VARCHAR(10),
    tax_type        VARCHAR(50),
    tax_amount      NUMERIC(18,4) NOT NULL DEFAULT 0,
    line_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    discount_rate   NUMERIC(9,4)
);

-- Purchase orders
CREATE TABLE purchase_orders (
    purchase_order_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id   UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    contact_id        UUID REFERENCES contacts(contact_id) ON DELETE SET NULL,
    purchase_order_number VARCHAR(255),
    reference         VARCHAR(255),
    date              DATE,
    delivery_date     DATE,
    delivery_address  TEXT,
    attention_to      VARCHAR(255),
    telephone         VARCHAR(50),
    delivery_instructions TEXT,
    currency_code     VARCHAR(3),
    currency_rate     NUMERIC(18,6),
    status            VARCHAR(20) NOT NULL DEFAULT 'DRAFT',
    line_amount_types VARCHAR(20) NOT NULL DEFAULT 'Exclusive',
    sub_total         NUMERIC(18,4) NOT NULL DEFAULT 0,
    total_tax         NUMERIC(18,4) NOT NULL DEFAULT 0,
    total             NUMERIC(18,4) NOT NULL DEFAULT 0,
    updated_date_utc  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, purchase_order_number)
);

CREATE INDEX idx_purchase_orders_organisation_id ON purchase_orders(organisation_id);
CREATE INDEX idx_purchase_orders_contact_id ON purchase_orders(contact_id);

CREATE TABLE purchase_order_line_items (
    line_item_id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    purchase_order_id UUID NOT NULL REFERENCES purchase_orders(purchase_order_id) ON DELETE CASCADE,
    sort_order        INT NOT NULL DEFAULT 0,
    description       TEXT,
    quantity          NUMERIC(18,4) NOT NULL DEFAULT 1,
    unit_amount       NUMERIC(18,4) NOT NULL DEFAULT 0,
    item_code         VARCHAR(30),
    account_code      VARCHAR(10),
    tax_type          VARCHAR(50),
    tax_amount        NUMERIC(18,4) NOT NULL DEFAULT 0,
    line_amount       NUMERIC(18,4) NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS purchase_order_line_items;
DROP TABLE IF EXISTS purchase_orders;
DROP TABLE IF EXISTS quote_line_items;
DROP TABLE IF EXISTS quotes;
DROP TABLE IF EXISTS manual_journal_lines;
DROP TABLE IF EXISTS manual_journals;
-- +goose StatementEnd
