-- +goose Up
-- +goose StatementBegin
-- Bank feed integration — stores per-tenant connections to Open Banking
-- aggregators (GoCardless Bank Account Data, Plaid, TrueLayer, Salt Edge, …)
-- plus a staging area for raw statement lines before they're matched against
-- invoices or posted as bank_transactions.
--
-- Design notes:
-- * `provider` is a free-form slug registered by `internal/bankfeed`. Adding
--   a new provider requires zero schema changes.
-- * `external_*` columns are whatever ids/opaque tokens the provider returns
--   (e.g. GoCardless requisition_id, Plaid item_id). We never store raw bank
--   credentials — OAuth/PSD2 consent is handled server-to-server.
-- * `bank_feed_accounts` maps an upstream account to our own
--   `accounts.account_id` (which must already be marked as BANK).
-- * `bank_feed_statement_lines` is append-only, unique on
--   (connection_id, provider_tx_id) so re-sync is idempotent.
CREATE TABLE bank_feed_connections (
    connection_id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id    UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    provider           VARCHAR(40) NOT NULL,
    status             VARCHAR(20) NOT NULL DEFAULT 'PENDING',    -- PENDING / LINKED / ERROR / REVOKED
    institution_id     VARCHAR(100),
    institution_name   VARCHAR(255),
    external_reference VARCHAR(255),                              -- GoCardless requisition_id / Plaid item_id
    auth_url           VARCHAR(1000),                             -- consent link to redirect the user to
    last_error         TEXT,
    last_synced_at     TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bank_feed_conn_org ON bank_feed_connections(organisation_id);
CREATE UNIQUE INDEX uq_bank_feed_conn_ref
    ON bank_feed_connections(organisation_id, provider, external_reference)
    WHERE external_reference IS NOT NULL;

CREATE TABLE bank_feed_accounts (
    feed_account_id     UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    connection_id       UUID NOT NULL REFERENCES bank_feed_connections(connection_id) ON DELETE CASCADE,
    organisation_id     UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    account_id          UUID REFERENCES accounts(account_id) ON DELETE SET NULL,
    external_account_id VARCHAR(100) NOT NULL,
    display_name        VARCHAR(255),
    iban                VARCHAR(64),
    currency_code       VARCHAR(10),
    balance             NUMERIC(18,4),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(connection_id, external_account_id)
);

CREATE INDEX idx_bank_feed_accts_org ON bank_feed_accounts(organisation_id);
CREATE INDEX idx_bank_feed_accts_account ON bank_feed_accounts(account_id);

CREATE TABLE bank_feed_statement_lines (
    statement_line_id   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id     UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    feed_account_id     UUID NOT NULL REFERENCES bank_feed_accounts(feed_account_id) ON DELETE CASCADE,
    provider_tx_id      VARCHAR(255) NOT NULL,
    posted_at           DATE NOT NULL,
    amount              NUMERIC(18,4) NOT NULL,   -- signed: positive = credit, negative = debit
    currency_code       VARCHAR(10) NOT NULL,
    description         TEXT,
    counterparty        VARCHAR(255),
    reference           VARCHAR(255),
    raw                 JSONB,
    status              VARCHAR(20) NOT NULL DEFAULT 'NEW',   -- NEW / IMPORTED / IGNORED
    bank_transaction_id UUID REFERENCES bank_transactions(bank_transaction_id) ON DELETE SET NULL,
    imported_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(feed_account_id, provider_tx_id)
);

CREATE INDEX idx_bank_feed_lines_org_status
    ON bank_feed_statement_lines(organisation_id, status, posted_at DESC);
CREATE INDEX idx_bank_feed_lines_account
    ON bank_feed_statement_lines(feed_account_id, posted_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bank_feed_statement_lines;
DROP TABLE IF EXISTS bank_feed_accounts;
DROP TABLE IF EXISTS bank_feed_connections;
-- +goose StatementEnd
