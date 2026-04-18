-- +goose Up
-- +goose StatementBegin
-- Draft ACCREC for Fixture Labs (depends on 00001_fixture_second_tenant).
INSERT INTO invoices (
    invoice_id,
    organisation_id,
    type,
    contact_id,
    invoice_number,
    reference,
    currency_code,
    status,
    line_amount_types,
    date,
    due_date,
    sub_total,
    total_tax,
    total,
    total_discount,
    amount_due,
    amount_paid
) VALUES (
    '1eb8691d-450a-4b89-a07e-9c0e8d007cd0',
    '72590a0d-deb9-4fcc-a05a-e40fb47afc43',
    'ACCREC',
    '0945278a-c8d8-457b-8a94-8900d7b94e21',
    'FIX-1001',
    'fixture-draft',
    'USD',
    'DRAFT',
    'Exclusive',
    CURRENT_DATE,
    CURRENT_DATE + INTERVAL '30 days',
    100.0000,
    8.2500,
    108.2500,
    0,
    108.2500,
    0
);

INSERT INTO invoice_line_items (
    line_item_id,
    invoice_id,
    sort_order,
    description,
    quantity,
    unit_amount,
    item_code,
    account_code,
    item_id,
    account_id,
    tax_type,
    tax_amount,
    line_amount
) VALUES (
    'cb97aa93-5e0d-4ae2-80ee-02c2d81dd4f7',
    '1eb8691d-450a-4b89-a07e-9c0e8d007cd0',
    0,
    'Fixture line',
    1,
    100.0000,
    'FIXTURE-SKU',
    '200',
    '4861bfeb-eb16-4f2d-82da-55a3f4a36fbf',
    '3f5f7b84-bf88-45f6-b199-8591b1d6770d',
    'OUTPUT',
    8.2500,
    100.0000
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM invoices WHERE invoice_id = '1eb8691d-450a-4b89-a07e-9c0e8d007cd0';
-- +goose StatementEnd
