-- +goose Up
-- +goose StatementBegin
-- Report line mappings: which line of a report an account's figure goes on.
--
-- WHY THIS EXISTS.  internal/repository/form1120.go carried these rows as a
-- table in Go, each pinned to an account CODE -- {Code: "400", Line: "22"},
-- {Code: "820", Line: "L18"} and fifty more.  A code is the organisation's own
-- label for an account and Xero lets it be anything, so a table keyed on codes
-- is a table about one chart: the same file described a different organisation's
-- accounts wrongly, silently, and could not be corrected without a rebuild.
--
-- The rows are the same rows.  What changed is where they live: they are now
-- rows in this table, addressed by account_id, so they belong to the
-- organisation that holds the accounts rather than to the program.  An
-- accountant can correct a placement with an UPDATE -- which is what the old
-- table's own comment asked for ("an accountant is expected to edit it") but
-- could not deliver.
--
-- The TYPE-WIDE rules stay in Go, because they are not chart data: Xero's SALES
-- type is income from normal business activity whatever the chart calls it, BANK
-- is cash, INVENTORY is stock, DIRECTCOSTS is the cost of goods, TERMLIAB is
-- borrowings repayable beyond a year.  A type is a fact about the account that
-- Xero fixes; a code is not, and neither is the name.  Those five rules place
-- accounts by type and name no code at all.
--
-- The reasons are carried across verbatim from the Go table, so nothing that was
-- considered is lost.  Each row here is one account of the reference chart
-- (migrations/00023_xero_reference_chart_of_accounts.sql, written up in
-- docs/form1120-xero-chart.md) and the form line its figure belongs on.
--
-- Account ids are looked up by code, in this migration only.  That is the one
-- place a code is the right key: this migration names rows of one known chart,
-- the same way 00023 does when it inserts them.  The lookup is a JOIN, so if a
-- code below is not in the chart the row is skipped rather than invented -- and
-- the self-check at the end counts what landed.

CREATE TABLE report_line_mappings (
    organisation_id  UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    report           VARCHAR(40) NOT NULL,
    account_id       UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    line             VARCHAR(20) NOT NULL,
    reason           TEXT,
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organisation_id, report, account_id)
);

-- The report reads one organisation's whole set for one report, so this is the
-- index its query uses.
CREATE INDEX report_line_mappings_lookup
    ON report_line_mappings (organisation_id, report);

WITH mapping (code, line, reason) AS (VALUES
    ('200', '1a', 'Sales — income from normal business activity, so gross receipts. Pinned by code because Xero types Sales, Other Revenue and Interest Income alike as REVENUE.'),
    ('270', '5', 'Interest Income is interest on line 5. Pinned for the same reason: the REVENUE type cannot tell it apart from sales.'),
    ('260', '10', 'Other Revenue is income outside normal business activity — other income on line 10.'),
    ('400', '22', 'Advertising.'),
    ('416', '20', 'Depreciation.'),
    ('437', '18', 'Interest Expense.'),
    ('469', '16', 'Rent.'),
    ('473', '14', 'Repairs and Maintenance.'),
    ('477', '13', 'Wages and Salaries.'),
    ('408', '26', 'Cleaning. Page 1 has no line for it, so Other deductions is where the form puts it.'),
    ('445', '26', 'Light, Power, Heating. Utilities are not Rents or Repairs and maintenance, and Page 1 names no line for them.'),
    ('453', '26', 'Office Expenses. Page 1 has no line for it.'),
    ('461', '26', 'Printing & Stationery. Page 1 has no line for it — it is not Advertising.'),
    ('489', '26', 'Telephone & Internet. Page 1 has no line for it.'),
    ('493', '26', 'Travel - National. Page 1 has no line for travel; it is not Rents or Advertising.'),
    ('420', '26', 'Entertainment — but note the figure: the account carries the full book amount, and meals and entertainment are deductible only up to the limit the form allows. It is on line 26 because Page 1 has no line that names it; the disallowed part is an adjustment on Schedule M-1 line 5, not a different line here.'),
    ('449', '26', 'Motor Vehicle Expenses. Page 1 has no line for them, and they are not Repairs and maintenance; whether the deduction is the standard mileage rate or actual costs is the accountant''s election, so the book figure is what is carried.'),
    ('505', 'M2', 'Income Tax Expense is not a deduction on the return at all, so it is the one EXPENSE account that is not carried on line 26: Schedule M-1 line 2 adds the federal income tax back to book income, which is where this figure belongs.'),
    ('404', '26', 'Bank Fees. Page 1 names no line for them; they are in Other deductions on their merits, not because they are in doubt as a deduction.'),
    ('412', '26', 'Consulting & Accounting. Professional fees Page 1 does not name.'),
    ('425', '26', 'Freight & Courier. Page 1 names no line for carriage. Freight on goods bought for resale would be cost of goods sold on line 2, but this chart files the account as an expense.'),
    ('429', '26', 'General Expenses. Unnamed by definition, which is what Other deductions is for.'),
    ('433', '26', 'Insurance. Page 1 names no line for premiums.'),
    ('441', '26', 'Legal expenses. Page 1 names no line for them, and the form''s own instructions for Other deductions cite legal fees as the kind of thing that belongs there.'),
    ('478', '23', 'Superannuation — an employer''s contribution to an employee retirement plan, which is what line 23 Pension and profit sharing is for. A plan that is not a qualified plan belongs on line 24 Employee benefit programmes instead.'),
    ('485', '26', 'Subscriptions. Page 1 names no line for them.'),
    ('494', '26', 'Travel - International. Page 1 names no line for travel; it is not Rents or Advertising.'),
    ('497', '26', 'Bank Revaluations — the account Xero carries exchange differences on foreign-currency balances through. Page 1 names no line for them.'),
    ('499', '26', 'Realised Currency Gains. This chart files exchange differences under EXPENSE whatever their sign, so the account is carried in Other deductions; a year that leaves it a net gain is income, and income belongs on line 10.'),
    ('498', '26', 'Unrealised Currency Gains, for the same reason as Realised Currency Gains above.'),
    ('610', 'L2', 'Accounts Receivable.'),
    ('620', 'L6', 'Prepayments are a current asset that is not cash, receivables or inventory.'),
    ('710', 'L10a', 'Office Equipment.'),
    ('720', 'L10a', 'Computer Equipment.'),
    ('711', 'L10b', 'Accumulated depreciation on Office Equipment.'),
    ('721', 'L10b', 'Accumulated depreciation on Computer Equipment.'),
    ('800', 'L16', 'Accounts Payable.'),
    ('801', 'L18', 'Unpaid Expense Claims.'),
    ('820', 'L18', 'Sales Tax payable.'),
    ('825', 'L18', 'Employee Tax Payable.'),
    ('826', 'L18', 'Superannuation Payable.'),
    ('830', 'L18', 'Income Tax Payable.'),
    ('835', 'L18', 'Revenue Received in Advance.'),
    ('840', 'L18', 'Historical Adjustment — the account Xero keeps a conversion balance in, and typed a current liability in this chart, so it is an other current liability here. Left unmapped it would put the whole of the opening balance into the Schedule L check row.'),
    ('855', 'L18', 'Clearing Account.'),
    ('850', 'L18', 'Suspense. An account whose content is unknown by definition, typed a current liability in this chart, so it is an other current liability here.'),
    ('860', 'L18', 'Rounding, typed a current liability in this chart.'),
    ('877', 'L18', 'Tracking Transfers, typed a current liability in this chart.'),
    ('880', 'L18', 'Owner A Drawings, typed a current liability in this chart. It is the owner''s own account rather than a liability to a third party, so an accountant may prefer to re-classify it against equity.'),
    ('881', 'L18', 'Owner A Funds Introduced, typed a current liability in this chart — the same note as Owner A Drawings above.'),
    ('970', 'L21', 'Owner A Share Capital is the stock the shareholders hold.'),
    ('960', 'L24', 'Retained Earnings.')
)
INSERT INTO report_line_mappings (organisation_id, report, account_id, line, reason)
SELECT a.organisation_id, 'Form1120', a.account_id, m.line, m.reason
  FROM mapping m
  JOIN accounts a
    ON a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
   AND a.code = m.code;

-- Self-check: every row above must have found its account.  A code that matched
-- nothing would leave an account unplaced, and the report would print it under
-- "Accounts with no 1120 line" -- a workpaper that has quietly stopped placing
-- one account.  This says so at migration time instead.
DO $$
DECLARE want int; got int;
BEGIN
  want := 52;
  SELECT count(*) INTO got FROM report_line_mappings
   WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND report = 'Form1120';
  IF got <> want THEN
    RAISE EXCEPTION 'expecting % mappings for the reference chart, wrote %', want, got;
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS report_line_mappings;
-- +goose StatementEnd
