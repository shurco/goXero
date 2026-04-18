-- +goose Up
-- +goose StatementBegin
-- Dev/test dataset: second organisation with explicit v4 UUIDs (see README.md in this folder).
-- Apply with: ./scripts/migrate dev up
INSERT INTO organisations (organisation_id, name, legal_name, short_code, country_code, base_currency, timezone, is_demo_company)
VALUES (
    '72590a0d-deb9-4fcc-a05a-e40fb47afc43',
    'Fixture Labs LLC',
    'Fixture Labs LLC',
    'FIXLAB',
    'US',
    'USD',
    'UTC',
    FALSE
);

-- password: admin123 (bcrypt, same cost as demo seed)
INSERT INTO users (user_id, email, password_hash, first_name, last_name, is_subscriber)
VALUES (
    'f8584d07-ec38-488b-9521-64da6fae19ee',
    'fixture-dev@goxero.test',
    '$2a$10$pAimBhbqKiEBvTXqKhhWlOfbgNNFoa5o3GlGLR9EGxKh5hedcwUVK',
    'Fixture',
    'Developer',
    FALSE
);

INSERT INTO organisation_users (organisation_id, user_id, role)
VALUES ('72590a0d-deb9-4fcc-a05a-e40fb47afc43', 'f8584d07-ec38-488b-9521-64da6fae19ee', 'ADMIN');

INSERT INTO accounts (account_id, organisation_id, code, name, type, status, tax_type, class, system_account) VALUES
    ('ab2d610b-2875-4693-bd21-552b2ee4b86d', '72590a0d-deb9-4fcc-a05a-e40fb47afc43', '090', 'Business Bank Account', 'BANK',     'ACTIVE', 'NONE',   'ASSET',    NULL),
    ('3f5f7b84-bf88-45f6-b199-8591b1d6770d', '72590a0d-deb9-4fcc-a05a-e40fb47afc43', '200', 'Sales',                 'REVENUE',  'ACTIVE', 'OUTPUT', 'REVENUE',  NULL),
    ('41e85ef9-33df-4838-8434-3642efafb8be', '72590a0d-deb9-4fcc-a05a-e40fb47afc43', '610', 'Accounts Receivable',   'CURRENT',  'ACTIVE', 'NONE',   'ASSET',    'DEBTORS');

INSERT INTO tax_rates (
    tax_rate_id, organisation_id, name, tax_type, report_tax_type,
    display_tax_rate, effective_rate
) VALUES (
    '6e64bbbf-c2c5-46fa-872f-1357c642c0a6',
    '72590a0d-deb9-4fcc-a05a-e40fb47afc43',
    'Tax on Sales',
    'OUTPUT',
    'OUTPUT',
    8.25,
    8.25
);

INSERT INTO contacts (contact_id, organisation_id, name, first_name, last_name, email_address, is_customer, is_supplier)
VALUES (
    '0945278a-c8d8-457b-8a94-8900d7b94e21',
    '72590a0d-deb9-4fcc-a05a-e40fb47afc43',
    'Fixture Customer Inc',
    'Dana',
    'Rivera',
    'dana@fixture-customer.test',
    TRUE,
    FALSE
);

INSERT INTO items (
    item_id, organisation_id, code, name, description,
    sales_unit_price, sales_account_code, is_sold, is_purchased
) VALUES (
    '4861bfeb-eb16-4f2d-82da-55a3f4a36fbf',
    '72590a0d-deb9-4fcc-a05a-e40fb47afc43',
    'FIXTURE-SKU',
    'Fixture catalog item',
    'Row used in integration tests',
    99.00,
    '200',
    TRUE,
    FALSE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM organisations WHERE organisation_id = '72590a0d-deb9-4fcc-a05a-e40fb47afc43';
DELETE FROM users WHERE user_id = 'f8584d07-ec38-488b-9521-64da6fae19ee';
-- +goose StatementEnd
