-- +goose Up
-- +goose StatementBegin
-- Contacts endpoint - https://developer.xero.com/documentation/api/accounting/contacts
CREATE TABLE contacts (
    contact_id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    contact_number  VARCHAR(50),
    account_number  VARCHAR(50),
    contact_status  VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    name            VARCHAR(255) NOT NULL,
    first_name      VARCHAR(100),
    last_name       VARCHAR(100),
    company_number  VARCHAR(50),
    email_address   CITEXT,
    skype_user_name VARCHAR(100),
    bank_account_details VARCHAR(50),
    tax_number      VARCHAR(50),
    accounts_receivable_tax_type VARCHAR(50),
    accounts_payable_tax_type    VARCHAR(50),
    is_supplier     BOOLEAN NOT NULL DEFAULT FALSE,
    is_customer     BOOLEAN NOT NULL DEFAULT FALSE,
    default_currency VARCHAR(3),
    website         VARCHAR(255),
    has_attachments BOOLEAN NOT NULL DEFAULT FALSE,
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_contacts_organisation_id ON contacts(organisation_id);
CREATE INDEX idx_contacts_name ON contacts(organisation_id, name);
CREATE INDEX idx_contacts_email ON contacts(email_address);
CREATE INDEX idx_contacts_status ON contacts(contact_status);

CREATE TABLE contact_addresses (
    id           BIGSERIAL PRIMARY KEY,
    contact_id   UUID NOT NULL REFERENCES contacts(contact_id) ON DELETE CASCADE,
    address_type VARCHAR(20) NOT NULL,
    address_line1 VARCHAR(500),
    address_line2 VARCHAR(500),
    address_line3 VARCHAR(500),
    address_line4 VARCHAR(500),
    city         VARCHAR(255),
    region       VARCHAR(255),
    postal_code  VARCHAR(50),
    country      VARCHAR(100),
    attention_to VARCHAR(255)
);
CREATE INDEX idx_contact_addresses_contact_id ON contact_addresses(contact_id);

CREATE TABLE contact_phones (
    id           BIGSERIAL PRIMARY KEY,
    contact_id   UUID NOT NULL REFERENCES contacts(contact_id) ON DELETE CASCADE,
    phone_type   VARCHAR(20) NOT NULL,
    phone_number VARCHAR(50),
    phone_area_code    VARCHAR(10),
    phone_country_code VARCHAR(10)
);
CREATE INDEX idx_contact_phones_contact_id ON contact_phones(contact_id);

CREATE TABLE contact_groups (
    contact_group_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id  UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    name             VARCHAR(100) NOT NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, name)
);

CREATE TABLE contact_group_members (
    contact_group_id UUID NOT NULL REFERENCES contact_groups(contact_group_id) ON DELETE CASCADE,
    contact_id       UUID NOT NULL REFERENCES contacts(contact_id) ON DELETE CASCADE,
    PRIMARY KEY (contact_group_id, contact_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS contact_group_members;
DROP TABLE IF EXISTS contact_groups;
DROP TABLE IF EXISTS contact_phones;
DROP TABLE IF EXISTS contact_addresses;
DROP TABLE IF EXISTS contacts;
-- +goose StatementEnd
