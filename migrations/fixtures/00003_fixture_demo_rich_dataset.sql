-- +goose Up
-- +goose StatementBegin
-- Rich dev dataset for the core seed org `6823b27b-c48f-4099-bb27-4202a4f496a2` (admin@demo.local / admin123).
-- Covers: organisation profile, extra bank account, contacts, sales invoices + bills, payments,
-- bank transactions, bank rules, bank feed + statement lines, quote, purchase order, manual journal, bank transfer.
-- Tripled row volume: extra block after the base dataset (contacts, invoices, bank activity, quotes/PO/MJ/transfers).
-- Depends on migrations/00009_seed_demo.sql (same database).

UPDATE organisations
SET
    description = 'Fixture-rich demo tenant for UI and manual QA.',
    financial_year_end_day = 31,
    financial_year_end_month = 3,
    tax_number = 'US12-3456789',
    profile = jsonb_build_object(
        'ShowExtraOnInvoices', true,
        'SameAsPostal', false,
        'Postal', jsonb_build_object(
            'AddressLine1', '1 Demo Plaza',
            'City', 'Austin',
            'Region', 'TX',
            'PostalCode', '73301',
            'Country', 'US',
            'Attention', 'Finance'
        ),
        'Physical', jsonb_build_object(
            'AddressLine1', '200 Warehouse Way',
            'City', 'Dallas',
            'Region', 'TX',
            'PostalCode', '75201',
            'Country', 'US'
        ),
        'Telephone', jsonb_build_object('PhoneCountryCode', '1', 'PhoneNumber', '5550100'),
        'Email', 'billing@demo.local',
        'Website', 'https://demo.local',
        'Social', jsonb_build_object('Facebook', 'https://facebook.com/demo', 'Twitter', '@democo')
    )
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2';

UPDATE accounts
SET bank_account_number = '132435465', bank_account_type = 'BANK'
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '090';

INSERT INTO accounts (
    account_id, organisation_id, code, name, type, status, tax_type, class,
    bank_account_number, bank_account_type, currency_code
) VALUES (
    'f2a30001-0001-4000-8000-000000000091',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    '091',
    'Savings Account',
    'BANK',
    'ACTIVE',
    'NONE',
    'ASSET',
    '987654321',
    'BANK',
    'USD'
)
ON CONFLICT (organisation_id, code) DO UPDATE SET
    name = EXCLUDED.name,
    bank_account_number = EXCLUDED.bank_account_number;

INSERT INTO contacts (contact_id, organisation_id, name, first_name, last_name, email_address, is_customer, is_supplier)
VALUES
    ('f2a30002-0002-4000-8000-000000000201', '6823b27b-c48f-4099-bb27-4202a4f496a2', 'Fixture Retail Partner', 'Riley', 'Nguyen', 'riley@fixture-retail.test', TRUE, FALSE),
    ('f2a30002-0002-4000-8000-000000000202', '6823b27b-c48f-4099-bb27-4202a4f496a2', 'Fixture Wholesale Co', 'Sam', 'Okafor', 'ap@fixture-wholesale.test', FALSE, TRUE);

-- Sales invoices (ACCREC)
INSERT INTO invoices (
    invoice_id, organisation_id, type, contact_id, invoice_number, reference, currency_code, status,
    line_amount_types, date, due_date, sub_total, total_tax, total, total_discount, amount_due, amount_paid,
    fully_paid_on_date, sent_to_contact
) VALUES
    (
        'f2a30101-0101-4101-8101-000000000001',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'ABC Consulting' LIMIT 1),
        'DEMO-5001',
        'fixture-draft-sale',
        'USD',
        'DRAFT',
        'Exclusive',
        CURRENT_DATE - 5,
        CURRENT_DATE + 25,
        150.0000,
        12.3750,
        162.3750,
        0,
        162.3750,
        0,
        NULL,
        FALSE
    ),
    (
        'f2a30101-0101-4101-8101-000000000002',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Acme Corporation' LIMIT 1),
        'DEMO-5002',
        'fixture-authorised-sale',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 12,
        CURRENT_DATE + 18,
        50.0000,
        4.1250,
        54.1250,
        0,
        54.1250,
        0,
        NULL,
        TRUE
    ),
    (
        'f2a30101-0101-4101-8101-000000000003',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        'f2a30002-0002-4000-8000-000000000201',
        'DEMO-5003',
        'fixture-overdue',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 45,
        CURRENT_DATE - 10,
        200.0000,
        16.5000,
        216.5000,
        0,
        216.5000,
        0,
        NULL,
        FALSE
    ),
    (
        'f2a30101-0101-4101-8101-000000000004',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'ABC Consulting' LIMIT 1),
        'DEMO-5004',
        'fixture-paid-sale',
        'USD',
        'PAID',
        'Exclusive',
        CURRENT_DATE - 20,
        CURRENT_DATE + 10,
        100.0000,
        8.2500,
        108.2500,
        0,
        0,
        108.2500,
        CURRENT_DATE - 15,
        TRUE
    );

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000001',
    'f2a30101-0101-4101-8101-000000000001',
    0,
    'Consulting (fixture)',
    1,
    150.0000,
    'CONS-01',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    12.3750,
    150.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'CONS-01'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000002',
    'f2a30101-0101-4101-8101-000000000002',
    0,
    'Widgets (fixture)',
    2,
    25.0000,
    'WIDGET',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    4.1250,
    50.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000003',
    'f2a30101-0101-4101-8101-000000000003',
    0,
    'Consulting — overdue fixture',
    1,
    200.0000,
    'CONS-01',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    16.5000,
    200.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'CONS-01'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000004',
    'f2a30101-0101-4101-8101-000000000004',
    0,
    'Paid invoice line',
    1,
    100.0000,
    'CONS-01',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    8.2500,
    100.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'CONS-01'
LIMIT 1;

-- Bills (ACCPAY)
INSERT INTO invoices (
    invoice_id, organisation_id, type, contact_id, invoice_number, reference, currency_code, status,
    line_amount_types, date, due_date, sub_total, total_tax, total, total_discount, amount_due, amount_paid,
    fully_paid_on_date
) VALUES
    (
        'f2a30101-0101-4101-8101-000000000005',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Global Supplies' LIMIT 1),
        'BILL-6001',
        'fixture-draft-bill',
        'USD',
        'DRAFT',
        'Exclusive',
        CURRENT_DATE - 3,
        CURRENT_DATE + 27,
        200.0000,
        16.5000,
        216.5000,
        0,
        216.5000,
        0,
        NULL
    ),
    (
        'f2a30101-0101-4101-8101-000000000006',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Global Supplies' LIMIT 1),
        'BILL-6002',
        'fixture-authorised-bill',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 8,
        CURRENT_DATE + 22,
        500.0000,
        41.2500,
        541.2500,
        0,
        541.2500,
        0,
        NULL
    ),
    (
        'f2a30101-0101-4101-8101-000000000007',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        'f2a30002-0002-4000-8000-000000000202',
        'BILL-6003',
        'fixture-paid-bill',
        'USD',
        'PAID',
        'Exclusive',
        CURRENT_DATE - 18,
        CURRENT_DATE + 12,
        300.0000,
        24.7500,
        324.7500,
        0,
        0,
        324.7500,
        CURRENT_DATE - 12
    );

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000005',
    'f2a30101-0101-4101-8101-000000000005',
    0,
    'Inventory purchase (fixture)',
    20,
    10.0000,
    'WIDGET',
    '500',
    i.item_id,
    a.account_id,
    'INPUT',
    16.5000,
    200.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.purchase_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, account_code,
    account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000006',
    'f2a30101-0101-4101-8101-000000000006',
    0,
    'Marketing campaign (fixture)',
    1,
    500.0000,
    '600',
    a.account_id,
    'INPUT',
    41.2500,
    500.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '600'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000007',
    'f2a30101-0101-4101-8101-000000000007',
    0,
    'Stock purchase (paid)',
    30,
    10.0000,
    'WIDGET',
    '500',
    i.item_id,
    a.account_id,
    'INPUT',
    24.7500,
    300.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.purchase_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

-- Payments (customer receipt + supplier payment)
INSERT INTO payments (
    payment_id, organisation_id, invoice_id, account_id, payment_type, status, date, amount, reference, is_reconciled
)
SELECT
    'f2a30201-0303-4303-8303-000000000001',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30101-0101-4101-8101-000000000004',
    a.account_id,
    'ACCRECPAYMENT',
    'AUTHORISED',
    CURRENT_DATE - 15,
    108.2500,
    'Fixture customer payment',
    TRUE
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO payments (
    payment_id, organisation_id, invoice_id, account_id, payment_type, status, date, amount, reference, is_reconciled
)
SELECT
    'f2a30201-0303-4303-8303-000000000002',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30101-0101-4101-8101-000000000007',
    a.account_id,
    'ACCPAYMENT',
    'AUTHORISED',
    CURRENT_DATE - 12,
    324.7500,
    'Fixture supplier payment',
    TRUE
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

-- Bank transactions on 090 (mix RECEIVE / SPEND, reconciled flags)
INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000001',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    FALSE,
    CURRENT_DATE - 2,
    'MONTHLY SERVICE CHARGE',
    'USD',
    'AUTHORISED',
    'Exclusive',
    12.5000,
    0,
    12.5000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000001',
    'f2a30301-0404-4404-8404-000000000001',
    'Bank fee',
    1,
    12.5000,
    '604',
    a.account_id,
    'INPUT',
    0,
    12.5000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '604'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000002',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    TRUE,
    CURRENT_DATE - 7,
    'GOOGLE ADS',
    'USD',
    'AUTHORISED',
    'Exclusive',
    50.0000,
    0,
    50.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000002',
    'f2a30301-0404-4404-8404-000000000002',
    'Advertising',
    1,
    50.0000,
    '600',
    a.account_id,
    'INPUT',
    0,
    50.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '600'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000003',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'ABC Consulting' LIMIT 1),
    a.account_id,
    'RECEIVE',
    TRUE,
    CURRENT_DATE - 9,
    'INV-ABC-7788',
    'USD',
    'AUTHORISED',
    'Exclusive',
    500.0000,
    0,
    500.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000003',
    'f2a30301-0404-4404-8404-000000000003',
    'Customer receipt (fixture)',
    1,
    500.0000,
    '400',
    a.account_id,
    'OUTPUT',
    0,
    500.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '400'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000004',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    FALSE,
    CURRENT_DATE - 4,
    'STRIPE *SUBSCRIPTION',
    'USD',
    'AUTHORISED',
    'Exclusive',
    89.9900,
    0,
    89.9900
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000004',
    'f2a30301-0404-4404-8404-000000000004',
    'Stripe charge (rule match)',
    1,
    89.9900,
    '604',
    a.account_id,
    'INPUT',
    0,
    89.9900
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '604'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000005',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Fixture Retail Partner' LIMIT 1),
    a.account_id,
    'RECEIVE',
    FALSE,
    CURRENT_DATE - 1,
    'EFT PAYMENT',
    'USD',
    'AUTHORISED',
    'Exclusive',
    250.0000,
    0,
    250.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000005',
    'f2a30301-0404-4404-8404-000000000005',
    'Unreconciled receipt',
    1,
    250.0000,
    '400',
    a.account_id,
    'OUTPUT',
    0,
    250.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '400'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000006',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Global Supplies' LIMIT 1),
    a.account_id,
    'SPEND',
    TRUE,
    CURRENT_DATE - 11,
    'ACH TO SUPPLIER',
    'USD',
    'AUTHORISED',
    'Exclusive',
    175.0000,
    0,
    175.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000006',
    'f2a30301-0404-4404-8404-000000000006',
    'Supplier payment (fixture)',
    1,
    175.0000,
    '200',
    a.account_id,
    'NONE',
    0,
    175.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '200'
LIMIT 1;

-- Bank rules (JSON matches internal/models.BankRuleDefinition)
INSERT INTO bank_rules (bank_rule_id, organisation_id, rule_type, name, definition, is_active)
SELECT
    'f2a30401-0606-4606-8606-000000000001',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'SPEND',
    'Fixture: Stripe text → Bank Fees',
    jsonb_build_object(
        'MatchMode', 'ALL',
        'Conditions', jsonb_build_array(
            jsonb_build_object('Field', 'Reference', 'Operator', 'CONTAINS', 'Value', 'STRIPE')
        ),
        'RunOn', 'IMPORTED',
        'FixedLines', jsonb_build_array(
            jsonb_build_object(
                'Description', 'Categorised bank fee',
                'AccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '604' LIMIT 1)
            )
        ),
        'ScopeBankAccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '090' LIMIT 1)
    ),
    TRUE;

INSERT INTO bank_rules (bank_rule_id, organisation_id, rule_type, name, definition, is_active)
SELECT
    'f2a30401-0606-4606-8606-000000000002',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'RECEIVE',
    'Fixture: EFT → Sales',
    jsonb_build_object(
        'MatchMode', 'ANY',
        'Conditions', jsonb_build_array(
            jsonb_build_object('Field', 'Reference', 'Operator', 'CONTAINS', 'Value', 'EFT')
        ),
        'RunOn', 'IMPORTED',
        'FixedLines', jsonb_build_array(
            jsonb_build_object(
                'Description', 'General income',
                'AccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '400' LIMIT 1)
            )
        ),
        'ScopeBankAccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '090' LIMIT 1)
    ),
    TRUE;

-- Bank feed (linked to 090)
INSERT INTO bank_feed_connections (
    connection_id, organisation_id, provider, status, institution_name, external_reference, last_synced_at
) VALUES (
    'f2a30501-0707-4707-8707-000000000001',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'gocardless',
    'LINKED',
    'Fixture Community Bank',
    'fixture-requisition-demo-001',
    now() - interval '2 hours'
);

INSERT INTO bank_feed_accounts (
    feed_account_id, connection_id, organisation_id, account_id, external_account_id, display_name, iban, currency_code, balance
)
SELECT
    'f2a30502-0808-4808-8808-000000000001',
    'f2a30501-0707-4707-8707-000000000001',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    a.account_id,
    'ext-feed-acct-090',
    'Business operating (feed)',
    'US00DEMO090000000001',
    'USD',
    12500.5000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_feed_statement_lines (
    statement_line_id, organisation_id, feed_account_id, provider_tx_id, posted_at, amount, currency_code,
    description, counterparty, reference, status
)
VALUES
    (
        'f2a30601-0909-4909-8909-000000000001',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'f2a30502-0808-4808-8808-000000000001',
        'fixture-tx-line-001',
        CURRENT_DATE - 1,
        -45.0000,
        'USD',
        'Card purchase',
        'Coffee Shop',
        'POS-9912',
        'NEW'
    ),
    (
        'f2a30601-0909-4909-8909-000000000002',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'f2a30502-0808-4808-8808-000000000001',
        'fixture-tx-line-002',
        CURRENT_DATE - 3,
        1200.0000,
        'USD',
        'Incoming transfer',
        'Acme Corporation',
        'WIRE-4411',
        'NEW'
    ),
    (
        'f2a30601-0909-4909-8909-000000000003',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'f2a30502-0808-4808-8808-000000000001',
        'fixture-tx-line-003',
        CURRENT_DATE - 6,
        -300.0000,
        'USD',
        'Payroll debit',
        'Payroll Provider',
        'ACH-PAY-07',
        'IMPORTED'
    );

-- Quote + PO + manual journal
INSERT INTO quotes (
    quote_id, organisation_id, contact_id, quote_number, reference, title, date, expiry_date, currency_code,
    status, line_amount_types, sub_total, total_tax, total, total_discount
)
SELECT
    'f2a30701-0101-4101-8101-000000000801',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Acme Corporation' LIMIT 1),
    'QUO-DEMO-9001',
    'fixture-quote',
    'Enterprise package',
    CURRENT_DATE - 2,
    CURRENT_DATE + 28,
    'USD',
    'SENT',
    'Exclusive',
    1200.0000,
    99.0000,
    1299.0000,
    0;

INSERT INTO quote_line_items (
    line_item_id, quote_id, sort_order, description, quantity, unit_amount, item_code, account_code, tax_type, tax_amount, line_amount
)
VALUES (
    'f2a30702-0202-4202-8202-000000000802',
    'f2a30701-0101-4101-8101-000000000801',
    0,
    'Implementation + training',
    40,
    30.0000,
    'CONS-01',
    '400',
    'OUTPUT',
    99.0000,
    1200.0000
);

INSERT INTO purchase_orders (
    purchase_order_id, organisation_id, contact_id, purchase_order_number, reference, date, delivery_date,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30801-0303-4303-8303-000000000811',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30002-0002-4000-8000-000000000202',
    'PO-DEMO-8001',
    'fixture-po',
    CURRENT_DATE - 4,
    CURRENT_DATE + 10,
    'USD',
    'AUTHORISED',
    'Exclusive',
    400.0000,
    33.0000,
    433.0000;

INSERT INTO purchase_order_line_items (
    line_item_id, purchase_order_id, sort_order, description, quantity, unit_amount, item_code, account_code, tax_type, tax_amount, line_amount
)
VALUES (
    'f2a30802-0404-4404-8404-000000000812',
    'f2a30801-0303-4303-8303-000000000811',
    0,
    'Widget stock PO',
    40,
    10.0000,
    'WIDGET',
    '500',
    'INPUT',
    33.0000,
    400.0000
);

INSERT INTO manual_journals (
    manual_journal_id, organisation_id, narration, date, line_amount_types, status, show_on_cash_basis_reports
) VALUES (
    'f2a30901-0505-4505-8505-000000000821',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'Fixture: reclassify prepaid advertising',
    CURRENT_DATE - 6,
    'Exclusive',
    'POSTED',
    FALSE
);

INSERT INTO manual_journal_lines (line_id, manual_journal_id, description, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30902-0606-4606-8606-000000000822',
    'f2a30901-0505-4505-8505-000000000821',
    'Debit advertising',
    '600',
    a.account_id,
    'NONE',
    0,
    75.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '600'
LIMIT 1;

INSERT INTO manual_journal_lines (line_id, manual_journal_id, description, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30902-0606-4606-8606-000000000823',
    'f2a30901-0505-4505-8505-000000000821',
    'Credit other revenue',
    '460',
    a.account_id,
    'NONE',
    0,
    -75.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '460'
LIMIT 1;

INSERT INTO bank_transfers (
    bank_transfer_id, organisation_id, from_bank_account_id, to_bank_account_id, amount, date, reference
)
SELECT
    'f2a31001-0707-4707-8707-000000000831',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    a090.account_id,
    a091.account_id,
    500.0000,
    CURRENT_DATE - 14,
    'Fixture sweep to savings'
FROM accounts a090
JOIN accounts a091 ON a091.organisation_id = a090.organisation_id AND a091.code = '091'
WHERE a090.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a090.code = '090'
LIMIT 1;


-- ── Tripled volume (×3): additional rows (same org). IDs are deterministic. ──
INSERT INTO contacts (contact_id, organisation_id, name, first_name, last_name, email_address, is_customer, is_supplier)
VALUES
    ('f2a30002-0002-4000-8000-000000000203', '6823b27b-c48f-4099-bb27-4202a4f496a2', 'Fixture Northwind LLC', 'Nora', 'Winters', 'hello@northwind-fixture.test', TRUE, FALSE),
    ('f2a30002-0002-4000-8000-000000000204', '6823b27b-c48f-4099-bb27-4202a4f496a2', 'Fixture Southbay Traders', 'Paul', 'Reyes', 'ap@southbay-fixture.test', FALSE, TRUE),
    ('f2a30002-0002-4000-8000-000000000205', '6823b27b-c48f-4099-bb27-4202a4f496a2', 'Fixture Metro Clinic', 'Morgan', 'Lee', 'billing@metroclinic-fixture.test', TRUE, TRUE),
    ('f2a30002-0002-4000-8000-000000000206', '6823b27b-c48f-4099-bb27-4202a4f496a2', 'Fixture Cloud SaaS Inc', 'Casey', 'Jordan', 'finance@cloudsaas-fixture.test', TRUE, FALSE);

INSERT INTO invoices (
    invoice_id, organisation_id, type, contact_id, invoice_number, reference, currency_code, status,
    line_amount_types, date, due_date, sub_total, total_tax, total, total_discount, amount_due, amount_paid,
    fully_paid_on_date, sent_to_contact
) VALUES
    (
        'f2a30101-0101-4101-8101-000000000008',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'ABC Consulting' LIMIT 1),
        'DEMO-5011',
        'fixture-x-draft',
        'USD',
        'DRAFT',
        'Exclusive',
        CURRENT_DATE - 6,
        CURRENT_DATE + 20,
        120.0000,
        9.9000,
        129.9000,
        0,
        129.9000,
        0.0000,
        NULL,
        FALSE
    ),
    (
        'f2a30101-0101-4101-8101-000000000009',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Acme Corporation' LIMIT 1),
        'DEMO-5012',
        'fixture-x-auth',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 10,
        CURRENT_DATE + 25,
        300.0000,
        24.7500,
        324.7500,
        0,
        324.7500,
        0.0000,
        NULL,
        TRUE
    ),
    (
        'f2a30101-0101-4101-8101-000000000010',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        'f2a30002-0002-4000-8000-000000000201',
        'DEMO-5013',
        'fixture-x-od',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 40,
        CURRENT_DATE + -5,
        400.0000,
        33.0000,
        433.0000,
        0,
        433.0000,
        0.0000,
        NULL,
        FALSE
    ),
    (
        'f2a30101-0101-4101-8101-000000000011',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'ABC Consulting' LIMIT 1),
        'DEMO-5014',
        'fixture-x-paid-a',
        'USD',
        'PAID',
        'Exclusive',
        CURRENT_DATE - 22,
        CURRENT_DATE + 12,
        175.0000,
        14.4375,
        189.4375,
        0,
        0.0000,
        189.4375,
        CURRENT_DATE - 16,
        TRUE
    ),
    (
        'f2a30101-0101-4101-8101-000000000012',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        'f2a30002-0002-4000-8000-000000000206',
        'DEMO-5015',
        'fixture-x-paid-b',
        'USD',
        'PAID',
        'Exclusive',
        CURRENT_DATE - 19,
        CURRENT_DATE + 15,
        220.0000,
        18.1500,
        238.1500,
        0,
        0.0000,
        238.1500,
        CURRENT_DATE - 9,
        TRUE
    ),
    (
        'f2a30101-0101-4101-8101-000000000013',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        'f2a30002-0002-4000-8000-000000000203',
        'DEMO-5016',
        'fixture-x-draft-2',
        'USD',
        'DRAFT',
        'Exclusive',
        CURRENT_DATE - 4,
        CURRENT_DATE + 28,
        88.0000,
        7.2600,
        95.2600,
        0,
        95.2600,
        0.0000,
        NULL,
        FALSE
    ),
    (
        'f2a30101-0101-4101-8101-000000000014',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        'f2a30002-0002-4000-8000-000000000205',
        'DEMO-5017',
        'fixture-x-auth-2',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 9,
        CURRENT_DATE + 30,
        150.0000,
        12.3750,
        162.3750,
        0,
        162.3750,
        0.0000,
        NULL,
        TRUE
    ),
    (
        'f2a30101-0101-4101-8101-000000000015',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCREC',
        'f2a30002-0002-4000-8000-000000000204',
        'DEMO-5018',
        'fixture-x-mix',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 11,
        CURRENT_DATE + 24,
        90.0000,
        7.4250,
        97.4250,
        0,
        97.4250,
        0.0000,
        NULL,
        FALSE
    );

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000008',
    'f2a30101-0101-4101-8101-000000000008',
    0,
    'Extra consulting A',
    1,
    120.0000,
    'CONS-01',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    9.9000,
    120.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'CONS-01'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000009',
    'f2a30101-0101-4101-8101-000000000009',
    0,
    'Extra widgets batch',
    6,
    50.0000,
    'WIDGET',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    24.7500,
    300.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000010',
    'f2a30101-0101-4101-8101-000000000010',
    0,
    'Overdue consulting',
    1,
    400.0000,
    'CONS-01',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    33.0000,
    400.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'CONS-01'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000011',
    'f2a30101-0101-4101-8101-000000000011',
    0,
    'Paid line A',
    1,
    175.0000,
    'CONS-01',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    14.4375,
    175.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'CONS-01'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000012',
    'f2a30101-0101-4101-8101-000000000012',
    0,
    'Paid line B',
    4,
    55.0000,
    'WIDGET',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    18.1500,
    220.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000013',
    'f2a30101-0101-4101-8101-000000000013',
    0,
    'Draft services',
    2,
    44.0000,
    'CONS-01',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    7.2600,
    88.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'CONS-01'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000014',
    'f2a30101-0101-4101-8101-000000000014',
    0,
    'Clinic retainer',
    1,
    150.0000,
    'CONS-01',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    12.3750,
    150.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'CONS-01'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000015',
    'f2a30101-0101-4101-8101-000000000015',
    0,
    'Trade sale',
    10,
    9.0000,
    'WIDGET',
    '400',
    i.item_id,
    a.account_id,
    'OUTPUT',
    7.4250,
    90.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.sales_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoices (
    invoice_id, organisation_id, type, contact_id, invoice_number, reference, currency_code, status,
    line_amount_types, date, due_date, sub_total, total_tax, total, total_discount, amount_due, amount_paid,
    fully_paid_on_date
) VALUES
    (
        'f2a30101-0101-4101-8101-000000000016',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Global Supplies' LIMIT 1),
        'BILL-6011',
        'fixture-x-bill-draft',
        'USD',
        'DRAFT',
        'Exclusive',
        CURRENT_DATE - 2,
        CURRENT_DATE + 26,
        120.0000,
        9.9000,
        129.9000,
        0,
        129.9000,
        0.0000,
        NULL
    ),
    (
        'f2a30101-0101-4101-8101-000000000017',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Global Supplies' LIMIT 1),
        'BILL-6012',
        'fixture-x-bill-auth',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 5,
        CURRENT_DATE + 24,
        250.0000,
        20.6250,
        270.6250,
        0,
        270.6250,
        0.0000,
        NULL
    ),
    (
        'f2a30101-0101-4101-8101-000000000018',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        'f2a30002-0002-4000-8000-000000000202',
        'BILL-6013',
        'fixture-x-bill-wh',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 9,
        CURRENT_DATE + 18,
        180.0000,
        14.8500,
        194.8500,
        0,
        194.8500,
        0.0000,
        NULL
    ),
    (
        'f2a30101-0101-4101-8101-000000000019',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        'f2a30002-0002-4000-8000-000000000204',
        'BILL-6014',
        'fixture-x-bill-paid-a',
        'USD',
        'PAID',
        'Exclusive',
        CURRENT_DATE - 17,
        CURRENT_DATE + 11,
        140.0000,
        11.5500,
        151.5500,
        0,
        0.0000,
        151.5500,
        CURRENT_DATE - 8
    ),
    (
        'f2a30101-0101-4101-8101-000000000020',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Global Supplies' LIMIT 1),
        'BILL-6015',
        'fixture-x-bill-paid-b',
        'USD',
        'PAID',
        'Exclusive',
        CURRENT_DATE - 21,
        CURRENT_DATE + 8,
        95.0000,
        7.8375,
        102.8375,
        0,
        0.0000,
        102.8375,
        CURRENT_DATE - 5
    ),
    (
        'f2a30101-0101-4101-8101-000000000021',
        '6823b27b-c48f-4099-bb27-4202a4f496a2',
        'ACCPAY',
        'f2a30002-0002-4000-8000-000000000203',
        'BILL-6016',
        'fixture-x-bill-nw',
        'USD',
        'AUTHORISED',
        'Exclusive',
        CURRENT_DATE - 6,
        CURRENT_DATE + 22,
        310.0000,
        25.5750,
        335.5750,
        0,
        335.5750,
        0.0000,
        NULL
    );

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000016',
    'f2a30101-0101-4101-8101-000000000016',
    0,
    'Fixture purchase (000000000016)',
    12,
    10.0000,
    'WIDGET',
    '500',
    i.item_id,
    a.account_id,
    'INPUT',
    9.9000,
    120.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.purchase_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, account_code,
    account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000017',
    'f2a30101-0101-4101-8101-000000000017',
    0,
    'Fixture marketing (000000000017)',
    1,
    250.0000,
    '600',
    a.account_id,
    'INPUT',
    20.6250,
    250.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '600'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000018',
    'f2a30101-0101-4101-8101-000000000018',
    0,
    'Fixture purchase (000000000018)',
    15,
    12.0000,
    'WIDGET',
    '500',
    i.item_id,
    a.account_id,
    'INPUT',
    14.8500,
    180.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.purchase_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000019',
    'f2a30101-0101-4101-8101-000000000019',
    0,
    'Fixture purchase (000000000019)',
    10,
    14.0000,
    'WIDGET',
    '500',
    i.item_id,
    a.account_id,
    'INPUT',
    11.5500,
    140.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.purchase_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000020',
    'f2a30101-0101-4101-8101-000000000020',
    0,
    'Fixture purchase (000000000020)',
    19,
    5.0000,
    'WIDGET',
    '500',
    i.item_id,
    a.account_id,
    'INPUT',
    7.8375,
    95.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.purchase_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO invoice_line_items (
    line_item_id, invoice_id, sort_order, description, quantity, unit_amount, item_code, account_code,
    item_id, account_id, tax_type, tax_amount, line_amount
)
SELECT
    'f2a30102-0202-4202-8202-000000000021',
    'f2a30101-0101-4101-8101-000000000021',
    0,
    'Fixture purchase (000000000021)',
    31,
    10.0000,
    'WIDGET',
    '500',
    i.item_id,
    a.account_id,
    'INPUT',
    25.5750,
    310.0000
FROM items i
JOIN accounts a ON a.organisation_id = i.organisation_id AND a.code = i.purchase_account_code
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.code = 'WIDGET'
LIMIT 1;

INSERT INTO payments (
    payment_id, organisation_id, invoice_id, account_id, payment_type, status, date, amount, reference, is_reconciled
)
SELECT
    'f2a30201-0303-4303-8303-000000000003',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30101-0101-4101-8101-000000000011',
    a.account_id,
    'ACCRECPAYMENT',
    'AUTHORISED',
    CURRENT_DATE - 7,
    189.4375,
    'Fixture pay extra A',
    TRUE
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO payments (
    payment_id, organisation_id, invoice_id, account_id, payment_type, status, date, amount, reference, is_reconciled
)
SELECT
    'f2a30201-0303-4303-8303-000000000004',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30101-0101-4101-8101-000000000012',
    a.account_id,
    'ACCRECPAYMENT',
    'AUTHORISED',
    CURRENT_DATE - 7,
    238.1500,
    'Fixture pay extra B',
    TRUE
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO payments (
    payment_id, organisation_id, invoice_id, account_id, payment_type, status, date, amount, reference, is_reconciled
)
SELECT
    'f2a30201-0303-4303-8303-000000000005',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30101-0101-4101-8101-000000000019',
    a.account_id,
    'ACCPAYMENT',
    'AUTHORISED',
    CURRENT_DATE - 7,
    151.5500,
    'Fixture supplier pay A',
    TRUE
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO payments (
    payment_id, organisation_id, invoice_id, account_id, payment_type, status, date, amount, reference, is_reconciled
)
SELECT
    'f2a30201-0303-4303-8303-000000000006',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30101-0101-4101-8101-000000000020',
    a.account_id,
    'ACCPAYMENT',
    'AUTHORISED',
    CURRENT_DATE - 7,
    102.8375,
    'Fixture supplier pay B',
    TRUE
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000007',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    FALSE,
    CURRENT_DATE - 3,
    'UBER TRIP',
    'USD',
    'AUTHORISED',
    'Exclusive',
    18.20,
    0,
    18.20
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000007',
    'f2a30301-0404-4404-8404-000000000007',
    'UBER TRIP line',
    1,
    18.20,
    '604',
    acc.account_id,
    'NONE',
    0,
    18.20
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '604'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000008',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    TRUE,
    CURRENT_DATE - 6,
    'AWS EMEA',
    'USD',
    'AUTHORISED',
    'Exclusive',
    240.00,
    0,
    240.00
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000008',
    'f2a30301-0404-4404-8404-000000000008',
    'AWS EMEA line',
    1,
    240.00,
    '600',
    acc.account_id,
    'NONE',
    0,
    240.00
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '600'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000009',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'RECEIVE',
    TRUE,
    CURRENT_DATE - 8,
    'WIRE IN REF-77',
    'USD',
    'AUTHORISED',
    'Exclusive',
    1200.00,
    0,
    1200.00
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000009',
    'f2a30301-0404-4404-8404-000000000009',
    'WIRE IN REF-77 line',
    1,
    1200.00,
    '400',
    acc.account_id,
    'NONE',
    0,
    1200.00
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '400'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000010',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    FALSE,
    CURRENT_DATE - 1,
    'OFFICE DEPOT',
    'USD',
    'AUTHORISED',
    'Exclusive',
    67.45,
    0,
    67.45
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000010',
    'f2a30301-0404-4404-8404-000000000010',
    'OFFICE DEPOT line',
    1,
    67.45,
    '600',
    acc.account_id,
    'NONE',
    0,
    67.45
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '600'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000011',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'RECEIVE',
    FALSE,
    CURRENT_DATE - 2,
    'SHOPIFY PAYOUT',
    'USD',
    'AUTHORISED',
    'Exclusive',
    430.00,
    0,
    430.00
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000011',
    'f2a30301-0404-4404-8404-000000000011',
    'SHOPIFY PAYOUT line',
    1,
    430.00,
    '400',
    acc.account_id,
    'NONE',
    0,
    430.00
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '400'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000012',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    TRUE,
    CURRENT_DATE - 13,
    'INSURANCE PREM',
    'USD',
    'AUTHORISED',
    'Exclusive',
    199.00,
    0,
    199.00
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000012',
    'f2a30301-0404-4404-8404-000000000012',
    'INSURANCE PREM line',
    1,
    199.00,
    '600',
    acc.account_id,
    'NONE',
    0,
    199.00
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '600'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000013',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    FALSE,
    CURRENT_DATE - 5,
    'FUEL STATION',
    'USD',
    'AUTHORISED',
    'Exclusive',
    42.10,
    0,
    42.10
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000013',
    'f2a30301-0404-4404-8404-000000000013',
    'FUEL STATION line',
    1,
    42.10,
    '600',
    acc.account_id,
    'NONE',
    0,
    42.10
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '600'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000014',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Acme Corporation' LIMIT 1),
    a.account_id,
    'RECEIVE',
    TRUE,
    CURRENT_DATE - 10,
    'ACME WIRE CREDIT',
    'USD',
    'AUTHORISED',
    'Exclusive',
    880.00,
    0,
    880.00
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000014',
    'f2a30301-0404-4404-8404-000000000014',
    'ACME WIRE CREDIT line',
    1,
    880.00,
    '400',
    acc.account_id,
    'NONE',
    0,
    880.00
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '400'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000015',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    FALSE,
    CURRENT_DATE - 4,
    'SLACK TEC*',
    'USD',
    'AUTHORISED',
    'Exclusive',
    42.00,
    0,
    42.00
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000015',
    'f2a30301-0404-4404-8404-000000000015',
    'SLACK TEC* line',
    1,
    42.00,
    '604',
    acc.account_id,
    'NONE',
    0,
    42.00
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '604'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000016',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    TRUE,
    CURRENT_DATE - 12,
    'RENT ACH',
    'USD',
    'AUTHORISED',
    'Exclusive',
    2100.00,
    0,
    2100.00
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000016',
    'f2a30301-0404-4404-8404-000000000016',
    'RENT ACH line',
    1,
    2100.00,
    '600',
    acc.account_id,
    'NONE',
    0,
    2100.00
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '600'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000017',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'RECEIVE',
    FALSE,
    CURRENT_DATE - 0,
    'INTEREST CREDIT',
    'USD',
    'AUTHORISED',
    'Exclusive',
    3.21,
    0,
    3.21
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000017',
    'f2a30301-0404-4404-8404-000000000017',
    'INTEREST CREDIT line',
    1,
    3.21,
    '400',
    acc.account_id,
    'NONE',
    0,
    3.21
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '400'
LIMIT 1;

INSERT INTO bank_transactions (
    bank_transaction_id, organisation_id, contact_id, bank_account_id, type, is_reconciled, date, reference,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30301-0404-4404-8404-000000000018',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    NULL,
    a.account_id,
    'SPEND',
    TRUE,
    CURRENT_DATE - 9,
    'PAYROLL DD',
    'USD',
    'AUTHORISED',
    'Exclusive',
    5400.00,
    0,
    5400.00
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'
LIMIT 1;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id, description, quantity, unit_amount, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30302-0505-4505-8505-000000000018',
    'f2a30301-0404-4404-8404-000000000018',
    'PAYROLL DD line',
    1,
    5400.00,
    '600',
    acc.account_id,
    'NONE',
    0,
    5400.00
FROM accounts acc
WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '600'
LIMIT 1;

INSERT INTO bank_rules (bank_rule_id, organisation_id, rule_type, name, definition, is_active)
SELECT
    'f2a30401-0606-4606-8606-000000000003',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'SPEND',
    'Fixture+: AWS → Advertising',
    jsonb_build_object(
        'MatchMode', 'ALL',
        'Conditions', jsonb_build_array(
            jsonb_build_object('Field', 'Reference', 'Operator', 'CONTAINS', 'Value', 'AWS')
        ),
        'RunOn', 'IMPORTED',
        'FixedLines', jsonb_build_array(
            jsonb_build_object(
                'Description', 'Auto-categorised',
                'AccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '600' LIMIT 1)
            )
        ),
        'ScopeBankAccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '090' LIMIT 1)
    ),
    TRUE;

INSERT INTO bank_rules (bank_rule_id, organisation_id, rule_type, name, definition, is_active)
SELECT
    'f2a30401-0606-4606-8606-000000000004',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'SPEND',
    'Fixture+: Uber → Bank fees',
    jsonb_build_object(
        'MatchMode', 'ALL',
        'Conditions', jsonb_build_array(
            jsonb_build_object('Field', 'Reference', 'Operator', 'CONTAINS', 'Value', 'UBER')
        ),
        'RunOn', 'IMPORTED',
        'FixedLines', jsonb_build_array(
            jsonb_build_object(
                'Description', 'Auto-categorised',
                'AccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '604' LIMIT 1)
            )
        ),
        'ScopeBankAccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '090' LIMIT 1)
    ),
    TRUE;

INSERT INTO bank_rules (bank_rule_id, organisation_id, rule_type, name, definition, is_active)
SELECT
    'f2a30401-0606-4606-8606-000000000005',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'RECEIVE',
    'Fixture+: Shopify → Sales',
    jsonb_build_object(
        'MatchMode', 'ALL',
        'Conditions', jsonb_build_array(
            jsonb_build_object('Field', 'Reference', 'Operator', 'CONTAINS', 'Value', 'SHOPIFY')
        ),
        'RunOn', 'IMPORTED',
        'FixedLines', jsonb_build_array(
            jsonb_build_object(
                'Description', 'Auto-categorised',
                'AccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '400' LIMIT 1)
            )
        ),
        'ScopeBankAccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '090' LIMIT 1)
    ),
    TRUE;

INSERT INTO bank_rules (bank_rule_id, organisation_id, rule_type, name, definition, is_active)
SELECT
    'f2a30401-0606-4606-8606-000000000006',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'SPEND',
    'Fixture+: Rent → Overheads',
    jsonb_build_object(
        'MatchMode', 'ALL',
        'Conditions', jsonb_build_array(
            jsonb_build_object('Field', 'Reference', 'Operator', 'CONTAINS', 'Value', 'RENT')
        ),
        'RunOn', 'IMPORTED',
        'FixedLines', jsonb_build_array(
            jsonb_build_object(
                'Description', 'Auto-categorised',
                'AccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '600' LIMIT 1)
            )
        ),
        'ScopeBankAccountID', (SELECT acc.account_id::text FROM accounts acc WHERE acc.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND acc.code = '090' LIMIT 1)
    ),
    TRUE;

INSERT INTO bank_feed_statement_lines (
    statement_line_id, organisation_id, feed_account_id, provider_tx_id, posted_at, amount, currency_code,
    description, counterparty, reference, status
) VALUES (
    'f2a30601-0909-4909-8909-000000000004',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30502-0808-4808-8808-000000000001',
    'fixture-tx-line-004',
    CURRENT_DATE - 1,
    -22.50,
    'USD',
    'Parking',
    'City parking',
    'PARK-12',
    'NEW'
);

INSERT INTO bank_feed_statement_lines (
    statement_line_id, organisation_id, feed_account_id, provider_tx_id, posted_at, amount, currency_code,
    description, counterparty, reference, status
) VALUES (
    'f2a30601-0909-4909-8909-000000000005',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30502-0808-4808-8808-000000000001',
    'fixture-tx-line-005',
    CURRENT_DATE - 2,
    350.00,
    'USD',
    'Deposit',
    'Walk-in cash',
    'DEP-88',
    'NEW'
);

INSERT INTO bank_feed_statement_lines (
    statement_line_id, organisation_id, feed_account_id, provider_tx_id, posted_at, amount, currency_code,
    description, counterparty, reference, status
) VALUES (
    'f2a30601-0909-4909-8909-000000000006',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30502-0808-4808-8808-000000000001',
    'fixture-tx-line-006',
    CURRENT_DATE - 4,
    -199.99,
    'USD',
    'Software',
    'SaaS annual',
    'INV-9001',
    'IMPORTED'
);

INSERT INTO bank_feed_statement_lines (
    statement_line_id, organisation_id, feed_account_id, provider_tx_id, posted_at, amount, currency_code,
    description, counterparty, reference, status
) VALUES (
    'f2a30601-0909-4909-8909-000000000007',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30502-0808-4808-8808-000000000001',
    'fixture-tx-line-007',
    CURRENT_DATE - 5,
    75.00,
    'USD',
    'Refund',
    'Vendor credit',
    'CR-21',
    'NEW'
);

INSERT INTO bank_feed_statement_lines (
    statement_line_id, organisation_id, feed_account_id, provider_tx_id, posted_at, amount, currency_code,
    description, counterparty, reference, status
) VALUES (
    'f2a30601-0909-4909-8909-000000000008',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30502-0808-4808-8808-000000000001',
    'fixture-tx-line-008',
    CURRENT_DATE - 7,
    -1200.00,
    'USD',
    'Tax payment',
    'IRS debit',
    'TAX-EST',
    'IMPORTED'
);

INSERT INTO bank_feed_statement_lines (
    statement_line_id, organisation_id, feed_account_id, provider_tx_id, posted_at, amount, currency_code,
    description, counterparty, reference, status
) VALUES (
    'f2a30601-0909-4909-8909-000000000009',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30502-0808-4808-8808-000000000001',
    'fixture-tx-line-009',
    CURRENT_DATE - 9,
    44.00,
    'USD',
    'Rounding',
    'Adjustment',
    'ADJ-01',
    'NEW'
);

INSERT INTO quotes (
    quote_id, organisation_id, contact_id, quote_number, reference, title, date, expiry_date, currency_code,
    status, line_amount_types, sub_total, total_tax, total, total_discount
)
SELECT
    'f2a30701-0101-4101-8101-000000000902',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Acme Corporation' LIMIT 1),
    'QUO-DEMO-902',
    'fixture-quote-extra',
    'Add-on services pack',
    CURRENT_DATE - 3,
    CURRENT_DATE + 25,
    'USD',
    'SENT',
    'Exclusive',
    450.0000,
    37.1250,
    487.1250,
    0;

INSERT INTO quotes (
    quote_id, organisation_id, contact_id, quote_number, reference, title, date, expiry_date, currency_code,
    status, line_amount_types, sub_total, total_tax, total, total_discount
)
SELECT
    'f2a30701-0101-4101-8101-000000000903',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Acme Corporation' LIMIT 1),
    'QUO-DEMO-903',
    'fixture-quote-extra',
    'Support renewal',
    CURRENT_DATE - 3,
    CURRENT_DATE + 25,
    'USD',
    'SENT',
    'Exclusive',
    800.0000,
    66.0000,
    866.0000,
    0;

INSERT INTO quote_line_items (
    line_item_id, quote_id, sort_order, description, quantity, unit_amount, item_code, account_code, tax_type, tax_amount, line_amount
)
VALUES (
    'f2a30702-0202-4202-8202-000000000904',
    'f2a30701-0101-4101-8101-000000000902',
    0,
    'Professional services (extra)',
    15,
    30.0000,
    'CONS-01',
    '400',
    'OUTPUT',
    37.1250,
    450.0000
);
INSERT INTO quote_line_items (
    line_item_id, quote_id, sort_order, description, quantity, unit_amount, item_code, account_code, tax_type, tax_amount, line_amount
)
VALUES (
    'f2a30702-0202-4202-8202-000000000905',
    'f2a30701-0101-4101-8101-000000000903',
    0,
    'Annual support',
    20,
    40.0000,
    'CONS-01',
    '400',
    'OUTPUT',
    66.0000,
    800.0000
);

INSERT INTO purchase_orders (
    purchase_order_id, organisation_id, contact_id, purchase_order_number, reference, date, delivery_date,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30801-0303-4303-8303-000000000912',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'f2a30002-0002-4000-8000-000000000202',
    'PO-DEMO-8012',
    'fixture-po-extra-a',
    CURRENT_DATE - 5,
    CURRENT_DATE + 14,
    'USD',
    'AUTHORISED',
    'Exclusive',
    250.0000,
    20.6250,
    270.6250;

INSERT INTO purchase_order_line_items (
    line_item_id, purchase_order_id, sort_order, description, quantity, unit_amount, item_code, account_code, tax_type, tax_amount, line_amount
)
VALUES (
    'f2a30802-0404-4404-8404-000000000913',
    'f2a30801-0303-4303-8303-000000000912',
    0,
    'Widget stock (extra A)',
    25,
    10.0000,
    'WIDGET',
    '500',
    'INPUT',
    20.6250,
    250.0000
);

INSERT INTO purchase_orders (
    purchase_order_id, organisation_id, contact_id, purchase_order_number, reference, date, delivery_date,
    currency_code, status, line_amount_types, sub_total, total_tax, total
)
SELECT
    'f2a30801-0303-4303-8303-000000000914',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    (SELECT contact_id FROM contacts WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name = 'Global Supplies' LIMIT 1),
    'PO-DEMO-8014',
    'fixture-po-extra-b',
    CURRENT_DATE - 2,
    CURRENT_DATE + 20,
    'USD',
    'DRAFT',
    'Exclusive',
    600.0000,
    49.5000,
    649.5000;

INSERT INTO purchase_order_line_items (
    line_item_id, purchase_order_id, sort_order, description, quantity, unit_amount, item_code, account_code, tax_type, tax_amount, line_amount
)
VALUES (
    'f2a30802-0404-4404-8404-000000000915',
    'f2a30801-0303-4303-8303-000000000914',
    0,
    'Inventory (extra B)',
    60,
    10.0000,
    'WIDGET',
    '500',
    'INPUT',
    49.5000,
    600.0000
);

INSERT INTO manual_journals (
    manual_journal_id, organisation_id, narration, date, line_amount_types, status, show_on_cash_basis_reports
) VALUES (
    'f2a30901-0505-4505-8505-000000000831',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'Fixture+: reclassify office supplies',
    CURRENT_DATE - 4,
    'Exclusive',
    'POSTED',
    FALSE
);
INSERT INTO manual_journal_lines (line_id, manual_journal_id, description, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30902-0606-4606-8606-000000000832',
    'f2a30901-0505-4505-8505-000000000831',
    'Debit supplies',
    '600',
    a.account_id,
    'NONE',
    0,
    42.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '600'
LIMIT 1;
INSERT INTO manual_journal_lines (line_id, manual_journal_id, description, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30902-0606-4606-8606-000000000833',
    'f2a30901-0505-4505-8505-000000000831',
    'Credit other revenue',
    '460',
    a.account_id,
    'NONE',
    0,
    -42.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '460'
LIMIT 1;

INSERT INTO manual_journals (
    manual_journal_id, organisation_id, narration, date, line_amount_types, status, show_on_cash_basis_reports
) VALUES (
    'f2a30901-0505-4505-8505-000000000841',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    'Fixture+: year-end accrual',
    CURRENT_DATE - 1,
    'Exclusive',
    'POSTED',
    FALSE
);
INSERT INTO manual_journal_lines (line_id, manual_journal_id, description, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30902-0606-4606-8606-000000000842',
    'f2a30901-0505-4505-8505-000000000841',
    'Debit accrued expense',
    '200',
    a.account_id,
    'NONE',
    0,
    125.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '200'
LIMIT 1;
INSERT INTO manual_journal_lines (line_id, manual_journal_id, description, account_code, account_id, tax_type, tax_amount, line_amount)
SELECT
    'f2a30902-0606-4606-8606-000000000843',
    'f2a30901-0505-4505-8505-000000000841',
    'Credit expense',
    '600',
    a.account_id,
    'NONE',
    0,
    -125.0000
FROM accounts a
WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '600'
LIMIT 1;

INSERT INTO bank_transfers (
    bank_transfer_id, organisation_id, from_bank_account_id, to_bank_account_id, amount, date, reference
)
SELECT
    'f2a31001-0707-4707-8707-000000000842',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    a090.account_id,
    a091.account_id,
    250.0000,
    CURRENT_DATE - 20,
    'Fixture sweep extra A'
FROM accounts a090
JOIN accounts a091 ON a091.organisation_id = a090.organisation_id AND a091.code = '091'
WHERE a090.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a090.code = '090'
LIMIT 1;

INSERT INTO bank_transfers (
    bank_transfer_id, organisation_id, from_bank_account_id, to_bank_account_id, amount, date, reference
)
SELECT
    'f2a31001-0707-4707-8707-000000000843',
    '6823b27b-c48f-4099-bb27-4202a4f496a2',
    a091.account_id,
    a090.account_id,
    100.0000,
    CURRENT_DATE - 8,
    'Fixture sweep back to operating'
FROM accounts a090
JOIN accounts a091 ON a091.organisation_id = a090.organisation_id AND a091.code = '091'
WHERE a090.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a090.code = '090'
LIMIT 1;


-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM bank_feed_statement_lines WHERE statement_line_id IN (
    'f2a30601-0909-4909-8909-000000000001',
    'f2a30601-0909-4909-8909-000000000002',
    'f2a30601-0909-4909-8909-000000000003',
    'f2a30601-0909-4909-8909-000000000004',
    'f2a30601-0909-4909-8909-000000000005',
    'f2a30601-0909-4909-8909-000000000006',
    'f2a30601-0909-4909-8909-000000000007',
    'f2a30601-0909-4909-8909-000000000008',
    'f2a30601-0909-4909-8909-000000000009'
);
DELETE FROM bank_feed_accounts WHERE feed_account_id = 'f2a30502-0808-4808-8808-000000000001';
DELETE FROM bank_feed_connections WHERE connection_id = 'f2a30501-0707-4707-8707-000000000001';

DELETE FROM bank_transfers WHERE bank_transfer_id IN (
    'f2a31001-0707-4707-8707-000000000831',
    'f2a31001-0707-4707-8707-000000000842',
    'f2a31001-0707-4707-8707-000000000843'
);

DELETE FROM manual_journal_lines WHERE line_id IN (
    'f2a30902-0606-4606-8606-000000000822',
    'f2a30902-0606-4606-8606-000000000823',
    'f2a30902-0606-4606-8606-000000000832',
    'f2a30902-0606-4606-8606-000000000833',
    'f2a30902-0606-4606-8606-000000000842',
    'f2a30902-0606-4606-8606-000000000843'
);
DELETE FROM manual_journals WHERE manual_journal_id IN (
    'f2a30901-0505-4505-8505-000000000821',
    'f2a30901-0505-4505-8505-000000000831',
    'f2a30901-0505-4505-8505-000000000841'
);

DELETE FROM purchase_order_line_items WHERE line_item_id IN (
    'f2a30802-0404-4404-8404-000000000812',
    'f2a30802-0404-4404-8404-000000000913',
    'f2a30802-0404-4404-8404-000000000915'
);
DELETE FROM purchase_orders WHERE purchase_order_id IN (
    'f2a30801-0303-4303-8303-000000000811',
    'f2a30801-0303-4303-8303-000000000912',
    'f2a30801-0303-4303-8303-000000000914'
);

DELETE FROM quote_line_items WHERE line_item_id IN (
    'f2a30702-0202-4202-8202-000000000802',
    'f2a30702-0202-4202-8202-000000000904',
    'f2a30702-0202-4202-8202-000000000905'
);
DELETE FROM quotes WHERE quote_id IN (
    'f2a30701-0101-4101-8101-000000000801',
    'f2a30701-0101-4101-8101-000000000902',
    'f2a30701-0101-4101-8101-000000000903'
);

DELETE FROM bank_rules WHERE bank_rule_id IN (
    'f2a30401-0606-4606-8606-000000000001',
    'f2a30401-0606-4606-8606-000000000002',
    'f2a30401-0606-4606-8606-000000000003',
    'f2a30401-0606-4606-8606-000000000004',
    'f2a30401-0606-4606-8606-000000000005',
    'f2a30401-0606-4606-8606-000000000006'
);

DELETE FROM bank_transaction_line_items WHERE line_item_id IN (
    'f2a30302-0505-4505-8505-000000000001',
    'f2a30302-0505-4505-8505-000000000002',
    'f2a30302-0505-4505-8505-000000000003',
    'f2a30302-0505-4505-8505-000000000004',
    'f2a30302-0505-4505-8505-000000000005',
    'f2a30302-0505-4505-8505-000000000006',
    'f2a30302-0505-4505-8505-000000000007',
    'f2a30302-0505-4505-8505-000000000008',
    'f2a30302-0505-4505-8505-000000000009',
    'f2a30302-0505-4505-8505-000000000010',
    'f2a30302-0505-4505-8505-000000000011',
    'f2a30302-0505-4505-8505-000000000012',
    'f2a30302-0505-4505-8505-000000000013',
    'f2a30302-0505-4505-8505-000000000014',
    'f2a30302-0505-4505-8505-000000000015',
    'f2a30302-0505-4505-8505-000000000016',
    'f2a30302-0505-4505-8505-000000000017',
    'f2a30302-0505-4505-8505-000000000018'
);
DELETE FROM bank_transactions WHERE bank_transaction_id IN (
    'f2a30301-0404-4404-8404-000000000001',
    'f2a30301-0404-4404-8404-000000000002',
    'f2a30301-0404-4404-8404-000000000003',
    'f2a30301-0404-4404-8404-000000000004',
    'f2a30301-0404-4404-8404-000000000005',
    'f2a30301-0404-4404-8404-000000000006',
    'f2a30301-0404-4404-8404-000000000007',
    'f2a30301-0404-4404-8404-000000000008',
    'f2a30301-0404-4404-8404-000000000009',
    'f2a30301-0404-4404-8404-000000000010',
    'f2a30301-0404-4404-8404-000000000011',
    'f2a30301-0404-4404-8404-000000000012',
    'f2a30301-0404-4404-8404-000000000013',
    'f2a30301-0404-4404-8404-000000000014',
    'f2a30301-0404-4404-8404-000000000015',
    'f2a30301-0404-4404-8404-000000000016',
    'f2a30301-0404-4404-8404-000000000017',
    'f2a30301-0404-4404-8404-000000000018'
);

DELETE FROM payments WHERE payment_id IN (
    'f2a30201-0303-4303-8303-000000000001',
    'f2a30201-0303-4303-8303-000000000002',
    'f2a30201-0303-4303-8303-000000000003',
    'f2a30201-0303-4303-8303-000000000004',
    'f2a30201-0303-4303-8303-000000000005',
    'f2a30201-0303-4303-8303-000000000006'
);

DELETE FROM invoice_line_items WHERE line_item_id IN (
    'f2a30102-0202-4202-8202-000000000001',
    'f2a30102-0202-4202-8202-000000000002',
    'f2a30102-0202-4202-8202-000000000003',
    'f2a30102-0202-4202-8202-000000000004',
    'f2a30102-0202-4202-8202-000000000005',
    'f2a30102-0202-4202-8202-000000000006',
    'f2a30102-0202-4202-8202-000000000007',
    'f2a30102-0202-4202-8202-000000000008',
    'f2a30102-0202-4202-8202-000000000009',
    'f2a30102-0202-4202-8202-000000000010',
    'f2a30102-0202-4202-8202-000000000011',
    'f2a30102-0202-4202-8202-000000000012',
    'f2a30102-0202-4202-8202-000000000013',
    'f2a30102-0202-4202-8202-000000000014',
    'f2a30102-0202-4202-8202-000000000015',
    'f2a30102-0202-4202-8202-000000000016',
    'f2a30102-0202-4202-8202-000000000017',
    'f2a30102-0202-4202-8202-000000000018',
    'f2a30102-0202-4202-8202-000000000019',
    'f2a30102-0202-4202-8202-000000000020',
    'f2a30102-0202-4202-8202-000000000021'
);
DELETE FROM invoices WHERE invoice_id IN (
    'f2a30101-0101-4101-8101-000000000001',
    'f2a30101-0101-4101-8101-000000000002',
    'f2a30101-0101-4101-8101-000000000003',
    'f2a30101-0101-4101-8101-000000000004',
    'f2a30101-0101-4101-8101-000000000005',
    'f2a30101-0101-4101-8101-000000000006',
    'f2a30101-0101-4101-8101-000000000007',
    'f2a30101-0101-4101-8101-000000000008',
    'f2a30101-0101-4101-8101-000000000009',
    'f2a30101-0101-4101-8101-000000000010',
    'f2a30101-0101-4101-8101-000000000011',
    'f2a30101-0101-4101-8101-000000000012',
    'f2a30101-0101-4101-8101-000000000013',
    'f2a30101-0101-4101-8101-000000000014',
    'f2a30101-0101-4101-8101-000000000015',
    'f2a30101-0101-4101-8101-000000000016',
    'f2a30101-0101-4101-8101-000000000017',
    'f2a30101-0101-4101-8101-000000000018',
    'f2a30101-0101-4101-8101-000000000019',
    'f2a30101-0101-4101-8101-000000000020',
    'f2a30101-0101-4101-8101-000000000021'
);

DELETE FROM contacts WHERE contact_id IN (
    'f2a30002-0002-4000-8000-000000000201',
    'f2a30002-0002-4000-8000-000000000202',
    'f2a30002-0002-4000-8000-000000000203',
    'f2a30002-0002-4000-8000-000000000204',
    'f2a30002-0002-4000-8000-000000000205',
    'f2a30002-0002-4000-8000-000000000206'
);

DELETE FROM accounts
WHERE account_id = 'f2a30001-0001-4000-8000-000000000091'
  AND organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2';

UPDATE accounts
SET bank_account_number = NULL, bank_account_type = NULL
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '090';

UPDATE organisations
SET
    description = NULL,
    profile = '{}'::jsonb,
    financial_year_end_day = 31,
    financial_year_end_month = 12,
    tax_number = NULL
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2';
-- +goose StatementEnd
