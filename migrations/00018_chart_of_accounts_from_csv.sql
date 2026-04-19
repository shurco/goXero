-- +goose Up
-- +goose StatementBegin
-- Full US-style chart of accounts for the demo org.
-- Source CSV (same columns): migrations/data/ChartOfAccounts.csv
-- Replaces the minimal seed in 00009: renames legacy codes to Z* staging rows, inserts/upserts
-- all CSV rows, repoints FKs from Z* accounts to the new rows, then deletes Z* rows.
-- Demo org: 6823b27b-c48f-4099-bb27-4202a4f496a2

UPDATE tax_rates
SET display_tax_rate = 9.25, effective_rate = 9.25, updated_at = now()
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND name IN ('Tax on Sales', 'Tax on Purchases');

UPDATE accounts SET code = 'Z090' WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '090';
UPDATE accounts SET code = 'Z200' WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '200';
UPDATE accounts SET code = 'Z260' WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '260';
UPDATE accounts SET code = 'Z310' WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '310';
UPDATE accounts SET code = 'Z400' WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '400';
UPDATE accounts SET code = 'Z404' WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '404';
UPDATE accounts SET code = 'Z610' WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '610';
UPDATE accounts SET code = 'Z800' WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '800';

INSERT INTO accounts (organisation_id, code, name, type, status, tax_type, class, system_account, description, enable_payments_to_account, show_in_expense_claims)
VALUES
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '090', 'Checking Account', 'BANK', 'ACTIVE', 'NONE', 'ASSET', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '091', 'Savings Account', 'BANK', 'ACTIVE', 'NONE', 'ASSET', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '120', 'Accounts Receivable', 'CURRENT', 'ACTIVE', 'NONE', 'ASSET', 'DEBTORS', NULLIF('Outstanding invoices the company has issued out to the client but has not yet received in cash at balance date.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '130', 'Prepayments', 'CURRENT', 'ACTIVE', 'NONE', 'ASSET', NULL, NULLIF('An expenditure that has been paid for in advance.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '140', 'Inventory', 'INVENTORY', 'ACTIVE', 'NONE', 'ASSET', NULL, NULLIF('Value of tracked inventory items for resale.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '150', 'Office Equipment', 'FIXED', 'ACTIVE', 'INPUT', 'ASSET', NULL, NULLIF('Office equipment that is owned and controlled by the business',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '151', 'Less Accumulated Depreciation on Office Equipment', 'FIXED', 'ACTIVE', 'NONE', 'ASSET', NULL, NULLIF('The total amount of office equipment cost that has been consumed by the entity (based on the useful life)',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '160', 'Computer Equipment', 'FIXED', 'ACTIVE', 'INPUT', 'ASSET', NULL, NULLIF('Computer equipment that is owned and controlled by the business',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '161', 'Less Accumulated Depreciation on Computer Equipment', 'FIXED', 'ACTIVE', 'NONE', 'ASSET', NULL, NULLIF('The total amount of computer equipment cost that has been consumed by the business (based on the useful life)',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '200', 'Accounts Payable', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', 'CREDITORS', NULLIF('Outstanding invoices the company has received from suppliers but has not yet paid at balance date',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '205', 'Accruals', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('Any services the business has received but have not yet been invoiced for e.g. Accountancy Fees',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '210', 'Unpaid Expense Claims', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('Expense claims typically made by employees/shareholder employees still outstanding.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '215', 'Wages Payable', 'PAYGLIABILITY', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('Xero automatically updates this account for payroll entries created using Payroll and will store the payroll amount to be paid to the employee for the pay run. This account enables you to maintain separate accounts for employee Wages Payable amounts and Accounts Payable amounts',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '216', 'Wages Payable – Payroll', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '220', 'Sales Tax', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('The balance in this account represents Sales Tax owing to or from your tax authority. At the end of the tax period, it is this account that should be used to code against either the ''refunds from'' or ''payments to'' your tax authority that will appear on the bank statement. Xero has been designed to use only one sales tax account to track sales taxes on income and expenses, so there is no need to add any new sales tax accounts to Xero.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '230', 'Employee Tax Payable', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('The amount of tax that has been deducted from wages or salaries paid to employes and is due to be paid',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '231', 'Federal Tax withholding', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '232', 'State Tax withholding', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '233', 'Employee Benefits payable', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '234', 'Employee Deductions payable', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '235', 'PTO payable', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '240', 'Income Tax Payable', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('The amount of income tax that is due to be paid, also resident withholding tax paid on interest received.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '250', 'Suspense', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('An entry that allows an unknown transaction to be entered, so the accounts can still be worked on in balance and the entry can be dealt with later.',''), TRUE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '255', 'Historical Adjustment', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('For accountant adjustments',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '260', 'Rounding', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('An adjustment entry to allow for rounding',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '265', 'Tracking Transfers', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('Transfers between tracking categories',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '290', 'Loan', 'TERMLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('Money that has been borrowed from a creditor',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '300', 'Owners Contribution', 'EQUITY', 'ACTIVE', 'NONE', 'EQUITY', NULL, NULLIF('Funds contributed by the owner',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '310', 'Owners Draw', 'EQUITY', 'ACTIVE', 'NONE', 'EQUITY', NULL, NULLIF('Withdrawals by the owners',''), TRUE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '320', 'Retained Earnings', 'EQUITY', 'ACTIVE', 'NONE', 'EQUITY', NULL, NULLIF('Do not Use',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '330', 'Common Stock', 'EQUITY', 'ACTIVE', 'NONE', 'EQUITY', NULL, NULLIF('The value of shares purchased by the shareholders',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '400', 'Sales', 'SALES', 'ACTIVE', 'OUTPUT', 'REVENUE', NULL, NULLIF('Income from any normal business activity',''), TRUE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '460', 'Other Revenue', 'REVENUE', 'ACTIVE', 'OUTPUT', 'REVENUE', NULL, NULLIF('Any other income that does not relate to normal business activities and is not recurring',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '470', 'Interest Income', 'REVENUE', 'ACTIVE', 'NONE', 'REVENUE', NULL, NULLIF('Interest income',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '500', 'Cost of Goods Sold', 'DIRECTCOSTS', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('Costs of goods made by the business include material, labor, and other modification costs.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '600', 'Advertising', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred for advertising while trying to increase sales',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '604', 'Bank Service Charges', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('Fees charged by your bank for transactions regarding your bank account(s).',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '608', 'Janitorial Expenses', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred for cleaning business property.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '612', 'Consulting & Accounting', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses related to paying consultants',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '620', 'Entertainment', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('Expenses paid by company for the business but are not deductable for income tax purposes.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '624', 'Postage & Delivery', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred on postage & delivery costs.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '628', 'General Expenses', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('General expenses related to the running of the business.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '632', 'Insurance', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred for insuring the business'' assets',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '640', 'Legal Expenses', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred on any legal matters',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '644', 'Utilities', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('Expenses incurred for lighting, powering or heating the premises',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '648', 'Automobile Expenses', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred on the running of company automobiles.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '652', 'Office Expenses', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('General expenses related to the running of the business office.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '656', 'Printing & Stationery', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred by the entity as a result of printing and stationery',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '660', 'Rent', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('The payment to lease a building or area.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '664', 'Repairs and Maintenance', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred on a damaged or run down asset that will bring the asset back to its original condition.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '668', 'Wages and Salaries', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('Payment to employees in exchange for their resources',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '669', 'Wages & Salaries - California', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '672', 'Payroll Tax Expense', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('The amount of payroll tax that is due to be paid',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '676', 'Dues & Subscriptions', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('E.g. Magazines, professional bodies',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '680', 'Telephone & Internet', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenditure incurred from any business-related phone calls, phone lines, or internet connections',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '684', 'Travel', 'EXPENSE', 'ACTIVE', 'INPUT', 'EXPENSE', NULL, NULLIF('Expenses incurred from travel which has a business purpose',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '690', 'Bad Debts', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('Noncollectable accounts receivable which have been written off.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '700', 'Depreciation', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('The amount of the asset''s cost (based on the useful life) that was consumed during the period',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '710', 'Income Tax Expense', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('A percentage of total earnings paid to the government.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '720', 'Federal Tax expense', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '721', 'State Tax expense', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '722', 'Employee Benefits expense', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '723', 'PTO expense', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '800', 'Interest Expense', 'EXPENSE', 'ACTIVE', 'NONE', 'EXPENSE', NULL, NULLIF('Any interest expenses paid to your tax authority, business bank accounts or credit card accounts.',''), FALSE, TRUE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '810', 'Bank Revaluations', 'CURRENT', 'ACTIVE', 'NONE', 'ASSET', NULL, NULLIF('Bank account revaluations due for foreign exchange rate changes',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '815', 'Unrealized Currency Gains', 'CURRENT', 'ACTIVE', 'NONE', 'ASSET', NULL, NULLIF('Unrealized gains on outstanding items',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '820', 'Realized Currency Gains', 'REVENUE', 'ACTIVE', 'NONE', 'REVENUE', NULL, NULLIF('Gains or losses made due to currency exchange rates',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '835', 'Revenue Received in Advance', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('When customers have paid in advance of work/services.',''), FALSE, FALSE),
    ('6823b27b-c48f-4099-bb27-4202a4f496a2', '855', 'Clearing Account', 'CURRLIAB', 'ACTIVE', 'NONE', 'LIABILITY', NULL, NULLIF('',''), TRUE, FALSE)
ON CONFLICT (organisation_id, code) DO UPDATE SET
    name = EXCLUDED.name,
    type = EXCLUDED.type,
    status = EXCLUDED.status,
    tax_type = EXCLUDED.tax_type,
    class = EXCLUDED.class,
    system_account = EXCLUDED.system_account,
    description = EXCLUDED.description,
    enable_payments_to_account = EXCLUDED.enable_payments_to_account,
    show_in_expense_claims = EXCLUDED.show_in_expense_claims;

DO $remap$
DECLARE
    m RECORD;
BEGIN
    FOR m IN
        SELECT oa.account_id AS oid, na.account_id AS nid
        FROM accounts oa
        JOIN accounts na ON na.organisation_id = oa.organisation_id
            AND na.code = (CASE oa.code
                WHEN 'Z090' THEN '090'
                WHEN 'Z200' THEN '400'
                WHEN 'Z260' THEN '460'
                WHEN 'Z310' THEN '500'
                WHEN 'Z400' THEN '600'
                WHEN 'Z404' THEN '604'
                WHEN 'Z610' THEN '120'
                WHEN 'Z800' THEN '200'
            END)
        WHERE oa.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND oa.code LIKE 'Z%'
    LOOP
        UPDATE invoice_line_items SET account_id = m.nid WHERE account_id = m.oid;
        UPDATE payments SET account_id = m.nid WHERE account_id = m.oid;
        UPDATE bank_transactions SET bank_account_id = m.nid WHERE bank_account_id = m.oid;
        UPDATE bank_transaction_line_items SET account_id = m.nid WHERE account_id = m.oid;
        UPDATE bank_feed_accounts SET account_id = m.nid WHERE account_id = m.oid;
        UPDATE bank_transfers SET from_bank_account_id = m.nid WHERE from_bank_account_id = m.oid;
        UPDATE bank_transfers SET to_bank_account_id = m.nid WHERE to_bank_account_id = m.oid;
        UPDATE manual_journal_lines SET account_id = m.nid WHERE account_id = m.oid;
        UPDATE gl_journal_lines SET account_id = m.nid WHERE account_id = m.oid;
        UPDATE batch_payments SET account_id = m.nid WHERE account_id = m.oid;
        UPDATE bank_rules SET definition = replace(definition::text, m.oid::text, m.nid::text)::jsonb
        WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND definition::text LIKE '%' || m.oid || '%';
    END LOOP;
END
$remap$;

UPDATE items SET sales_account_code = '400'
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND sales_account_code = '200';
UPDATE items SET purchase_account_code = '500'
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND purchase_account_code = '310';

UPDATE invoice_line_items ili SET account_code = a.code
FROM accounts a
WHERE ili.account_id = a.account_id
  AND EXISTS (SELECT 1 FROM invoices i WHERE i.invoice_id = ili.invoice_id AND i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2');

UPDATE bank_transaction_line_items btl SET account_code = a.code
FROM accounts a
WHERE btl.account_id = a.account_id
  AND EXISTS (
      SELECT 1 FROM bank_transactions bt
      WHERE bt.bank_transaction_id = btl.bank_transaction_id
        AND bt.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
  );

UPDATE quote_line_items qli SET account_code = qc.newc
FROM (VALUES
    ('200', '400')
) AS qc(oldc, newc)
WHERE qli.account_code = qc.oldc
  AND EXISTS (SELECT 1 FROM quotes q WHERE q.quote_id = qli.quote_id AND q.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2');

UPDATE purchase_order_line_items pol SET account_code = pc.newc
FROM (VALUES
    ('310', '500')
) AS pc(oldc, newc)
WHERE pol.account_code = pc.oldc
  AND EXISTS (SELECT 1 FROM purchase_orders po WHERE po.purchase_order_id = pol.purchase_order_id AND po.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2');

UPDATE manual_journal_lines mjl SET account_code = a.code
FROM accounts a
WHERE mjl.account_id = a.account_id
  AND EXISTS (
      SELECT 1 FROM manual_journals mj
      WHERE mj.manual_journal_id = mjl.manual_journal_id
        AND mj.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
  );

DELETE FROM accounts
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code LIKE 'Z%';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Replacing the full chart cannot be safely reversed without breaking FKs; use a dev DB reset if needed.
SELECT 1;
-- +goose StatementEnd
