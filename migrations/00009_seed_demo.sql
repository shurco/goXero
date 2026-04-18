-- +goose Up
-- +goose StatementBegin
-- Seed demo organisation + admin user for local development.
-- Stable UUIDs (v4, generated): organisation 6823b27b-c48f-4099-bb27-4202a4f496a2, user e906a37e-41c0-4b9d-b374-a34052b3b7d1
INSERT INTO organisations (organisation_id, name, legal_name, short_code, country_code, base_currency, timezone, is_demo_company)
VALUES ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'Demo Company (Global)', 'Demo Company Global Ltd', 'DEMO', 'US', 'USD', 'UTC', TRUE)
ON CONFLICT DO NOTHING;

-- password: admin123 (bcrypt)
INSERT INTO users (user_id, email, password_hash, first_name, last_name, is_subscriber)
VALUES ('e906a37e-41c0-4b9d-b374-a34052b3b7d1',
        'admin@demo.local',
        '$2a$10$pAimBhbqKiEBvTXqKhhWlOfbgNNFoa5o3GlGLR9EGxKh5hedcwUVK',
        'Admin', 'User', TRUE)
ON CONFLICT DO NOTHING;

INSERT INTO organisation_users (organisation_id, user_id, role)
VALUES ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'e906a37e-41c0-4b9d-b374-a34052b3b7d1', 'ADMIN')
ON CONFLICT DO NOTHING;

-- Demo chart of accounts (simplified Xero defaults)
INSERT INTO accounts (organisation_id, code, name, type, status, tax_type, class, system_account) VALUES
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '090', 'Business Bank Account', 'BANK',     'ACTIVE', 'NONE',          'ASSET',   NULL),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '200', 'Sales',                 'REVENUE',  'ACTIVE', 'OUTPUT',        'REVENUE', NULL),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '260', 'Other Revenue',         'REVENUE',  'ACTIVE', 'OUTPUT',        'REVENUE', NULL),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '310', 'Cost of Goods Sold',    'DIRECTCOSTS','ACTIVE','INPUT',        'EXPENSE', NULL),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '400', 'Advertising',           'EXPENSE',  'ACTIVE', 'INPUT',         'EXPENSE', NULL),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '404', 'Bank Fees',             'EXPENSE',  'ACTIVE', 'INPUT',         'EXPENSE', NULL),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '610', 'Accounts Receivable',   'CURRENT',  'ACTIVE', 'NONE',          'ASSET',   'DEBTORS'),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '800', 'Accounts Payable',      'CURRLIAB', 'ACTIVE', 'NONE',          'LIABILITY','CREDITORS')
ON CONFLICT DO NOTHING;

-- Default tax rates
INSERT INTO tax_rates (organisation_id, name, tax_type, report_tax_type, display_tax_rate, effective_rate) VALUES
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'Tax Exempt',        'NONE',     'NONE',     0,     0),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'Tax on Sales',      'OUTPUT',   'OUTPUT',   8.25,  8.25),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'Tax on Purchases',  'INPUT',    'INPUT',    8.25,  8.25)
ON CONFLICT DO NOTHING;

-- Sample contacts
INSERT INTO contacts (organisation_id, name, first_name, last_name, email_address, is_customer, is_supplier) VALUES
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'ABC Consulting',   'Alice', 'Smith',   'alice@abc.example',  TRUE,  FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'Global Supplies',  'Bob',   'Johnson', 'bob@global.example', FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'Acme Corporation', 'Carol', 'Williams','carol@acme.example', TRUE,  FALSE)
ON CONFLICT DO NOTHING;

-- Sample items
INSERT INTO items (organisation_id, code, name, description, sales_unit_price, sales_account_code, purchase_unit_price, purchase_account_code, is_sold, is_purchased) VALUES
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'CONS-01', 'Consulting Hour', 'Professional consulting', 150.00, '200', NULL,   NULL, TRUE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', 'WIDGET',  'Widget',          'Standard widget',          25.00, '200', 10.00, '310', TRUE, TRUE)
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM organisations WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2';
DELETE FROM users WHERE user_id = 'e906a37e-41c0-4b9d-b374-a34052b3b7d1';
-- +goose StatementEnd
