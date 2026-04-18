-- +goose Up
-- +goose StatementBegin
-- Organisation is the tenant concept in Xero. Each organisation has a unique
-- OrganisationID (tenant id) and all other resources belong to it.
CREATE TABLE organisations (
    organisation_id     UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    api_key             VARCHAR(64) UNIQUE NOT NULL DEFAULT encode(gen_random_bytes(24), 'hex'),
    name                VARCHAR(255) NOT NULL,
    legal_name          VARCHAR(255),
    short_code          VARCHAR(32),
    organisation_type   VARCHAR(50) DEFAULT 'COMPANY',
    country_code        VARCHAR(2),
    base_currency       VARCHAR(3) NOT NULL DEFAULT 'USD',
    timezone            VARCHAR(64) DEFAULT 'UTC',
    financial_year_end_day   SMALLINT DEFAULT 31,
    financial_year_end_month SMALLINT DEFAULT 12,
    tax_number          VARCHAR(50),
    line_of_business    VARCHAR(255),
    registration_number VARCHAR(100),
    is_demo_company     BOOLEAN NOT NULL DEFAULT FALSE,
    organisation_status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_organisations_api_key ON organisations(api_key);
CREATE INDEX idx_organisations_status ON organisations(organisation_status);

CREATE TABLE users (
    user_id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email          CITEXT UNIQUE NOT NULL,
    password_hash  VARCHAR(255) NOT NULL,
    first_name     VARCHAR(100),
    last_name      VARCHAR(100),
    is_subscriber  BOOLEAN NOT NULL DEFAULT FALSE,
    organisation_role VARCHAR(50) DEFAULT 'STANDARD',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE organisation_users (
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    role            VARCHAR(50) NOT NULL DEFAULT 'STANDARD',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organisation_id, user_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS organisation_users;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organisations;
-- +goose StatementEnd
