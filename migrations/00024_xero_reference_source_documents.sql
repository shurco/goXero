-- +goose Up
-- +goose StatementBegin
-- Xero reference dataset, part 2: the source documents.
--
-- 00023 imported the chart of accounts and the bank feed, and stood a made-up
-- opening balance under them.  This migration replaces that scaffolding with
-- the documents Xero actually holds: the sales invoices, the bills, the credit
-- notes, the expense claims and the payments, plus the 34 coded bank
-- transactions the first capture missed.  After it runs the demo
-- organisation's ledger is the same posting run Xero did, so its Trial
-- Balance, P&L, Balance Sheet, Aged Receivables/Payables and Sales Tax
-- reports can print Xero's own figures.
--
-- Source of every value: migrations/data/xero/{contacts,invoices,
-- invoice-lines,bills,bill-lines,credit-notes,credit-note-lines,payments,
-- expense-claims,opening-balances,bank-transactions}.csv.  The posting rules
-- are the table in docs/xero-reference/reconciliation.md section 2 -- this
-- migration is that posting run in SQL, not a fresh derivation.  No figure
-- here is estimated, rounded or averaged.
--
-- 1. ONE OWNER FOR THE OPENING BALANCE.
--    00023 derived an opening balance of 8,654.01 from the closing balance
--    (7,430.22) minus the movement of the 48 transactions then captured
--    (-1,223.79).  That was right for the data it had, but it is not Xero's
--    figure and it is not a document.  Xero's own opening balance is
--    journal 388 "Conversion Balance" of 21 Jun 2026: Dr 090 4,130.98 /
--    Cr 840 4,130.98 (migrations/data/xero/opening-balances.csv).  This
--    migration deletes 00023's journal and posts that one instead.
--    00023's journal is identified by its fixed id,
--    uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/opening-balance/journal'),
--    which is the only journal in this organisation that is a MANUALJOURNAL
--    with a NULL source_id.  The replacement keeps the same shape under a key
--    that names the Xero journal it comes from,
--    'goxero/xero-ref/opening-balance/journal/388', so no other id reuses it.
--    The 8,654.01 is gone: nothing is added to it and no second journal owns it.
--
--    The six 630 Inventory rows in opening-balances.csv are NOT posted.  They
--    are journal 651's inventory-opening pairs, and each pair debits and
--    credits 630 by the same amount: they move nothing, and Xero prints no 630
--    line on its Trial Balance (docs/xero-reference/trial-balance.txt has 25
--    accounts and 630 is not one of them).  Posting them inside FY2026 would
--    also make goXero's Trial Balance render a "630 Inventory" row Xero does
--    not print -- internal/handlers/report_render.go renders every account that
--    has any figure, and a zero-net pair still leaves a non-zero debit and
--    credit.  Skipping them is also what the posting table in
--    docs/xero-reference/reconciliation.md does.
--
-- 2. THE LEDGER MUST BALANCE, AND LAND ON XERO'S TRIAL BALANCE.
--    Two separate checks, both run at the end of this migration:
--      (i)  the report -- every one of the 25 accounts in
--           docs/xero-reference/trial-balance.txt carries Xero's figure and
--           the total is 42,595.46 on both sides;
--      (ii) the ledger -- every gl_journals row's lines sum to zero and
--           SUM(net_amount) over all debits equals -SUM over all credits.
--    The raw all-time ledger total is NOT 42,595.46 and is not forced to it:
--    the trial balance nets accounts that the raw ledger does not.  See
--    docs/xero-import-00024.md for both totals and their decomposition.
--
-- 3. NO INVENTED FIGURES.  An empty CSV cell becomes NULL, never 0.  The one
--    number that is not a straight copy is the tax split inside a bank
--    transaction's journal: Xero's bank grid prints a single gross amount, so
--    the coded account gets round(|amount| / (1 + rate), 2) and 820 gets the
--    remainder.  That rule is reconciliation.md section 2, and it is verified
--    rather than asserted -- it is the only split under which account 820 lands
--    on Xero's own 422.59 and account 090 on 7,430.22.
--
-- 4. THE 710 DISCREPANCY.  00023 posted the 21 Aug 2026 ABC Furniture payment
--    of 1,000.00 to account 710 Office Equipment.  bill-lines.csv shows what
--    that payment settled: one ABC Furniture bill whose single line
--    ("Coffee table for reception") is 923.79 including tax.  The payment goes
--    where Xero's ledger puts it -- to 800 Accounts Payable, against the bill.
--    The 1,000.00 leaves account 710 with it, so 710 prints Xero's 923.79.
--    720 Computer Equipment needs two lines (PC Complete: "Laptop (Oliver)"
--    1,804.50 and "Laptop (Tracy)" 1,969.99) and no depreciation account moves
--    in FY2026, which is what fixed-assets.csv shows for the year too.
--
-- 5. RE-RUNNABLE.  Every id is uuid_generate_v5() of a fixed key, so `down`
--    restores 00023's state with the same ids and a following `up` reproduces
--    this one.  The single exception is gl_journals.journal_number, which comes
--    from a sequence and therefore advances on every re-run.
--
-- 6. SCOPED.  Every statement is scoped to organisation
--    6823b27b-c48f-4099-bb27-4202a4f496a2.  No row of any other organisation is
--    read or written, and the deletes below name fixed v5 ids rather than a
--    date range or a source_type, so a row another session added to this
--    organisation in the meantime is left alone.
--
-- 7. SOURCE DOCUMENTS AS ROWS.  The reports the task asks for derive from the
--    ledger (gl_journals + gl_journal_lines + accounts), from invoices +
--    contacts (Aged Receivables/Payables, Executive Summary) and from
--    invoice_line_items + invoices (Sales Tax).  All three lineages are
--    created here, and the document rows and the ledger are made to agree.
--
-- 8. THE ORGANISATION'S OWN SETTINGS.  Where the organisations table has a
--    column and Xero publishes a value, the value is written.  Where it has no
--    column the setting is listed in docs/xero-import-00024.md rather than
--    given a column of its own.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- 0. The captured rows, as staging tables.
--    Columns are named after the CSV headers they come from; the conversion
--    from a cell to a column is written out so it can be checked line by line.
-- ---------------------------------------------------------------------------

CREATE TEMP TABLE xr_contact (
    name        text,
    is_customer boolean,
    is_supplier boolean,
    email       text
) ON COMMIT DROP;

-- contacts.csv.  `type` is Xero's own flag column: "customer", "supplier",
-- "customer;supplier" or empty.  The schema has no nullable type column, only
-- the two booleans, so an empty type means neither flag -- it is not treated as
-- a customer, and it is not invented into one.
INSERT INTO xr_contact (name, is_customer, is_supplier, email) VALUES
    ('24 Locks', FALSE, FALSE, NULL),
    ('7-Eleven', FALSE, FALSE, NULL),
    ('Abby & Wells', FALSE, FALSE, NULL),
    ('ABC Furniture', FALSE, TRUE, 'info@abfl.com'),
    ('Adam Michkevich', FALSE, FALSE, 'pzkjjjibecbhyotxma@onldm.net'),
    ('Bank West', TRUE, FALSE, NULL),
    ('Basket Case', TRUE, FALSE, NULL),
    ('Bayside Club', TRUE, TRUE, 'secretarybob@bsclub.co'),
    ('Bayside Wholesale', FALSE, TRUE, NULL),
    ('Berry Brew', FALSE, FALSE, NULL),
    ('Boom FM', TRUE, FALSE, NULL),
    ('Brunswick Petals', FALSE, FALSE, NULL),
    ('Capital Cab Co', FALSE, TRUE, NULL),
    ('Carlton Functions', FALSE, TRUE, NULL),
    ('Central Copiers', FALSE, TRUE, NULL),
    ('City Agency', TRUE, FALSE, NULL),
    ('City Limousines', TRUE, FALSE, NULL),
    ('Coco Cafe', FALSE, FALSE, NULL),
    ('DIISR - Small Business Services', TRUE, FALSE, NULL),
    ('Dimples Warehouse', FALSE, FALSE, NULL),
    ('Eastside Club', FALSE, FALSE, NULL),
    ('Epicenter Cafe', FALSE, FALSE, NULL),
    ('Espresso 31', FALSE, FALSE, NULL),
    ('Fulton Airport Parking', FALSE, FALSE, NULL),
    ('Gable Print', FALSE, FALSE, NULL),
    ('Gateway Motors', FALSE, TRUE, NULL),
    ('Hamilton Smith Ltd', TRUE, FALSE, 'info@hsg.co'),
    ('Hoyt Productions', FALSE, TRUE, NULL),
    ('Luna Cafe', FALSE, FALSE, NULL),
    ('Marine Systems', TRUE, FALSE, NULL),
    ('MCO Cleaning Services', FALSE, TRUE, NULL),
    ('Melrose Parking', FALSE, FALSE, NULL),
    ('Net Connect', FALSE, TRUE, NULL),
    ('Office Supplies Company', FALSE, FALSE, NULL),
    ('Orlena Greenville', FALSE, FALSE, NULL),
    ('Passing Places Parking', FALSE, FALSE, NULL),
    ('PC Complete', FALSE, TRUE, NULL),
    ('Petrie McLoud Watson & Associates', TRUE, FALSE, NULL),
    ('Port & Philip Freight', TRUE, FALSE, NULL),
    ('PowerDirect', FALSE, TRUE, NULL),
    ('Rex Media Group', TRUE, FALSE, 'info@rexmedia.co'),
    ('Ridgeway Bank', FALSE, FALSE, NULL),
    ('Ridgeway University', TRUE, FALSE, NULL),
    ('RITE Agency', FALSE, FALSE, NULL),
    ('SMART Agency', FALSE, TRUE, NULL),
    ('Swanston Security', FALSE, TRUE, NULL),
    ('Telus', FALSE, FALSE, NULL),
    ('Truxton Property Management', FALSE, TRUE, NULL),
    ('Woolworths Market', FALSE, FALSE, NULL),
    ('Xero', FALSE, TRUE, NULL),
    ('Young Bros Transport', TRUE, TRUE, 'rog@ybt.co');

CREATE TEMP TABLE xr_doc (
    doc_key     text,
    kind        text,
    doc_number  text,
    contact     text,
    ref         text,
    doc_date    date,
    due_date    date,
    status      text,
    currency    text,
    sub_total   numeric,
    total_tax   numeric,
    total       numeric,
    amount_paid numeric,
    amount_due  numeric
) ON COMMIT DROP;

-- invoices.csv (kind ACCREC) and bills.csv (kind ACCPAY).  Both files carry the
-- same columns, so both land here.  A bill number that Xero leaves blank stays
-- NULL -- it is not given a number.
INSERT INTO xr_doc (doc_key, kind, doc_number, contact, ref, doc_date, due_date, status, currency, sub_total, total_tax, total, amount_paid, amount_due) VALUES
    ('invoice/INV-0001', 'ACCREC', 'INV-0001', 'Hamilton Smith Ltd', 'Monthly Support', DATE '2026-07-11', DATE '2026-07-22', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0002', 'ACCREC', 'INV-0002', 'Young Bros Transport', 'Monthly Support', DATE '2026-07-11', DATE '2026-07-22', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0003', 'ACCREC', 'INV-0003', 'Port & Philip Freight', 'Monthly Support', DATE '2026-07-11', DATE '2026-07-22', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0004', 'ACCREC', 'INV-0004', 'Rex Media Group', 'Monthly Support', DATE '2026-07-11', DATE '2026-07-22', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0005', 'ACCREC', 'INV-0005', 'Hamilton Smith Ltd', 'Monthly Support', DATE '2026-07-12', DATE '2026-07-22', 'PAID', 'USD', 500.00, 41.25, 541.25, 0.00, 0.00),
    ('invoice/INV-0006', 'ACCREC', 'INV-0006', 'City Limousines', 'P/O 9711', DATE '2026-07-10', DATE '2026-07-20', 'AUTHORISED', 'USD', 230.95, 19.05, 250.00, 0.00, 250.00),
    ('invoice/INV-0007', 'ACCREC', 'INV-0007', 'City Agency', 'Workshop', DATE '2026-07-14', DATE '2026-07-25', 'PAID', 'USD', 547.80, 45.43, 593.23, 593.23, 0.00),
    ('invoice/INV-0008', 'ACCREC', 'INV-0008', 'Bank West', 'Training', DATE '2026-07-13', DATE '2026-07-24', 'PAID', 'USD', 1200.00, 99.00, 1299.00, 1299.00, 0.00),
    ('invoice/INV-0009', 'ACCREC', 'INV-0009', 'Ridgeway University', 'P/O CRM08-12', DATE '2026-07-20', DATE '2026-08-11', 'PAID', 'USD', 5715.94, 471.56, 6187.50, 6187.50, 0.00),
    ('invoice/INV-0010', 'ACCREC', 'INV-0010', 'Boom FM', 'Training', DATE '2026-07-19', DATE '2026-08-01', 'PAID', 'USD', 500.00, 41.25, 541.25, 0.00, 0.00),
    ('invoice/INV-0011', 'ACCREC', 'INV-0011', 'Petrie McLoud Watson & Associates', 'Portal Proj', DATE '2026-07-25', DATE '2026-08-11', 'PAID', 'USD', 1300.00, 107.25, 1407.25, 1407.25, 0.00),
    ('invoice/INV-0012', 'ACCREC', 'INV-0012', 'City Limousines', 'P/O 9711', DATE '2026-07-27', DATE '2026-08-06', 'AUTHORISED', 'USD', 200.00, 16.50, 216.50, 0.00, 216.50),
    ('invoice/INV-0013', 'ACCREC', 'INV-0013', 'Boom FM', 'Training', DATE '2026-07-30', DATE '2026-08-11', 'PAID', 'USD', 1000.00, 82.50, 1082.50, 1082.50, 0.00),
    ('invoice/INV-0016', 'ACCREC', 'INV-0016', 'DIISR - Small Business Services', 'Yr Ref W08-143', DATE '2026-08-01', DATE '2026-08-11', 'AUTHORISED', 'USD', 775.00, 63.94, 838.94, 568.31, 270.63),
    ('invoice/INV-0017', 'ACCREC', 'INV-0017', 'City Limousines', 'Book', DATE '2026-07-30', DATE '2026-08-09', 'AUTHORISED', 'USD', 19.95, 1.75, 21.70, 0.00, 21.70),
    ('invoice/INV-0018', 'ACCREC', 'INV-0018', 'Hamilton Smith Ltd', 'Monthly Support', DATE '2026-08-11', DATE '2026-08-21', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0019', 'ACCREC', 'INV-0019', 'Young Bros Transport', 'Monthly Support', DATE '2026-08-11', DATE '2026-08-21', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0020', 'ACCREC', 'INV-0020', 'Port & Philip Freight', 'Monthly Support', DATE '2026-08-11', DATE '2026-08-21', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0021', 'ACCREC', 'INV-0021', 'Rex Media Group', 'Monthly Support', DATE '2026-08-11', DATE '2026-08-21', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0022', 'ACCREC', 'INV-0022', 'DIISR - Small Business Services', 'Yr Ref W08-143', DATE '2026-08-11', DATE '2026-08-21', 'PAID', 'USD', 219.95, 18.25, 238.20, 216.50, 0.00),
    ('invoice/INV-0024', 'ACCREC', 'INV-0024', 'City Limousines', 'P/O 9711', DATE '2026-09-05', DATE '2026-09-20', 'AUTHORISED', 'USD', 650.00, 53.63, 703.63, 0.00, 703.63),
    ('invoice/INV-0025', 'ACCREC', 'INV-0025', 'Ridgeway University', 'P/O CRM08-12', DATE '2026-08-20', DATE '2026-09-10', 'AUTHORISED', 'USD', 5715.94, 471.56, 6187.50, 0.00, 6187.50),
    ('invoice/INV-0026', 'ACCREC', 'INV-0026', 'Basket Case', NULL, DATE '2026-09-10', DATE '2026-09-20', 'AUTHORISED', 'USD', 844.85, 69.70, 914.55, 0.00, 914.55),
    ('invoice/INV-0027', 'ACCREC', 'INV-0027', 'Marine Systems', 'Ref MK815', DATE '2026-09-10', DATE '2026-09-16', 'AUTHORISED', 'USD', 365.82, 30.18, 396.00, 0.00, 396.00),
    ('invoice/INV-0028', 'ACCREC', 'INV-0028', 'Bayside Club', 'GB1-White', DATE '2026-09-10', DATE '2026-09-29', 'AUTHORISED', 'USD', 216.93, 17.07, 234.00, 0.00, 234.00),
    ('invoice/INV-0029', 'ACCREC', 'INV-0029', 'Hamilton Smith Ltd', 'Monthly support', DATE '2026-09-09', DATE '2026-09-24', 'DRAFT', 'USD', 508.08, 41.92, 550.00, 0.00, 550.00),
    ('invoice/INV-0030', 'ACCREC', 'INV-0030', 'Rex Media Group', 'Monthly support', DATE '2026-09-09', DATE '2026-09-24', 'DRAFT', 'USD', 508.08, 41.92, 550.00, 0.00, 550.00),
    ('invoice/INV-0031', 'ACCREC', 'INV-0031', 'City Agency', 'Golf Balls', DATE '2026-08-26', DATE '2026-09-02', 'PAID', 'USD', 96.00, 8.40, 104.40, 104.40, 0.00),
    ('invoice/INV-0032', 'ACCREC', 'INV-0032', 'Bank West', 'Website', DATE '2026-08-26', DATE '2026-09-02', 'PAID', 'USD', 300.00, 24.75, 324.75, 324.75, 0.00),
    ('invoice/INV-0033', 'ACCREC', 'INV-0033', 'Boom FM', 'Training', DATE '2026-08-31', DATE '2026-09-07', 'PAID', 'USD', 500.00, 41.25, 541.25, 541.25, 0.00),
    ('invoice/INV-0034', 'ACCREC', 'INV-0034', 'Boom FM', 'Consulting', DATE '2026-09-03', DATE '2026-09-10', 'PAID', 'USD', 3600.00, 297.00, 3897.00, 3897.00, 0.00),
    ('invoice/INV-0036', 'ACCREC', 'INV-0036', 'Petrie McLoud Watson & Associates', 'Consulting', DATE '2026-09-03', DATE '2026-09-10', 'PAID', 'USD', 2070.00, 170.78, 2240.78, 2240.78, 0.00),
    ('bill/Xero/2026-07-08/AP', 'ACCPAY', 'AP', 'Xero', NULL, DATE '2026-07-08', DATE '2026-07-08', 'PAID', 'USD', 29.00, 2.39, 31.39, 31.39, 0.00),
    ('bill/Truxton Property Management/2026-07-11/RENT', 'ACCPAY', 'RENT', 'Truxton Property Management', NULL, DATE '2026-07-11', DATE '2026-07-11', 'PAID', 'USD', 1091.22, 90.03, 1181.25, 1181.25, 0.00),
    ('bill/PowerDirect/2026-07-11/Rpt', 'ACCPAY', 'Rpt', 'PowerDirect', NULL, DATE '2026-07-11', DATE '2026-07-21', 'PAID', 'USD', 110.00, 9.08, 119.08, 119.08, 0.00),
    ('bill/Net Connect/2026-07-12/Rpt', 'ACCPAY', 'Rpt', 'Net Connect', NULL, DATE '2026-07-12', DATE '2026-07-22', 'PAID', 'USD', 41.50, 3.42, 44.92, 44.92, 0.00),
    ('bill/Central Copiers/2026-07-19/945-OCon', 'ACCPAY', '945-OCon', 'Central Copiers', NULL, DATE '2026-07-19', DATE '2026-07-24', 'AUTHORISED', 'USD', 982.50, 81.06, 1063.56, 900.00, 163.56),
    ('bill/Net Connect/2026-07-20/9781', 'ACCPAY', '9781', 'Net Connect', NULL, DATE '2026-07-20', DATE '2026-07-31', 'PAID', 'USD', 1350.00, 113.88, 1463.88, 1463.88, 0.00),
    ('bill/PC Complete/2026-07-21/', 'ACCPAY', NULL, 'PC Complete', NULL, DATE '2026-07-21', DATE '2026-07-21', 'PAID', 'USD', 1804.50, 148.87, 1953.37, 1682.74, 0.00),
    ('bill/MCO Cleaning Services/2026-07-21/5679', 'ACCPAY', '5679', 'MCO Cleaning Services', NULL, DATE '2026-07-21', DATE '2026-07-27', 'PAID', 'USD', 110.00, 9.08, 119.08, 119.08, 0.00),
    ('bill/Swanston Security/2026-07-21/AP', 'ACCPAY', 'AP', 'Swanston Security', NULL, DATE '2026-07-21', DATE '2026-07-28', 'PAID', 'USD', 55.00, 4.54, 59.54, 34.10, 0.00),
    ('bill/SMART Agency/2026-07-21/SM0195', 'ACCPAY', 'SM0195', 'SMART Agency', NULL, DATE '2026-07-21', DATE '2026-08-01', 'AUTHORISED', 'USD', 1847.58, 152.42, 2000.00, 0.00, 2000.00),
    ('bill/PC Complete/2026-08-01/OG laptop', 'ACCPAY', 'OG laptop', 'PC Complete', NULL, DATE '2026-08-01', DATE '2026-08-01', 'PAID', 'USD', 250.00, 20.63, 270.63, 270.63, 0.00),
    ('bill/Xero/2026-08-08/AP', 'ACCPAY', 'AP', 'Xero', NULL, DATE '2026-08-08', DATE '2026-08-08', 'PAID', 'USD', 29.00, 2.39, 31.39, 31.39, 0.00),
    ('bill/MCO Cleaning Services/2026-08-08/M000435', 'ACCPAY', 'M000435', 'MCO Cleaning Services', NULL, DATE '2026-08-08', DATE '2026-08-15', 'PAID', 'USD', 200.00, 16.50, 216.50, 216.50, 0.00),
    ('bill/Hoyt Productions/2026-08-11/08-4123', 'ACCPAY', '08-4123', 'Hoyt Productions', NULL, DATE '2026-08-11', DATE '2026-08-21', 'PAID', 'USD', 5500.00, 453.75, 5953.75, 5953.75, 0.00),
    ('bill/Carlton Functions/2026-08-11/Dep', 'ACCPAY', 'Dep', 'Carlton Functions', NULL, DATE '2026-08-11', DATE '2026-08-21', 'PAID', 'USD', 1500.00, 0.00, 1500.00, 1500.00, 0.00),
    ('bill/Truxton Property Management/2026-08-11/RENT', 'ACCPAY', 'RENT', 'Truxton Property Management', NULL, DATE '2026-08-11', DATE '2026-08-11', 'PAID', 'USD', 1091.22, 90.03, 1181.25, 1181.25, 0.00),
    ('bill/PowerDirect/2026-08-11/Rpt', 'ACCPAY', 'Rpt', 'PowerDirect', NULL, DATE '2026-08-11', DATE '2026-08-21', 'PAID', 'USD', 125.50, 10.35, 135.85, 135.85, 0.00),
    ('bill/Net Connect/2026-08-12/Rpt', 'ACCPAY', 'Rpt', 'Net Connect', NULL, DATE '2026-08-12', DATE '2026-08-21', 'PAID', 'USD', 43.25, 3.57, 46.82, 46.82, 0.00),
    ('bill/MCO Cleaning Services/2026-08-15/M000442', 'ACCPAY', 'M000442', 'MCO Cleaning Services', NULL, DATE '2026-08-15', DATE '2026-08-22', 'PAID', 'USD', 200.00, 16.50, 216.50, 216.50, 0.00),
    ('bill/ABC Furniture/2026-08-21/710', 'ACCPAY', '710', 'ABC Furniture', NULL, DATE '2026-08-21', DATE '2026-08-21', 'PAID', 'USD', 923.79, 76.21, 1000.00, 1000.00, 0.00),
    ('bill/Swanston Security/2026-08-21/AP', 'ACCPAY', 'AP', 'Swanston Security', NULL, DATE '2026-08-21', DATE '2026-08-28', 'AUTHORISED', 'USD', 55.00, 4.54, 59.54, 0.00, 59.54),
    ('bill/MCO Cleaning Services/2026-08-22/M000456', 'ACCPAY', 'M000456', 'MCO Cleaning Services', NULL, DATE '2026-08-22', DATE '2026-08-29', 'PAID', 'USD', 200.00, 16.50, 216.50, 216.50, 0.00),
    ('bill/MCO Cleaning Services/2026-08-29/M000463', 'ACCPAY', 'M000463', 'MCO Cleaning Services', NULL, DATE '2026-08-29', DATE '2026-09-05', 'PAID', 'USD', 200.00, 16.50, 216.50, 216.50, 0.00),
    ('bill/Truxton Property Management/2026-08-31/RENT', 'ACCPAY', 'RENT', 'Truxton Property Management', NULL, DATE '2026-08-31', DATE '2026-09-10', 'PAID', 'USD', 1091.22, 90.03, 1181.25, 1181.25, 0.00),
    ('bill/SMART Agency/2026-08-31/SM0210', 'ACCPAY', 'SM0210', 'SMART Agency', NULL, DATE '2026-08-31', DATE '2026-09-10', 'AUTHORISED', 'USD', 2309.47, 190.53, 2500.00, 0.00, 2500.00),
    ('bill/Bayside Club/2026-09-05/', 'ACCPAY', NULL, 'Bayside Club', NULL, DATE '2026-09-05', DATE '2026-09-15', 'AUTHORISED', 'USD', 120.09, 9.91, 130.00, 0.00, 130.00),
    ('bill/PC Complete/2026-09-05/', 'ACCPAY', NULL, 'PC Complete', NULL, DATE '2026-09-05', DATE '2026-10-11', 'AUTHORISED', 'USD', 1969.99, 162.52, 2132.51, 0.00, 2132.51),
    ('bill/MCO Cleaning Services/2026-09-05/M000471', 'ACCPAY', 'M000471', 'MCO Cleaning Services', NULL, DATE '2026-09-05', DATE '2026-09-12', 'PAID', 'USD', 200.00, 16.50, 216.50, 216.50, 0.00),
    ('bill/Gateway Motors/2026-09-06/', 'ACCPAY', NULL, 'Gateway Motors', NULL, DATE '2026-09-06', DATE '2026-10-07', 'PAID', 'USD', 380.00, 31.35, 411.35, 411.35, 0.00),
    ('bill/Young Bros Transport/2026-09-07/', 'ACCPAY', NULL, 'Young Bros Transport', NULL, DATE '2026-09-07', DATE '2026-09-20', 'AUTHORISED', 'USD', 115.50, 9.53, 125.03, 0.00, 125.03),
    ('bill/Xero/2026-09-07/AP', 'ACCPAY', 'AP', 'Xero', NULL, DATE '2026-09-07', DATE '2026-09-07', 'AUTHORISED', 'USD', 29.00, 2.39, 31.39, 0.00, 31.39),
    ('bill/Bayside Wholesale/2026-09-08/GB1-White', 'ACCPAY', 'GB1-White', 'Bayside Wholesale', NULL, DATE '2026-09-08', DATE '2026-09-19', 'AUTHORISED', 'USD', 775.98, 64.02, 840.00, 0.00, 840.00),
    ('bill/PowerDirect/2026-09-10/Rpt', 'ACCPAY', 'Rpt', 'PowerDirect', NULL, DATE '2026-09-10', DATE '2026-09-20', 'AUTHORISED', 'USD', 100.32, 8.28, 108.60, 0.00, 108.60),
    ('bill/Capital Cab Co/2026-09-11/CS815', 'ACCPAY', 'CS815', 'Capital Cab Co', NULL, DATE '2026-09-11', DATE '2026-09-17', 'AUTHORISED', 'USD', 223.56, 18.44, 242.00, 0.00, 242.00),
    ('bill/Net Connect/2026-09-11/Rpt', 'ACCPAY', 'Rpt', 'Net Connect', NULL, DATE '2026-09-11', DATE '2026-09-21', 'AUTHORISED', 'USD', 50.00, 4.13, 54.13, 0.00, 54.13),
    ('bill/Swanston Security/2026-09-20/AP', 'ACCPAY', 'AP', 'Swanston Security', NULL, DATE '2026-09-20', DATE '2026-09-28', 'VOIDED', 'USD', 55.00, 4.54, 59.54, 0.00, 59.54),
    ('bill/Xero/2026-10-08/AP', 'ACCPAY', 'AP', 'Xero', NULL, DATE '2026-10-08', DATE '2026-10-23', 'VOIDED', 'USD', 29.00, 2.39, 31.39, 0.00, 31.39),
    ('bill/Truxton Property Management/2026-10-11/RENT', 'ACCPAY', 'RENT', 'Truxton Property Management', NULL, DATE '2026-10-11', DATE '2026-10-23', 'VOIDED', 'USD', 1091.22, 90.03, 1181.25, 0.00, 1181.25),
    ('bill/PowerDirect/2026-10-11/Rpt', 'ACCPAY', 'Rpt', 'PowerDirect', NULL, DATE '2026-10-11', DATE '2026-11-01', 'DELETED', 'USD', 105.00, 8.66, 113.66, 0.00, 113.66),
    ('bill/Net Connect/2026-10-12/Rpt', 'ACCPAY', 'Rpt', 'Net Connect', NULL, DATE '2026-10-12', DATE '2026-11-19', 'DELETED', 'USD', 39.00, 3.22, 42.22, 0.00, 42.22),
    ('bill/Swanston Security/2026-10-21/AP', 'ACCPAY', 'AP', 'Swanston Security', NULL, DATE '2026-10-21', DATE '2026-10-29', 'VOIDED', 'USD', 55.00, 4.54, 59.54, 0.00, 59.54),
    ('bill/Xero/2026-11-08/AP', 'ACCPAY', 'AP', 'Xero', NULL, DATE '2026-11-08', DATE '2026-11-20', 'VOIDED', 'USD', 29.00, 2.39, 31.39, 0.00, 31.39),
    ('bill/Truxton Property Management/2026-11-11/RENT', 'ACCPAY', 'RENT', 'Truxton Property Management', NULL, DATE '2026-11-11', DATE '2026-11-20', 'VOIDED', 'USD', 1091.22, 90.03, 1181.25, 0.00, 1181.25),
    ('bill/PowerDirect/2026-11-11/Rpt', 'ACCPAY', 'Rpt', 'PowerDirect', NULL, DATE '2026-11-11', DATE '2026-11-29', 'DELETED', 'USD', 105.00, 8.66, 113.66, 0.00, 113.66),
    ('bill/Net Connect/2026-11-12/Rpt', 'ACCPAY', 'Rpt', 'Net Connect', NULL, DATE '2026-11-12', DATE '2026-09-11', 'DELETED', 'USD', 39.00, 3.22, 42.22, 0.00, 42.22),
    ('bill/Swanston Security/2026-11-19/AP', 'ACCPAY', 'AP', 'Swanston Security', NULL, DATE '2026-11-19', DATE '2026-11-26', 'VOIDED', 'USD', 55.00, 4.54, 59.54, 0.00, 59.54);

CREATE TEMP TABLE xr_doc_line (
    doc_key      text,
    ord          int,
    description  text,
    quantity     numeric,
    unit_amount  numeric,
    account_code text,
    tax_type     text,
    tax_amount   numeric,
    line_amount  numeric
) ON COMMIT DROP;

-- invoice-lines.csv and bill-lines.csv.  `tax_type` is the rate name mapped to
-- the code 00023 created for it; a nil rate cell stays NULL.
INSERT INTO xr_doc_line (doc_key, ord, description, quantity, unit_amount, account_code, tax_type, tax_amount, line_amount) VALUES
    ('invoice/INV-0001', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0002', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0003', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0004', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0005', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0006', 1, 'Project management & implementation - branding workshop with your team', 1.00, 230.95, '200', 'OUTPUT', 19.05, 230.95),
    ('invoice/INV-0007', 1, 'Project management & implementation - branding workshop with your team ======================== - ''Buzz Words'' session with your Steering Group - Analysis of current marketing materials - Workshop on re-brand outcomes and stakeholder identification - Analysis and presentation of findings to your Steering Group & Board', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0007', 2, 'Copies of ''Fish out of Water'' text for your Branding Team', 4.00, 11.95, '200', 'OUTPUT2', 4.18, 47.80),
    ('invoice/INV-0008', 1, 'Half day training - Microsoft Office - for your Priority Mortgage Services Team (Session 3)', 1.00, 400.00, '200', 'OUTPUT', 33.00, 400.00),
    ('invoice/INV-0008', 2, 'Half day training - Microsoft Office - for your Lending Services Team (Session 2)', 1.00, 400.00, '200', 'OUTPUT', 33.00, 400.00),
    ('invoice/INV-0008', 3, 'Half day training - Microsoft Office - for your Customer Support Team (Session 1)', 1.00, 400.00, '200', 'OUTPUT', 33.00, 400.00),
    ('invoice/INV-0009', 1, 'Onsite project management for CRM Project 3 days/week', 1.00, 5715.94, '200', 'OUTPUT', 471.56, 5715.94),
    ('invoice/INV-0010', 1, 'Half day training - Microsoft Office', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0011', 1, 'Development work - develper onsite per day', 2.00, 650.00, '200', 'OUTPUT', 107.25, 1300.00),
    ('invoice/INV-0012', 1, 'Project management & implementation - branding workshop with your team - follow up session', 1.00, 200.00, '200', 'OUTPUT', 16.50, 200.00),
    ('invoice/INV-0013', 1, 'Half day training - Microsoft Office - reception staff (Session 1)', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0013', 2, 'Half day training - Microsoft Office - operations staff (Session 2)', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0016', 1, 'Project management & implementation - branding workshop with your team', 1.00, 250.00, '200', 'OUTPUT', 20.63, 250.00),
    ('invoice/INV-0016', 2, 'Project management & implementation - ''due diligence'' stocktake of your project scope/schedule/implementation plan/outcome measures (hourly rate as agreed)', 5.00, 105.00, '200', 'OUTPUT', 43.31, 525.00),
    ('invoice/INV-0017', 1, '''Fish out of Water: Finding Your Brand''', 1.00, 19.95, '200', 'OUTPUT2', 1.75, 19.95),
    ('invoice/INV-0018', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0019', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0020', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0021', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0022', 1, '''Fish out of Water: Finding Your Brand''', 1.00, 19.95, '200', 'OUTPUT2', 1.75, 19.95),
    ('invoice/INV-0022', 2, 'Project management & implementation - branding workshop with your team - follow up', 1.00, 200.00, '200', 'OUTPUT', 16.50, 200.00),
    ('invoice/INV-0024', 1, 'Development work - develper onsite per day', 1.00, 650.00, '200', 'OUTPUT', 53.63, 650.00),
    ('invoice/INV-0025', 1, 'Onsite project management for CRM Project 3 days/week', 1.00, 5715.94, '200', 'OUTPUT', 471.56, 5715.94),
    ('invoice/INV-0026', 1, 'Development work - per hour rate', 5.00, 86.84, '200', 'OUTPUT', 35.82, 434.18),
    ('invoice/INV-0026', 2, 'Project team meeting to discuss dev changes required to your online gift basket ordering system', 1.00, 410.67, '200', 'OUTPUT', 33.88, 410.67),
    ('invoice/INV-0027', 1, 'Marketing guides', 4.00, 91.46, '200', 'OUTPUT', 30.18, 365.82),
    ('invoice/INV-0028', 1, 'Golf balls - white single', 40.00, 5.17, '200', 'OUTPUT', 17.07, 206.93),
    ('invoice/INV-0028', 2, 'Delivery charge', 1.00, 10.00, '425', 'NONE', 0.00, 10.00),
    ('invoice/INV-0029', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 508.08, '200', 'OUTPUT', 41.92, 508.08),
    ('invoice/INV-0030', 1, 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 1.00, 508.08, '200', 'OUTPUT', 41.92, 508.08),
    ('invoice/INV-0031', 1, 'Golf Balls', 3.00, 32.00, '200', 'OUTPUT2', 8.40, 96.00),
    ('invoice/INV-0032', 1, 'Website creation', 1.00, 300.00, '200', 'OUTPUT', 24.75, 300.00),
    ('invoice/INV-0033', 1, 'Half day training - Microsoft Office - Marketing team', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('invoice/INV-0034', 1, 'Development work - software integration', 40.00, 90.00, '200', 'OUTPUT', 297.00, 3600.00),
    ('invoice/INV-0036', 1, 'Development work - software integration', 23.00, 90.00, '200', 'OUTPUT', 170.78, 2070.00),
    ('bill/Xero/2026-07-08/AP', 1, 'Monthly subscription', 1.00, 29.00, '412', 'INPUT', 2.39, 29.00),
    ('bill/Truxton Property Management/2026-07-11/RENT', 1, 'Monthy rent in advance', 1.00, 1091.22, '469', 'INPUT', 90.03, 1091.22),
    ('bill/PowerDirect/2026-07-11/Rpt', 1, 'Monthly power supply', 1.00, 110.00, '445', 'INPUT', 9.08, 110.00),
    ('bill/Net Connect/2026-07-12/Rpt', 1, 'Cable internet', 1.00, 41.50, '489', 'INPUT', 3.42, 41.50),
    ('bill/Central Copiers/2026-07-19/945-OCon', 1, 'Photocopier repair & drum replacement', 1.00, 982.50, '473', 'INPUT', 81.06, 982.50),
    ('bill/Net Connect/2026-07-20/9781', 1, 'Network diagnostics software', 1.00, 500.00, '453', 'OUTPUT2', 43.75, 500.00),
    ('bill/Net Connect/2026-07-20/9781', 2, 'Replacement hub & network switches', 1.00, 850.00, '473', 'INPUT', 70.13, 850.00),
    ('bill/PC Complete/2026-07-21/', 1, 'Laptop (Oliver)', 1.00, 1804.50, '720', 'INPUT', 148.87, 1804.50),
    ('bill/MCO Cleaning Services/2026-07-21/5679', 1, 'Office Cleaning for month', 1.00, 110.00, '408', 'INPUT', 9.08, 110.00),
    ('bill/Swanston Security/2026-07-21/AP', 1, 'Our share building doorman/security', 1.00, 55.00, '453', 'INPUT', 4.54, 55.00),
    ('bill/SMART Agency/2026-07-21/SM0195', 1, 'Design concepts for Oaktown Business Leader ad series', 1.00, 1847.58, '400', 'INPUT', 152.42, 1847.58),
    ('bill/PC Complete/2026-08-01/OG laptop', 1, 'DVD writer for laptop', 1.00, 250.00, '453', 'INPUT', 20.63, 250.00),
    ('bill/Xero/2026-08-08/AP', 1, 'Monthly subscription', 1.00, 29.00, '412', 'INPUT', 2.39, 29.00),
    ('bill/MCO Cleaning Services/2026-08-08/M000435', 1, 'Office Cleaning - daily rate', 5.00, 40.00, '408', 'INPUT', 16.50, 200.00),
    ('bill/Hoyt Productions/2026-08-11/08-4123', 1, '20-second still frame ad shown in 5 city cinemas 5 times each', 1.00, 5500.00, '400', 'INPUT', 453.75, 5500.00),
    ('bill/Carlton Functions/2026-08-11/Dep', 1, 'Deposit on venue hire for client function', 1.00, 1500.00, '420', 'NONE', 0.00, 1500.00),
    ('bill/Truxton Property Management/2026-08-11/RENT', 1, 'Monthy rent in advance', 1.00, 1091.22, '469', 'INPUT', 90.03, 1091.22),
    ('bill/PowerDirect/2026-08-11/Rpt', 1, 'Monthly power supply', 1.00, 125.50, '445', 'INPUT', 10.35, 125.50),
    ('bill/Net Connect/2026-08-12/Rpt', 1, 'Cable internet', 1.00, 43.25, '489', 'INPUT', 3.57, 43.25),
    ('bill/MCO Cleaning Services/2026-08-15/M000442', 1, 'Office Cleaning - daily rate', 5.00, 40.00, '408', 'INPUT', 16.50, 200.00),
    ('bill/ABC Furniture/2026-08-21/710', 1, 'Coffee table for reception', 1.00, 923.79, '710', 'INPUT', 76.21, 923.79),
    ('bill/Swanston Security/2026-08-21/AP', 1, 'Our share building doorman/security', 1.00, 55.00, '453', 'INPUT', 4.54, 55.00),
    ('bill/MCO Cleaning Services/2026-08-22/M000456', 1, 'Office Cleaning - daily rate', 5.00, 40.00, '408', 'INPUT', 16.50, 200.00),
    ('bill/MCO Cleaning Services/2026-08-29/M000463', 1, 'Office Cleaning - daily rate', 5.00, 40.00, '408', 'INPUT', 16.50, 200.00),
    ('bill/Truxton Property Management/2026-08-31/RENT', 1, 'Monthy rent in advance', 1.00, 1091.22, '469', 'INPUT', 90.03, 1091.22),
    ('bill/SMART Agency/2026-08-31/SM0210', 1, 'Prototype media banner & print mockups for Oaktown Business Leader ad series', 1.00, 2309.47, '400', 'INPUT', 190.53, 2309.47),
    ('bill/Bayside Club/2026-09-05/', 1, 'Room hire', 1.00, 120.09, '429', 'INPUT', 9.91, 120.09),
    ('bill/PC Complete/2026-09-05/', 1, 'Laptop (Tracy)', 1.00, 1969.99, '720', 'INPUT', 162.52, 1969.99),
    ('bill/MCO Cleaning Services/2026-09-05/M000471', 1, 'Office Cleaning - daily rate', 5.00, 40.00, '408', 'INPUT', 16.50, 200.00),
    ('bill/Gateway Motors/2026-09-06/', 1, 'Annual service company car', 1.00, 380.00, '449', 'INPUT', 31.35, 380.00),
    ('bill/Young Bros Transport/2026-09-07/', 1, 'Delivery of new reception desk', 1.00, 115.50, '425', 'INPUT', 9.53, 115.50),
    ('bill/Xero/2026-09-07/AP', 1, 'Monthly subscription', 1.00, 29.00, '412', 'INPUT', 2.39, 29.00),
    ('bill/Bayside Wholesale/2026-09-08/GB1-White', 1, 'Golf balls - white single', 200.00, 3.88, '300', 'INPUT', 64.02, 775.98),
    ('bill/PowerDirect/2026-09-10/Rpt', 1, 'Monthly power supply', 1.00, 100.32, '445', 'INPUT', 8.28, 100.32),
    ('bill/Capital Cab Co/2026-09-11/CS815', 1, 'Taxi services', 1.00, 223.56, '493', 'INPUT', 18.44, 223.56),
    ('bill/Net Connect/2026-09-11/Rpt', 1, 'Cable internet', 1.00, 50.00, '489', 'INPUT', 4.13, 50.00),
    ('bill/Swanston Security/2026-09-20/AP', 1, 'Our share building doorman/security', 1.00, 55.00, '453', 'INPUT', 4.54, 55.00),
    ('bill/Xero/2026-10-08/AP', 1, 'Monthly subscription', 1.00, 29.00, '412', 'INPUT', 2.39, 29.00),
    ('bill/Truxton Property Management/2026-10-11/RENT', 1, 'Monthy rent in advance', 1.00, 1091.22, '469', 'INPUT', 90.03, 1091.22),
    ('bill/PowerDirect/2026-10-11/Rpt', 1, 'Monthly power supply', 1.00, 105.00, '445', 'INPUT', 8.66, 105.00),
    ('bill/Net Connect/2026-10-12/Rpt', 1, 'Cable internet', 1.00, 39.00, '489', 'INPUT', 3.22, 39.00),
    ('bill/Swanston Security/2026-10-21/AP', 1, 'Our share building doorman/security', 1.00, 55.00, '453', 'INPUT', 4.54, 55.00),
    ('bill/Xero/2026-11-08/AP', 1, 'Monthly subscription', 1.00, 29.00, '412', 'INPUT', 2.39, 29.00),
    ('bill/Truxton Property Management/2026-11-11/RENT', 1, 'Monthy rent in advance', 1.00, 1091.22, '469', 'INPUT', 90.03, 1091.22),
    ('bill/PowerDirect/2026-11-11/Rpt', 1, 'Monthly power supply', 1.00, 105.00, '445', 'INPUT', 8.66, 105.00),
    ('bill/Net Connect/2026-11-12/Rpt', 1, 'Cable internet', 1.00, 39.00, '489', 'INPUT', 3.22, 39.00),
    ('bill/Swanston Security/2026-11-19/AP', 1, 'Our share building doorman/security', 1.00, 55.00, '453', 'INPUT', 4.54, 55.00);

CREATE TEMP TABLE xr_credit_note (
    cn_key      text,
    cn_number   text,
    contact     text,
    cn_type     text,
    cn_date     date,
    cn_status   text,
    sub_total   numeric,
    total_tax   numeric,
    total       numeric,
    remaining   numeric
) ON COMMIT DROP;

-- credit-notes.csv.  The file carries the total and the remaining credit but no
-- tax split; the split comes from the captured lines, and it adds up to the
-- captured total on all five notes (checked in section 7).
INSERT INTO xr_credit_note (cn_key, cn_number, contact, cn_type, cn_date, cn_status, sub_total, total_tax, total, remaining) VALUES
    ('credit-note/CN-0014', 'CN-0014', 'Boom FM', 'ACCRECCREDIT', DATE '2026-07-30', 'PAID', 500.00, 41.25, 541.25, 0.00),
    ('credit-note/CN-0015', 'CN-0015', 'Hamilton Smith Ltd', 'ACCRECCREDIT', DATE '2026-08-21', 'PAID', 500.00, 41.25, 541.25, 0.00),
    ('credit-note/CN-0023', 'CN-0023', 'DIISR - Small Business Services', 'ACCRECCREDIT', DATE '2026-08-16', 'PAID', 19.95, 1.75, 21.70, 0.00),
    ('credit-note/OG laptop', 'OG laptop', 'PC Complete', 'ACCPAYCREDIT', DATE '2026-07-27', 'PAID', 250.00, 20.63, 270.63, 0.00),
    ('credit-note/Refund', 'Refund', 'Swanston Security', 'ACCPAYCREDIT', DATE '2026-07-27', 'PAID', 23.50, 1.94, 25.44, 0.00);

CREATE TEMP TABLE xr_credit_note_line (
    cn_key       text,
    ord          int,
    description  text,
    quantity     numeric,
    unit_amount  numeric,
    account_code text,
    tax_type     text,
    tax_amount   numeric,
    line_amount  numeric
) ON COMMIT DROP;
INSERT INTO xr_credit_note_line (cn_key, ord, description, quantity, unit_amount, account_code, tax_type, tax_amount, line_amount) VALUES
    ('credit-note/CN-0014', 1, 'CREDIT Half day training - Microsoft Office and include in suite of training INV-0013', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('credit-note/CN-0015', 1, 'Full credit - DUPLICATE of INV-0001', 1.00, 500.00, '200', 'OUTPUT', 41.25, 500.00),
    ('credit-note/CN-0023', 1, '''Fish out of Water: Finding Your Brand'' - credit - charged in error - should be included overall project', 1.00, 19.95, '200', 'OUTPUT2', 1.75, 19.95),
    ('credit-note/OG laptop', 1, 'Unable to supply DVD writer for laptop - backorder', 1.00, 250.00, '453', 'INPUT', 20.63, 250.00),
    ('credit-note/Refund', 1, 'Refund as agreed due to window break when guard absent', 1.00, 23.50, '453', 'INPUT', 1.94, 23.50);

CREATE TEMP TABLE xr_bank (
    n            int,
    tx_date      date,
    contact      text,
    description  text,
    reference    text,
    amount       numeric,
    direction    text,
    tax_type     text,
    coded_code   text,
    leg_code     text,
    leg_tax_type text,
    leg_tax      numeric,
    leg_net      numeric,
    sub_total    numeric,
    total_tax    numeric,
    total        numeric,
    is_payment   boolean
) ON COMMIT DROP;

-- bank-transactions.csv, all 82 rows.  `amount` is signed_amount.
--
-- `leg_code` is the account the coded ledger leg posts to, which for a
-- settlement row ("Payment: ...") is the control account the CSV does not name:
-- 800 Accounts Payable for money out, 610 Accounts Receivable for money in, and
-- 801 Unpaid Expense Claims when the row the capture records is 801.  For the
-- 26 settlement rows `coded_code` is the account of the bill or invoice that was
-- paid -- the capture resolved it by following the transaction to its linked
-- document (docs/xero-reference/README.md, method notes) -- while Xero's ledger
-- clears the control account.  That is Xero's own behaviour, not a
-- contradiction: it is why 00023, which posted every row to coded_code, could
-- not reach Xero's Trial Balance, and why those postings are superseded below.
--
-- sub_total + total_tax = total on every row, and `total` is the captured
-- signed_amount unchanged.  For a direct spend or receipt sub_total is the
-- tax-exclusive part and total_tax the remainder; for a settlement both are the
-- captured amount and 0, because a payment of a document carries no tax of its
-- own -- the tax sits on the document.
INSERT INTO xr_bank (n, tx_date, contact, description, reference, amount, direction, tax_type, coded_code, leg_code, leg_tax_type, leg_tax, leg_net, sub_total, total_tax, total, is_payment) VALUES
    (1, DATE '2026-09-11', 'Telus', 'Telus', NULL, -110.00, 'out', 'INPUT', '489', '489', 'INPUT', 8.38, 101.62, -101.62, -8.38, -110.00, FALSE),
    (2, DATE '2026-09-11', '7-Eleven', '7-Eleven', NULL, -6.00, 'out', 'INPUT', '453', '453', 'INPUT', 0.46, 5.54, -5.54, -0.46, -6.00, FALSE),
    (3, DATE '2026-09-10', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000471', -216.50, 'out', 'INPUT', '408', '800', NULL, 0, -216.50, -216.50, 0, -216.50, TRUE),
    (4, DATE '2026-09-09', 'Petrie McLoud Watson & Associates', 'Payment: Petrie McLoud Watson & Associates', 'Consulting | INV-0036', 2240.78, 'in', 'OUTPUT', '200', '610', NULL, 0, 2240.78, 2240.78, 0, 2240.78, TRUE),
    (5, DATE '2026-09-09', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (6, DATE '2026-09-07', 'Boom FM', 'Payment: Boom FM', 'Training | INV-0033', 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (7, DATE '2026-09-07', 'Boom FM', 'Payment: Boom FM', 'Consulting | INV-0034', 3897.00, 'in', 'OUTPUT', '200', '610', NULL, 0, 3897.00, 3897.00, 0, 3897.00, TRUE),
    (8, DATE '2026-09-06', 'Bank West', 'Payment: Bank West', 'Website | INV-0032', 324.75, 'in', 'OUTPUT', '200', '610', NULL, 0, 324.75, 324.75, 0, 324.75, TRUE),
    (9, DATE '2026-09-06', '7-Eleven', '7-Eleven', NULL, -8.00, 'out', 'INPUT', '453', '453', 'INPUT', 0.61, 7.39, -7.39, -0.61, -8.00, FALSE),
    (10, DATE '2026-09-06', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (11, DATE '2026-09-06', '7-Eleven', '7-Eleven', NULL, -12.00, 'out', 'INPUT', '453', '453', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (12, DATE '2026-09-05', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000463', -216.50, 'out', 'INPUT', '408', '800', NULL, 0, -216.50, -216.50, 0, -216.50, TRUE),
    (13, DATE '2026-09-03', 'City Agency', 'Payment: City Agency', 'Golf Balls | INV-0031', 104.40, 'in', 'OUTPUT2', '200', '610', NULL, 0, 104.40, 104.40, 0, 104.40, TRUE),
    (14, DATE '2026-09-03', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (15, DATE '2026-09-02', '7-Eleven', '7-Eleven', NULL, -4.00, 'out', 'INPUT', '453', '453', 'INPUT', 0.30, 3.70, -3.70, -0.30, -4.00, FALSE),
    (16, DATE '2026-08-31', '7-Eleven', '7-Eleven', NULL, -5.50, 'out', 'INPUT', '453', '453', 'INPUT', 0.42, 5.08, -5.08, -0.42, -5.50, FALSE),
    (17, DATE '2026-08-31', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000456', -216.50, 'out', 'INPUT', '408', '800', NULL, 0, -216.50, -216.50, 0, -216.50, TRUE),
    (18, DATE '2026-08-31', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (19, DATE '2026-08-30', '7-Eleven', '7-Eleven', NULL, -12.50, 'out', 'INPUT', '453', '453', 'INPUT', 0.95, 11.55, -11.55, -0.95, -12.50, FALSE),
    (20, DATE '2026-08-30', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000442', -216.50, 'out', 'INPUT', '408', '800', NULL, 0, -216.50, -216.50, 0, -216.50, TRUE),
    (21, DATE '2026-08-26', '7-Eleven', '7-Eleven', NULL, -6.00, 'out', 'INPUT', '453', '453', 'INPUT', 0.46, 5.54, -5.54, -0.46, -6.00, FALSE),
    (22, DATE '2026-08-26', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (23, DATE '2026-08-26', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (24, DATE '2026-08-24', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (25, DATE '2026-08-23', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000435', -216.50, 'out', 'INPUT', '408', '800', NULL, 0, -216.50, -216.50, 0, -216.50, TRUE),
    (26, DATE '2026-08-22', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (27, DATE '2026-08-21', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (28, DATE '2026-08-21', 'ABC Furniture', 'Payment: ABC Furniture', '710', -1000.00, 'out', 'INPUT', '710', '800', NULL, 0, -1000.00, -1000.00, 0, -1000.00, TRUE),
    (29, DATE '2026-08-21', 'Rex Media Group', 'Payment: Rex Media Group', 'Monthly Support | INV-0021', 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (30, DATE '2026-08-21', 'Port & Philip Freight', 'Payment: Port & Philip Freight', 'Monthly Support | INV-0020', 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (31, DATE '2026-08-21', 'PowerDirect', 'Payment: multiple items', NULL, -1363.92, 'out', 'INPUT', '445', '800', NULL, 0, -1363.92, -1363.92, 0, -1363.92, TRUE),
    (32, DATE '2026-08-21', 'Young Bros Transport', 'Payment: Young Bros Transport', 'Monthly Support | INV-0019', 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (33, DATE '2026-08-21', 'Carlton Functions', 'Payment: Carlton Functions', 'Dep', -1500.00, 'out', 'NONE', '420', '800', NULL, 0, -1500.00, -1500.00, 0, -1500.00, TRUE),
    (34, DATE '2026-08-21', 'Hamilton Smith Ltd', 'Payment: Hamilton Smith Ltd', 'Monthly Support | INV-0018', 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (35, DATE '2026-08-21', 'Hoyt Productions', 'Payment: Hoyt Productions', '08-4123', -5953.75, 'out', 'INPUT', '400', '800', NULL, 0, -5953.75, -5953.75, 0, -5953.75, TRUE),
    (36, DATE '2026-08-20', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (37, DATE '2026-08-20', '24 Locks', '24 Locks', NULL, -69.50, 'out', 'INPUT', '473', '473', 'INPUT', 5.30, 64.20, -64.20, -5.30, -69.50, FALSE),
    (38, DATE '2026-08-20', 'Berry Brew', 'Berry Brew', NULL, -22.00, 'out', 'NONE', '420', '420', 'NONE', 0.00, 22.00, -22.00, 0.00, -22.00, FALSE),
    (39, DATE '2026-08-19', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (40, DATE '2026-08-18', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (41, DATE '2026-08-16', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (42, DATE '2026-08-15', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (43, DATE '2026-08-14', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (44, DATE '2026-08-14', 'Office Supplies Company', 'Office Supplies Company', NULL, -49.20, 'out', 'INPUT', '461', '461', 'INPUT', 3.75, 45.45, -45.45, -3.75, -49.20, FALSE),
    (45, DATE '2026-08-13', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493', '493', 'INPUT', 0.91, 11.09, -11.09, -0.91, -12.00, FALSE),
    (46, DATE '2026-08-12', 'Melrose Parking', 'Melrose Parking', NULL, -148.50, 'out', 'INPUT', '449', '449', 'INPUT', 11.32, 137.18, -137.18, -11.32, -148.50, FALSE),
    (47, DATE '2026-08-11', 'Woolworths Market', 'Woolworths Market', NULL, -34.10, 'out', 'INPUT', '453', '453', 'INPUT', 2.60, 31.50, -31.50, -2.60, -34.10, FALSE),
    (48, DATE '2026-08-11', 'Boom FM', 'Payment: Boom FM', 'Training | INV-0013', 1082.50, 'in', 'OUTPUT', '200', '610', NULL, 0, 1082.50, 1082.50, 0, 1082.50, TRUE),
    (49, DATE '2026-09-07', 'Gateway Motors', 'Payment: Gateway Motors', NULL, -411.35, 'out', 'INPUT', '449', '800', NULL, 0, -411.35, -411.35, 0, -411.35, TRUE),
    (50, DATE '2026-08-31', 'Truxton Property Management', 'Payment: Truxton Property Management', 'RENT', -1181.25, 'out', 'INPUT', '469', '800', NULL, 0, -1181.25, -1181.25, 0, -1181.25, TRUE),
    (51, DATE '2026-08-11', 'DIISR - Small Business Services', 'Payment: DIISR - Small Business Services', 'Yr Ref W08-143 | INV-0016', 568.31, 'in', 'OUTPUT', '200', '610', NULL, 0, 568.31, 568.31, 0, 568.31, TRUE),
    (52, DATE '2026-08-11', 'DIISR - Small Business Services', 'Payment: DIISR - Small Business Services', 'Yr Ref W08-143 | INV-0022', 216.50, 'in', 'OUTPUT2', '200', '610', NULL, 0, 216.50, 216.50, 0, 216.50, TRUE),
    (53, DATE '2026-08-11', 'Ridgeway University', 'Payment: Ridgeway University', 'P/O CRM08-12 | INV-0009', 6187.50, 'in', 'OUTPUT', '200', '610', NULL, 0, 6187.50, 6187.50, 0, 6187.50, TRUE),
    (54, DATE '2026-08-11', 'Orlena Greenville', 'Orlena Greenville', NULL, -29.50, 'out', 'INPUT', '453', '453', 'INPUT', 2.25, 27.25, -27.25, -2.25, -29.50, FALSE),
    (55, DATE '2026-08-11', 'Petrie McLoud Watson & Associates', 'Payment: Petrie McLoud Watson & Associates', 'Portal Proj | INV-0011', 1407.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 1407.25, 1407.25, 0, 1407.25, TRUE),
    (56, DATE '2026-08-11', 'Orlena Greenville', 'Payment: Orlena Greenville', NULL, -29.50, 'out', 'NONE', '801', '801', NULL, 0, -29.50, -29.50, 0, -29.50, TRUE),
    (57, DATE '2026-08-08', 'Xero', 'Payment: Xero', 'AP', -31.39, 'out', 'INPUT', '412', '800', NULL, 0, -31.39, -31.39, 0, -31.39, TRUE),
    (58, DATE '2026-08-01', 'PC Complete', 'Payment: PC Complete', 'OG laptop', -270.63, 'out', 'INPUT', '453', '800', NULL, 0, -270.63, -270.63, 0, -270.63, TRUE),
    (59, DATE '2026-08-01', 'Ridgeway Bank', 'Ridgeway Bank', 'Fee', -15.00, 'out', 'NONE', '404', '404', 'NONE', 0.00, 15.00, -15.00, 0.00, -15.00, FALSE),
    (60, DATE '2026-07-30', 'Net Connect', 'Payment: Net Connect', '9781', -1463.88, 'out', 'OUTPUT2', '453', '800', NULL, 0, -1463.88, -1463.88, 0, -1463.88, TRUE),
    (61, DATE '2026-07-27', 'Central Copiers', 'Payment: Central Copiers', '945-OCon', -900.00, 'out', 'INPUT', '473', '800', NULL, 0, -900.00, -900.00, 0, -900.00, TRUE),
    (62, DATE '2026-07-27', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', '5679', -119.08, 'out', 'INPUT', '408', '800', NULL, 0, -119.08, -119.08, 0, -119.08, TRUE),
    (63, DATE '2026-07-27', 'Orlena Greenville', 'Payment: Orlena Greenville', NULL, -34.90, 'out', 'NONE', '801', '801', NULL, 0, -34.90, -34.90, 0, -34.90, TRUE),
    (64, DATE '2026-07-27', 'Swanston Security', 'Payment: Swanston Security', 'AP', -34.10, 'out', 'INPUT', '453', '800', NULL, 0, -34.10, -34.10, 0, -34.10, TRUE),
    (65, DATE '2026-07-25', 'City Agency', 'Payment: City Agency', 'Workshop | INV-0007', 593.23, 'in', 'OUTPUT', '200', '610', NULL, 0, 593.23, 593.23, 0, 593.23, TRUE),
    (66, DATE '2026-07-21', 'Truxton Property Management', 'Payment: Truxton Property Management', 'RENT', -1181.25, 'out', 'INPUT', '469', '800', NULL, 0, -1181.25, -1181.25, 0, -1181.25, TRUE),
    (67, DATE '2026-07-21', 'Net Connect', 'Payment: Net Connect', 'Rpt', -44.92, 'out', 'INPUT', '489', '800', NULL, 0, -44.92, -44.92, 0, -44.92, TRUE),
    (68, DATE '2026-07-21', 'PC Complete', 'Payment: PC Complete', NULL, -1682.74, 'out', 'INPUT', '720', '800', NULL, 0, -1682.74, -1682.74, 0, -1682.74, TRUE),
    (69, DATE '2026-07-21', 'Bank West', 'Payment: Bank West', NULL, 1299.00, 'in', 'OUTPUT', '200', '610', NULL, 0, 1299.00, 1299.00, 0, 1299.00, TRUE),
    (70, DATE '2026-07-21', 'PowerDirect', 'Payment: PowerDirect', 'Rpt', -119.08, 'out', 'INPUT', '445', '800', NULL, 0, -119.08, -119.08, 0, -119.08, TRUE),
    (71, DATE '2026-07-20', 'Hamilton Smith Ltd', 'Payment: Hamilton Smith Ltd', NULL, 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (72, DATE '2026-07-20', 'Rex Media Group', 'Payment: Rex Media Group', NULL, 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (73, DATE '2026-07-20', 'Port & Philip Freight', 'Payment: Port & Philip Freight', NULL, 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (74, DATE '2026-07-20', 'Young Bros Transport', 'Payment: Young Bros Transport', NULL, 541.25, 'in', 'OUTPUT', '200', '610', NULL, 0, 541.25, 541.25, 0, 541.25, TRUE),
    (75, DATE '2026-07-20', 'Melrose Parking', 'Melrose Parking', 'Chq 409', -148.50, 'out', 'INPUT', '449', '449', 'INPUT', 11.32, 137.18, -137.18, -11.32, -148.50, FALSE),
    (76, DATE '2026-07-18', 'Berry Brew', 'Berry Brew', NULL, -15.60, 'out', 'NONE', '420', '420', 'NONE', 0.00, 15.60, -15.60, 0.00, -15.60, FALSE),
    (77, DATE '2026-07-15', 'Brunswick Petals', 'Brunswick Petals', 'Gift', -50.00, 'out', 'INPUT', '429', '429', 'INPUT', 3.81, 46.19, -46.19, -3.81, -50.00, FALSE),
    (78, DATE '2026-07-11', 'Woolworths Market', 'Woolworths Market', NULL, -65.20, 'out', 'INPUT', '453', '453', 'INPUT', 4.97, 60.23, -60.23, -4.97, -65.20, FALSE),
    (79, DATE '2026-07-08', 'Espresso 31', 'Espresso 31', NULL, -16.00, 'out', 'NONE', '420', '420', 'NONE', 0.00, 16.00, -16.00, 0.00, -16.00, FALSE),
    (80, DATE '2026-07-08', 'Xero', 'Payment: Xero', NULL, -31.39, 'out', 'INPUT', '412', '800', NULL, 0, -31.39, -31.39, 0, -31.39, TRUE),
    (81, DATE '2026-07-06', 'Office Supplies Company', 'Office Supplies Company', 'Eft', -23.50, 'out', 'INPUT', '461', '461', 'INPUT', 1.79, 21.71, -21.71, -1.79, -23.50, FALSE),
    (82, DATE '2026-07-01', 'Ridgeway Bank', 'Ridgeway Bank', 'Fee', -15.00, 'out', 'NONE', '404', '404', 'NONE', 0.00, 15.00, -15.00, 0.00, -15.00, FALSE);

CREATE TEMP TABLE xr_payment (
    n            int,
    pay_date     date,
    contact      text,
    doc_type     text,
    doc_number   text,
    amount       numeric,
    account_code text
) ON COMMIT DROP;

-- payments.csv.  A payment row records which document a settlement cleared; it
-- carries no posting of its own, because the money side of the payment is the
-- bank transaction the same capture already holds (the "Payment: ..." rows).
INSERT INTO xr_payment (n, pay_date, contact, doc_type, doc_number, amount, account_code) VALUES
    (1, DATE '2026-08-01', 'PC Complete', 'ACCPAY', 'OG laptop', 270.63, '800'),
    (2, DATE '2026-09-10', 'MCO Cleaning Services', 'ACCPAY', 'M000471', 216.50, '800'),
    (3, DATE '2026-08-11', 'Boom FM', 'ACCREC', 'INV-0013', 1082.50, '610'),
    (4, DATE '2026-08-11', 'DIISR - Small Business Services', 'ACCREC', 'INV-0016', 568.31, '610'),
    (5, DATE '2026-08-11', 'DIISR - Small Business Services', 'ACCREC', 'INV-0022', 216.50, '610'),
    (6, DATE '2026-08-11', 'Petrie McLoud Watson & Associates', 'ACCREC', 'INV-0011', 1407.25, '610'),
    (7, DATE '2026-08-11', 'Ridgeway University', 'ACCREC', 'INV-0009', 6187.50, '610'),
    (8, DATE '2026-07-20', 'Hamilton Smith Ltd', 'ACCREC', 'INV-0001', 541.25, '610'),
    (9, DATE '2026-07-20', 'Port & Philip Freight', 'ACCREC', 'INV-0003', 541.25, '610'),
    (10, DATE '2026-07-20', 'Rex Media Group', 'ACCREC', 'INV-0004', 541.25, '610'),
    (11, DATE '2026-07-20', 'Young Bros Transport', 'ACCREC', 'INV-0002', 541.25, '610'),
    (12, DATE '2026-08-21', 'ABC Furniture', 'ACCPAY', '710', 1000.00, '800'),
    (13, DATE '2026-08-21', 'Carlton Functions', 'ACCPAY', 'Dep', 1500.00, '800'),
    (14, DATE '2026-08-21', 'Hamilton Smith Ltd', 'ACCREC', 'INV-0018', 541.25, '610'),
    (15, DATE '2026-08-21', 'Hoyt Productions', 'ACCPAY', '08-4123', 5953.75, '800'),
    (16, DATE '2026-08-21', 'Net Connect', 'ACCPAY', 'Rpt', 46.82, '800'),
    (17, DATE '2026-08-21', 'Port & Philip Freight', 'ACCREC', 'INV-0020', 541.25, '610'),
    (18, DATE '2026-08-21', 'PowerDirect', 'ACCPAY', 'Rpt', 135.85, '800'),
    (19, DATE '2026-08-21', 'Rex Media Group', 'ACCREC', 'INV-0021', 541.25, '610'),
    (20, DATE '2026-08-21', 'Truxton Property Management', 'ACCPAY', 'RENT', 1181.25, '800'),
    (21, DATE '2026-08-21', 'Young Bros Transport', 'ACCREC', 'INV-0019', 541.25, '610'),
    (22, DATE '2026-07-21', 'Bank West', 'ACCREC', 'INV-0008', 1299.00, '610'),
    (23, DATE '2026-07-21', 'Net Connect', 'ACCPAY', 'Rpt', 44.92, '800'),
    (24, DATE '2026-07-21', 'PC Complete', 'ACCPAY', NULL, 1682.74, '800'),
    (25, DATE '2026-07-21', 'PowerDirect', 'ACCPAY', 'Rpt', 119.08, '800'),
    (26, DATE '2026-07-21', 'Truxton Property Management', 'ACCPAY', 'RENT', 1181.25, '800'),
    (27, DATE '2026-08-23', 'MCO Cleaning Services', 'ACCPAY', 'M000435', 216.50, '800'),
    (28, DATE '2026-07-25', 'City Agency', 'ACCREC', 'INV-0007', 593.23, '610'),
    (29, DATE '2026-07-27', 'Central Copiers', 'ACCPAY', '945-OCon', 900.00, '800'),
    (30, DATE '2026-07-27', 'MCO Cleaning Services', 'ACCPAY', '5679', 119.08, '800'),
    (31, DATE '2026-07-27', 'Swanston Security', 'ACCPAY', 'AP', 34.10, '800'),
    (32, DATE '2026-09-03', 'City Agency', 'ACCREC', 'INV-0031', 104.40, '610'),
    (33, DATE '2026-08-30', 'MCO Cleaning Services', 'ACCPAY', 'M000442', 216.50, '800'),
    (34, DATE '2026-07-30', 'Net Connect', 'ACCPAY', '9781', 1463.88, '800'),
    (35, DATE '2026-08-31', 'MCO Cleaning Services', 'ACCPAY', 'M000456', 216.50, '800'),
    (36, DATE '2026-08-31', 'Truxton Property Management', 'ACCPAY', 'RENT', 1181.25, '800'),
    (37, DATE '2026-09-05', 'MCO Cleaning Services', 'ACCPAY', 'M000463', 216.50, '800'),
    (38, DATE '2026-09-06', 'Bank West', 'ACCREC', 'INV-0032', 324.75, '610'),
    (39, DATE '2026-09-07', 'Boom FM', 'ACCREC', 'INV-0034', 3897.00, '610'),
    (40, DATE '2026-09-07', 'Boom FM', 'ACCREC', 'INV-0033', 541.25, '610'),
    (41, DATE '2026-09-07', 'Gateway Motors', 'ACCPAY', NULL, 411.35, '800'),
    (42, DATE '2026-08-08', 'Xero', 'ACCPAY', 'AP', 31.39, '800'),
    (43, DATE '2026-07-08', 'Xero', 'ACCPAY', 'AP', 31.39, '800'),
    (44, DATE '2026-09-09', 'Petrie McLoud Watson & Associates', 'ACCREC', 'INV-0036', 2240.78, '610');

CREATE TEMP TABLE xr_claim (
    claim_key    text,
    contact      text,
    claim_date   date,
    total        numeric,
    status       text,
    paid_date    date,
    paid_amount  numeric,
    amount_due   numeric
) ON COMMIT DROP;

-- expense-claims.csv.  The file holds one row per claim LINE, not per claim:
-- its two 34.90 rows of 11 Jul 2026 are one claim of 34.90 with two lines,
-- which is how the posting table in reconciliation.md section 2 reads it
-- ("a claim with two lines occupies two rows but posts one liability") and how
-- the ledger posts it -- one 801 credit of 34.90, not two.  So this table holds
-- one row per CLAIM, keyed the way the ledger journal for it is keyed, and no
-- claim_total is counted twice.  The claim line detail is not lost: it is in
-- gl_journal_lines, where source_type = EXPENSECLAIM holds the account, the
-- description and the tax of every captured line.
--
-- `status` is Xero's paid/unpaid flag mapped onto the schema's CHECK: paid
-- becomes PAID, unpaid becomes AUTHORISED -- the state in which a claim is a
-- live payable, which is the population Xero's Aged Payables Summary prints.
-- amount_due is claim_total - paid_amount, so the unpaid claim is the only one
-- with anything owing and the only one that contributes to 801.
INSERT INTO xr_claim (claim_key, contact, claim_date, total, status, paid_date, paid_amount, amount_due) VALUES
    ('expense-claim/Xero Demo/2026-09-10', 'Xero Demo', DATE '2026-09-10', 115.95, 'AUTHORISED', NULL, 0.00, 115.95),
    ('expense-claim/Orlena Greenville/2026-08-11', 'Orlena Greenville', DATE '2026-08-11', 29.50, 'PAID', DATE '2026-08-11', 29.50, 0.00),
    ('expense-claim/Orlena Greenville/2026-07-11', 'Orlena Greenville', DATE '2026-07-11', 34.90, 'PAID', DATE '2026-07-27', 34.90, 0.00);

CREATE TEMP TABLE xr_gl (
    journal_key  text,
    jdate        date,
    reference    text,
    source_type  text,
    source_key   text,
    line_key     text,
    account_code text,
    description  text,
    tax_type     text,
    tax_amount   numeric,
    net_amount   numeric
) ON COMMIT DROP;

-- The posting run itself: one row per ledger line, grouped into journals by
-- journal_key.  Every group below is a rule from
-- docs/xero-reference/reconciliation.md section 2.
INSERT INTO xr_gl (journal_key, jdate, reference, source_type, source_key, line_key, account_code, description, tax_type, tax_amount, net_amount) VALUES
    ('invoice/INV-0001', DATE '2026-07-11', 'INV-0001', 'INVOICE', 'invoice/INV-0001', 'control', '610', 'INV-0001', NULL, 0.00, 541.25),
    ('invoice/INV-0001', DATE '2026-07-11', 'INV-0001', 'INVOICE', 'invoice/INV-0001', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0001', DATE '2026-07-11', 'INV-0001', 'INVOICE', 'invoice/INV-0001', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0002', DATE '2026-07-11', 'INV-0002', 'INVOICE', 'invoice/INV-0002', 'control', '610', 'INV-0002', NULL, 0.00, 541.25),
    ('invoice/INV-0002', DATE '2026-07-11', 'INV-0002', 'INVOICE', 'invoice/INV-0002', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0002', DATE '2026-07-11', 'INV-0002', 'INVOICE', 'invoice/INV-0002', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0003', DATE '2026-07-11', 'INV-0003', 'INVOICE', 'invoice/INV-0003', 'control', '610', 'INV-0003', NULL, 0.00, 541.25),
    ('invoice/INV-0003', DATE '2026-07-11', 'INV-0003', 'INVOICE', 'invoice/INV-0003', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0003', DATE '2026-07-11', 'INV-0003', 'INVOICE', 'invoice/INV-0003', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0004', DATE '2026-07-11', 'INV-0004', 'INVOICE', 'invoice/INV-0004', 'control', '610', 'INV-0004', NULL, 0.00, 541.25),
    ('invoice/INV-0004', DATE '2026-07-11', 'INV-0004', 'INVOICE', 'invoice/INV-0004', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0004', DATE '2026-07-11', 'INV-0004', 'INVOICE', 'invoice/INV-0004', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0005', DATE '2026-07-12', 'INV-0005', 'INVOICE', 'invoice/INV-0005', 'control', '610', 'INV-0005', NULL, 0.00, 541.25),
    ('invoice/INV-0005', DATE '2026-07-12', 'INV-0005', 'INVOICE', 'invoice/INV-0005', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0005', DATE '2026-07-12', 'INV-0005', 'INVOICE', 'invoice/INV-0005', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0006', DATE '2026-07-10', 'INV-0006', 'INVOICE', 'invoice/INV-0006', 'control', '610', 'INV-0006', NULL, 0.00, 250.00),
    ('invoice/INV-0006', DATE '2026-07-10', 'INV-0006', 'INVOICE', 'invoice/INV-0006', 'line/1', '200', 'Project management & implementation - branding workshop with your team', 'OUTPUT', 0.00, -230.95),
    ('invoice/INV-0006', DATE '2026-07-10', 'INV-0006', 'INVOICE', 'invoice/INV-0006', 'tax', '820', NULL, NULL, 0.00, -19.05),
    ('invoice/INV-0007', DATE '2026-07-14', 'INV-0007', 'INVOICE', 'invoice/INV-0007', 'control', '610', 'INV-0007', NULL, 0.00, 593.23),
    ('invoice/INV-0007', DATE '2026-07-14', 'INV-0007', 'INVOICE', 'invoice/INV-0007', 'line/1', '200', 'Project management & implementation - branding workshop with your team ======================== - ''Buzz Words'' session with your Steering Group - Analysis of current marketing materials - Workshop on re-brand outcomes and stakeholder identification - Analysis and presentation of findings to your Steering Group & Board', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0007', DATE '2026-07-14', 'INV-0007', 'INVOICE', 'invoice/INV-0007', 'line/2', '200', 'Copies of ''Fish out of Water'' text for your Branding Team', 'OUTPUT2', 0.00, -47.80),
    ('invoice/INV-0007', DATE '2026-07-14', 'INV-0007', 'INVOICE', 'invoice/INV-0007', 'tax', '820', NULL, NULL, 0.00, -45.43),
    ('invoice/INV-0008', DATE '2026-07-13', 'INV-0008', 'INVOICE', 'invoice/INV-0008', 'control', '610', 'INV-0008', NULL, 0.00, 1299.00),
    ('invoice/INV-0008', DATE '2026-07-13', 'INV-0008', 'INVOICE', 'invoice/INV-0008', 'line/1', '200', 'Half day training - Microsoft Office - for your Priority Mortgage Services Team (Session 3)', 'OUTPUT', 0.00, -400.00),
    ('invoice/INV-0008', DATE '2026-07-13', 'INV-0008', 'INVOICE', 'invoice/INV-0008', 'line/2', '200', 'Half day training - Microsoft Office - for your Lending Services Team (Session 2)', 'OUTPUT', 0.00, -400.00),
    ('invoice/INV-0008', DATE '2026-07-13', 'INV-0008', 'INVOICE', 'invoice/INV-0008', 'line/3', '200', 'Half day training - Microsoft Office - for your Customer Support Team (Session 1)', 'OUTPUT', 0.00, -400.00),
    ('invoice/INV-0008', DATE '2026-07-13', 'INV-0008', 'INVOICE', 'invoice/INV-0008', 'tax', '820', NULL, NULL, 0.00, -99.00),
    ('invoice/INV-0009', DATE '2026-07-20', 'INV-0009', 'INVOICE', 'invoice/INV-0009', 'control', '610', 'INV-0009', NULL, 0.00, 6187.50),
    ('invoice/INV-0009', DATE '2026-07-20', 'INV-0009', 'INVOICE', 'invoice/INV-0009', 'line/1', '200', 'Onsite project management for CRM Project 3 days/week', 'OUTPUT', 0.00, -5715.94),
    ('invoice/INV-0009', DATE '2026-07-20', 'INV-0009', 'INVOICE', 'invoice/INV-0009', 'tax', '820', NULL, NULL, 0.00, -471.56),
    ('invoice/INV-0010', DATE '2026-07-19', 'INV-0010', 'INVOICE', 'invoice/INV-0010', 'control', '610', 'INV-0010', NULL, 0.00, 541.25),
    ('invoice/INV-0010', DATE '2026-07-19', 'INV-0010', 'INVOICE', 'invoice/INV-0010', 'line/1', '200', 'Half day training - Microsoft Office', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0010', DATE '2026-07-19', 'INV-0010', 'INVOICE', 'invoice/INV-0010', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0011', DATE '2026-07-25', 'INV-0011', 'INVOICE', 'invoice/INV-0011', 'control', '610', 'INV-0011', NULL, 0.00, 1407.25),
    ('invoice/INV-0011', DATE '2026-07-25', 'INV-0011', 'INVOICE', 'invoice/INV-0011', 'line/1', '200', 'Development work - develper onsite per day', 'OUTPUT', 0.00, -1300.00),
    ('invoice/INV-0011', DATE '2026-07-25', 'INV-0011', 'INVOICE', 'invoice/INV-0011', 'tax', '820', NULL, NULL, 0.00, -107.25),
    ('invoice/INV-0012', DATE '2026-07-27', 'INV-0012', 'INVOICE', 'invoice/INV-0012', 'control', '610', 'INV-0012', NULL, 0.00, 216.50),
    ('invoice/INV-0012', DATE '2026-07-27', 'INV-0012', 'INVOICE', 'invoice/INV-0012', 'line/1', '200', 'Project management & implementation - branding workshop with your team - follow up session', 'OUTPUT', 0.00, -200.00),
    ('invoice/INV-0012', DATE '2026-07-27', 'INV-0012', 'INVOICE', 'invoice/INV-0012', 'tax', '820', NULL, NULL, 0.00, -16.50),
    ('invoice/INV-0013', DATE '2026-07-30', 'INV-0013', 'INVOICE', 'invoice/INV-0013', 'control', '610', 'INV-0013', NULL, 0.00, 1082.50),
    ('invoice/INV-0013', DATE '2026-07-30', 'INV-0013', 'INVOICE', 'invoice/INV-0013', 'line/1', '200', 'Half day training - Microsoft Office - reception staff (Session 1)', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0013', DATE '2026-07-30', 'INV-0013', 'INVOICE', 'invoice/INV-0013', 'line/2', '200', 'Half day training - Microsoft Office - operations staff (Session 2)', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0013', DATE '2026-07-30', 'INV-0013', 'INVOICE', 'invoice/INV-0013', 'tax', '820', NULL, NULL, 0.00, -82.50),
    ('invoice/INV-0016', DATE '2026-08-01', 'INV-0016', 'INVOICE', 'invoice/INV-0016', 'control', '610', 'INV-0016', NULL, 0.00, 838.94),
    ('invoice/INV-0016', DATE '2026-08-01', 'INV-0016', 'INVOICE', 'invoice/INV-0016', 'line/1', '200', 'Project management & implementation - branding workshop with your team', 'OUTPUT', 0.00, -250.00),
    ('invoice/INV-0016', DATE '2026-08-01', 'INV-0016', 'INVOICE', 'invoice/INV-0016', 'line/2', '200', 'Project management & implementation - ''due diligence'' stocktake of your project scope/schedule/implementation plan/outcome measures (hourly rate as agreed)', 'OUTPUT', 0.00, -525.00),
    ('invoice/INV-0016', DATE '2026-08-01', 'INV-0016', 'INVOICE', 'invoice/INV-0016', 'tax', '820', NULL, NULL, 0.00, -63.94),
    ('invoice/INV-0017', DATE '2026-07-30', 'INV-0017', 'INVOICE', 'invoice/INV-0017', 'control', '610', 'INV-0017', NULL, 0.00, 21.70),
    ('invoice/INV-0017', DATE '2026-07-30', 'INV-0017', 'INVOICE', 'invoice/INV-0017', 'line/1', '200', '''Fish out of Water: Finding Your Brand''', 'OUTPUT2', 0.00, -19.95),
    ('invoice/INV-0017', DATE '2026-07-30', 'INV-0017', 'INVOICE', 'invoice/INV-0017', 'tax', '820', NULL, NULL, 0.00, -1.75),
    ('invoice/INV-0018', DATE '2026-08-11', 'INV-0018', 'INVOICE', 'invoice/INV-0018', 'control', '610', 'INV-0018', NULL, 0.00, 541.25),
    ('invoice/INV-0018', DATE '2026-08-11', 'INV-0018', 'INVOICE', 'invoice/INV-0018', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0018', DATE '2026-08-11', 'INV-0018', 'INVOICE', 'invoice/INV-0018', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0019', DATE '2026-08-11', 'INV-0019', 'INVOICE', 'invoice/INV-0019', 'control', '610', 'INV-0019', NULL, 0.00, 541.25),
    ('invoice/INV-0019', DATE '2026-08-11', 'INV-0019', 'INVOICE', 'invoice/INV-0019', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0019', DATE '2026-08-11', 'INV-0019', 'INVOICE', 'invoice/INV-0019', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0020', DATE '2026-08-11', 'INV-0020', 'INVOICE', 'invoice/INV-0020', 'control', '610', 'INV-0020', NULL, 0.00, 541.25),
    ('invoice/INV-0020', DATE '2026-08-11', 'INV-0020', 'INVOICE', 'invoice/INV-0020', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0020', DATE '2026-08-11', 'INV-0020', 'INVOICE', 'invoice/INV-0020', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0021', DATE '2026-08-11', 'INV-0021', 'INVOICE', 'invoice/INV-0021', 'control', '610', 'INV-0021', NULL, 0.00, 541.25),
    ('invoice/INV-0021', DATE '2026-08-11', 'INV-0021', 'INVOICE', 'invoice/INV-0021', 'line/1', '200', 'Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0021', DATE '2026-08-11', 'INV-0021', 'INVOICE', 'invoice/INV-0021', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0022', DATE '2026-08-11', 'INV-0022', 'INVOICE', 'invoice/INV-0022', 'control', '610', 'INV-0022', NULL, 0.00, 238.20),
    ('invoice/INV-0022', DATE '2026-08-11', 'INV-0022', 'INVOICE', 'invoice/INV-0022', 'line/1', '200', '''Fish out of Water: Finding Your Brand''', 'OUTPUT2', 0.00, -19.95),
    ('invoice/INV-0022', DATE '2026-08-11', 'INV-0022', 'INVOICE', 'invoice/INV-0022', 'line/2', '200', 'Project management & implementation - branding workshop with your team - follow up', 'OUTPUT', 0.00, -200.00),
    ('invoice/INV-0022', DATE '2026-08-11', 'INV-0022', 'INVOICE', 'invoice/INV-0022', 'tax', '820', NULL, NULL, 0.00, -18.25),
    ('invoice/INV-0024', DATE '2026-09-05', 'INV-0024', 'INVOICE', 'invoice/INV-0024', 'control', '610', 'INV-0024', NULL, 0.00, 703.63),
    ('invoice/INV-0024', DATE '2026-09-05', 'INV-0024', 'INVOICE', 'invoice/INV-0024', 'line/1', '200', 'Development work - develper onsite per day', 'OUTPUT', 0.00, -650.00),
    ('invoice/INV-0024', DATE '2026-09-05', 'INV-0024', 'INVOICE', 'invoice/INV-0024', 'tax', '820', NULL, NULL, 0.00, -53.63),
    ('invoice/INV-0025', DATE '2026-08-20', 'INV-0025', 'INVOICE', 'invoice/INV-0025', 'control', '610', 'INV-0025', NULL, 0.00, 6187.50),
    ('invoice/INV-0025', DATE '2026-08-20', 'INV-0025', 'INVOICE', 'invoice/INV-0025', 'line/1', '200', 'Onsite project management for CRM Project 3 days/week', 'OUTPUT', 0.00, -5715.94),
    ('invoice/INV-0025', DATE '2026-08-20', 'INV-0025', 'INVOICE', 'invoice/INV-0025', 'tax', '820', NULL, NULL, 0.00, -471.56),
    ('invoice/INV-0026', DATE '2026-09-10', 'INV-0026', 'INVOICE', 'invoice/INV-0026', 'control', '610', 'INV-0026', NULL, 0.00, 914.55),
    ('invoice/INV-0026', DATE '2026-09-10', 'INV-0026', 'INVOICE', 'invoice/INV-0026', 'line/1', '200', 'Development work - per hour rate', 'OUTPUT', 0.00, -434.18),
    ('invoice/INV-0026', DATE '2026-09-10', 'INV-0026', 'INVOICE', 'invoice/INV-0026', 'line/2', '200', 'Project team meeting to discuss dev changes required to your online gift basket ordering system', 'OUTPUT', 0.00, -410.67),
    ('invoice/INV-0026', DATE '2026-09-10', 'INV-0026', 'INVOICE', 'invoice/INV-0026', 'tax', '820', NULL, NULL, 0.00, -69.70),
    ('invoice/INV-0027', DATE '2026-09-10', 'INV-0027', 'INVOICE', 'invoice/INV-0027', 'control', '610', 'INV-0027', NULL, 0.00, 396.00),
    ('invoice/INV-0027', DATE '2026-09-10', 'INV-0027', 'INVOICE', 'invoice/INV-0027', 'line/1', '200', 'Marketing guides', 'OUTPUT', 0.00, -365.82),
    ('invoice/INV-0027', DATE '2026-09-10', 'INV-0027', 'INVOICE', 'invoice/INV-0027', 'tax', '820', NULL, NULL, 0.00, -30.18),
    ('invoice/INV-0028', DATE '2026-09-10', 'INV-0028', 'INVOICE', 'invoice/INV-0028', 'control', '610', 'INV-0028', NULL, 0.00, 234.00),
    ('invoice/INV-0028', DATE '2026-09-10', 'INV-0028', 'INVOICE', 'invoice/INV-0028', 'line/1', '200', 'Golf balls - white single', 'OUTPUT', 0.00, -206.93),
    ('invoice/INV-0028', DATE '2026-09-10', 'INV-0028', 'INVOICE', 'invoice/INV-0028', 'line/2', '425', 'Delivery charge', 'NONE', 0.00, -10.00),
    ('invoice/INV-0028', DATE '2026-09-10', 'INV-0028', 'INVOICE', 'invoice/INV-0028', 'tax', '820', NULL, NULL, 0.00, -17.07),
    ('invoice/INV-0031', DATE '2026-08-26', 'INV-0031', 'INVOICE', 'invoice/INV-0031', 'control', '610', 'INV-0031', NULL, 0.00, 104.40),
    ('invoice/INV-0031', DATE '2026-08-26', 'INV-0031', 'INVOICE', 'invoice/INV-0031', 'line/1', '200', 'Golf Balls', 'OUTPUT2', 0.00, -96.00),
    ('invoice/INV-0031', DATE '2026-08-26', 'INV-0031', 'INVOICE', 'invoice/INV-0031', 'tax', '820', NULL, NULL, 0.00, -8.40),
    ('invoice/INV-0032', DATE '2026-08-26', 'INV-0032', 'INVOICE', 'invoice/INV-0032', 'control', '610', 'INV-0032', NULL, 0.00, 324.75),
    ('invoice/INV-0032', DATE '2026-08-26', 'INV-0032', 'INVOICE', 'invoice/INV-0032', 'line/1', '200', 'Website creation', 'OUTPUT', 0.00, -300.00),
    ('invoice/INV-0032', DATE '2026-08-26', 'INV-0032', 'INVOICE', 'invoice/INV-0032', 'tax', '820', NULL, NULL, 0.00, -24.75),
    ('invoice/INV-0033', DATE '2026-08-31', 'INV-0033', 'INVOICE', 'invoice/INV-0033', 'control', '610', 'INV-0033', NULL, 0.00, 541.25),
    ('invoice/INV-0033', DATE '2026-08-31', 'INV-0033', 'INVOICE', 'invoice/INV-0033', 'line/1', '200', 'Half day training - Microsoft Office - Marketing team', 'OUTPUT', 0.00, -500.00),
    ('invoice/INV-0033', DATE '2026-08-31', 'INV-0033', 'INVOICE', 'invoice/INV-0033', 'tax', '820', NULL, NULL, 0.00, -41.25),
    ('invoice/INV-0034', DATE '2026-09-03', 'INV-0034', 'INVOICE', 'invoice/INV-0034', 'control', '610', 'INV-0034', NULL, 0.00, 3897.00),
    ('invoice/INV-0034', DATE '2026-09-03', 'INV-0034', 'INVOICE', 'invoice/INV-0034', 'line/1', '200', 'Development work - software integration', 'OUTPUT', 0.00, -3600.00),
    ('invoice/INV-0034', DATE '2026-09-03', 'INV-0034', 'INVOICE', 'invoice/INV-0034', 'tax', '820', NULL, NULL, 0.00, -297.00),
    ('invoice/INV-0036', DATE '2026-09-03', 'INV-0036', 'INVOICE', 'invoice/INV-0036', 'control', '610', 'INV-0036', NULL, 0.00, 2240.78),
    ('invoice/INV-0036', DATE '2026-09-03', 'INV-0036', 'INVOICE', 'invoice/INV-0036', 'line/1', '200', 'Development work - software integration', 'OUTPUT', 0.00, -2070.00),
    ('invoice/INV-0036', DATE '2026-09-03', 'INV-0036', 'INVOICE', 'invoice/INV-0036', 'tax', '820', NULL, NULL, 0.00, -170.78),
    ('bill/Xero/2026-07-08/AP', DATE '2026-07-08', 'AP', 'INVOICE', 'bill/Xero/2026-07-08/AP', 'control', '800', 'AP', NULL, 0.00, -31.39),
    ('bill/Xero/2026-07-08/AP', DATE '2026-07-08', 'AP', 'INVOICE', 'bill/Xero/2026-07-08/AP', 'line/1', '412', 'Monthly subscription', 'INPUT', 0.00, 29.00),
    ('bill/Xero/2026-07-08/AP', DATE '2026-07-08', 'AP', 'INVOICE', 'bill/Xero/2026-07-08/AP', 'tax', '820', NULL, NULL, 0.00, 2.39),
    ('bill/Truxton Property Management/2026-07-11/RENT', DATE '2026-07-11', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-07-11/RENT', 'control', '800', 'RENT', NULL, 0.00, -1181.25),
    ('bill/Truxton Property Management/2026-07-11/RENT', DATE '2026-07-11', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-07-11/RENT', 'line/1', '469', 'Monthy rent in advance', 'INPUT', 0.00, 1091.22),
    ('bill/Truxton Property Management/2026-07-11/RENT', DATE '2026-07-11', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-07-11/RENT', 'tax', '820', NULL, NULL, 0.00, 90.03),
    ('bill/PowerDirect/2026-07-11/Rpt', DATE '2026-07-11', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-07-11/Rpt', 'control', '800', 'Rpt', NULL, 0.00, -119.08),
    ('bill/PowerDirect/2026-07-11/Rpt', DATE '2026-07-11', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-07-11/Rpt', 'line/1', '445', 'Monthly power supply', 'INPUT', 0.00, 110.00),
    ('bill/PowerDirect/2026-07-11/Rpt', DATE '2026-07-11', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-07-11/Rpt', 'tax', '820', NULL, NULL, 0.00, 9.08),
    ('bill/Net Connect/2026-07-12/Rpt', DATE '2026-07-12', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-07-12/Rpt', 'control', '800', 'Rpt', NULL, 0.00, -44.92),
    ('bill/Net Connect/2026-07-12/Rpt', DATE '2026-07-12', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-07-12/Rpt', 'line/1', '489', 'Cable internet', 'INPUT', 0.00, 41.50),
    ('bill/Net Connect/2026-07-12/Rpt', DATE '2026-07-12', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-07-12/Rpt', 'tax', '820', NULL, NULL, 0.00, 3.42),
    ('bill/Central Copiers/2026-07-19/945-OCon', DATE '2026-07-19', '945-OCon', 'INVOICE', 'bill/Central Copiers/2026-07-19/945-OCon', 'control', '800', '945-OCon', NULL, 0.00, -1063.56),
    ('bill/Central Copiers/2026-07-19/945-OCon', DATE '2026-07-19', '945-OCon', 'INVOICE', 'bill/Central Copiers/2026-07-19/945-OCon', 'line/1', '473', 'Photocopier repair & drum replacement', 'INPUT', 0.00, 982.50),
    ('bill/Central Copiers/2026-07-19/945-OCon', DATE '2026-07-19', '945-OCon', 'INVOICE', 'bill/Central Copiers/2026-07-19/945-OCon', 'tax', '820', NULL, NULL, 0.00, 81.06),
    ('bill/Net Connect/2026-07-20/9781', DATE '2026-07-20', '9781', 'INVOICE', 'bill/Net Connect/2026-07-20/9781', 'control', '800', '9781', NULL, 0.00, -1463.88),
    ('bill/Net Connect/2026-07-20/9781', DATE '2026-07-20', '9781', 'INVOICE', 'bill/Net Connect/2026-07-20/9781', 'line/1', '453', 'Network diagnostics software', 'OUTPUT2', 0.00, 500.00),
    ('bill/Net Connect/2026-07-20/9781', DATE '2026-07-20', '9781', 'INVOICE', 'bill/Net Connect/2026-07-20/9781', 'line/2', '473', 'Replacement hub & network switches', 'INPUT', 0.00, 850.00),
    ('bill/Net Connect/2026-07-20/9781', DATE '2026-07-20', '9781', 'INVOICE', 'bill/Net Connect/2026-07-20/9781', 'tax', '820', NULL, NULL, 0.00, 113.88),
    ('bill/PC Complete/2026-07-21/', DATE '2026-07-21', NULL, 'INVOICE', 'bill/PC Complete/2026-07-21/', 'control', '800', NULL, NULL, 0.00, -1953.37),
    ('bill/PC Complete/2026-07-21/', DATE '2026-07-21', NULL, 'INVOICE', 'bill/PC Complete/2026-07-21/', 'line/1', '720', 'Laptop (Oliver)', 'INPUT', 0.00, 1804.50),
    ('bill/PC Complete/2026-07-21/', DATE '2026-07-21', NULL, 'INVOICE', 'bill/PC Complete/2026-07-21/', 'tax', '820', NULL, NULL, 0.00, 148.87),
    ('bill/MCO Cleaning Services/2026-07-21/5679', DATE '2026-07-21', '5679', 'INVOICE', 'bill/MCO Cleaning Services/2026-07-21/5679', 'control', '800', '5679', NULL, 0.00, -119.08),
    ('bill/MCO Cleaning Services/2026-07-21/5679', DATE '2026-07-21', '5679', 'INVOICE', 'bill/MCO Cleaning Services/2026-07-21/5679', 'line/1', '408', 'Office Cleaning for month', 'INPUT', 0.00, 110.00),
    ('bill/MCO Cleaning Services/2026-07-21/5679', DATE '2026-07-21', '5679', 'INVOICE', 'bill/MCO Cleaning Services/2026-07-21/5679', 'tax', '820', NULL, NULL, 0.00, 9.08),
    ('bill/Swanston Security/2026-07-21/AP', DATE '2026-07-21', 'AP', 'INVOICE', 'bill/Swanston Security/2026-07-21/AP', 'control', '800', 'AP', NULL, 0.00, -59.54),
    ('bill/Swanston Security/2026-07-21/AP', DATE '2026-07-21', 'AP', 'INVOICE', 'bill/Swanston Security/2026-07-21/AP', 'line/1', '453', 'Our share building doorman/security', 'INPUT', 0.00, 55.00),
    ('bill/Swanston Security/2026-07-21/AP', DATE '2026-07-21', 'AP', 'INVOICE', 'bill/Swanston Security/2026-07-21/AP', 'tax', '820', NULL, NULL, 0.00, 4.54),
    ('bill/SMART Agency/2026-07-21/SM0195', DATE '2026-07-21', 'SM0195', 'INVOICE', 'bill/SMART Agency/2026-07-21/SM0195', 'control', '800', 'SM0195', NULL, 0.00, -2000.00),
    ('bill/SMART Agency/2026-07-21/SM0195', DATE '2026-07-21', 'SM0195', 'INVOICE', 'bill/SMART Agency/2026-07-21/SM0195', 'line/1', '400', 'Design concepts for Oaktown Business Leader ad series', 'INPUT', 0.00, 1847.58),
    ('bill/SMART Agency/2026-07-21/SM0195', DATE '2026-07-21', 'SM0195', 'INVOICE', 'bill/SMART Agency/2026-07-21/SM0195', 'tax', '820', NULL, NULL, 0.00, 152.42),
    ('bill/PC Complete/2026-08-01/OG laptop', DATE '2026-08-01', 'OG laptop', 'INVOICE', 'bill/PC Complete/2026-08-01/OG laptop', 'control', '800', 'OG laptop', NULL, 0.00, -270.63),
    ('bill/PC Complete/2026-08-01/OG laptop', DATE '2026-08-01', 'OG laptop', 'INVOICE', 'bill/PC Complete/2026-08-01/OG laptop', 'line/1', '453', 'DVD writer for laptop', 'INPUT', 0.00, 250.00),
    ('bill/PC Complete/2026-08-01/OG laptop', DATE '2026-08-01', 'OG laptop', 'INVOICE', 'bill/PC Complete/2026-08-01/OG laptop', 'tax', '820', NULL, NULL, 0.00, 20.63),
    ('bill/Xero/2026-08-08/AP', DATE '2026-08-08', 'AP', 'INVOICE', 'bill/Xero/2026-08-08/AP', 'control', '800', 'AP', NULL, 0.00, -31.39),
    ('bill/Xero/2026-08-08/AP', DATE '2026-08-08', 'AP', 'INVOICE', 'bill/Xero/2026-08-08/AP', 'line/1', '412', 'Monthly subscription', 'INPUT', 0.00, 29.00),
    ('bill/Xero/2026-08-08/AP', DATE '2026-08-08', 'AP', 'INVOICE', 'bill/Xero/2026-08-08/AP', 'tax', '820', NULL, NULL, 0.00, 2.39),
    ('bill/MCO Cleaning Services/2026-08-08/M000435', DATE '2026-08-08', 'M000435', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-08/M000435', 'control', '800', 'M000435', NULL, 0.00, -216.50),
    ('bill/MCO Cleaning Services/2026-08-08/M000435', DATE '2026-08-08', 'M000435', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-08/M000435', 'line/1', '408', 'Office Cleaning - daily rate', 'INPUT', 0.00, 200.00),
    ('bill/MCO Cleaning Services/2026-08-08/M000435', DATE '2026-08-08', 'M000435', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-08/M000435', 'tax', '820', NULL, NULL, 0.00, 16.50),
    ('bill/Hoyt Productions/2026-08-11/08-4123', DATE '2026-08-11', '08-4123', 'INVOICE', 'bill/Hoyt Productions/2026-08-11/08-4123', 'control', '800', '08-4123', NULL, 0.00, -5953.75),
    ('bill/Hoyt Productions/2026-08-11/08-4123', DATE '2026-08-11', '08-4123', 'INVOICE', 'bill/Hoyt Productions/2026-08-11/08-4123', 'line/1', '400', '20-second still frame ad shown in 5 city cinemas 5 times each', 'INPUT', 0.00, 5500.00),
    ('bill/Hoyt Productions/2026-08-11/08-4123', DATE '2026-08-11', '08-4123', 'INVOICE', 'bill/Hoyt Productions/2026-08-11/08-4123', 'tax', '820', NULL, NULL, 0.00, 453.75),
    ('bill/Carlton Functions/2026-08-11/Dep', DATE '2026-08-11', 'Dep', 'INVOICE', 'bill/Carlton Functions/2026-08-11/Dep', 'control', '800', 'Dep', NULL, 0.00, -1500.00),
    ('bill/Carlton Functions/2026-08-11/Dep', DATE '2026-08-11', 'Dep', 'INVOICE', 'bill/Carlton Functions/2026-08-11/Dep', 'line/1', '420', 'Deposit on venue hire for client function', 'NONE', 0.00, 1500.00),
    ('bill/Carlton Functions/2026-08-11/Dep', DATE '2026-08-11', 'Dep', 'INVOICE', 'bill/Carlton Functions/2026-08-11/Dep', 'tax', '820', NULL, NULL, 0.00, 0.00),
    ('bill/Truxton Property Management/2026-08-11/RENT', DATE '2026-08-11', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-08-11/RENT', 'control', '800', 'RENT', NULL, 0.00, -1181.25),
    ('bill/Truxton Property Management/2026-08-11/RENT', DATE '2026-08-11', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-08-11/RENT', 'line/1', '469', 'Monthy rent in advance', 'INPUT', 0.00, 1091.22),
    ('bill/Truxton Property Management/2026-08-11/RENT', DATE '2026-08-11', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-08-11/RENT', 'tax', '820', NULL, NULL, 0.00, 90.03),
    ('bill/PowerDirect/2026-08-11/Rpt', DATE '2026-08-11', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-08-11/Rpt', 'control', '800', 'Rpt', NULL, 0.00, -135.85),
    ('bill/PowerDirect/2026-08-11/Rpt', DATE '2026-08-11', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-08-11/Rpt', 'line/1', '445', 'Monthly power supply', 'INPUT', 0.00, 125.50),
    ('bill/PowerDirect/2026-08-11/Rpt', DATE '2026-08-11', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-08-11/Rpt', 'tax', '820', NULL, NULL, 0.00, 10.35),
    ('bill/Net Connect/2026-08-12/Rpt', DATE '2026-08-12', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-08-12/Rpt', 'control', '800', 'Rpt', NULL, 0.00, -46.82),
    ('bill/Net Connect/2026-08-12/Rpt', DATE '2026-08-12', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-08-12/Rpt', 'line/1', '489', 'Cable internet', 'INPUT', 0.00, 43.25),
    ('bill/Net Connect/2026-08-12/Rpt', DATE '2026-08-12', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-08-12/Rpt', 'tax', '820', NULL, NULL, 0.00, 3.57),
    ('bill/MCO Cleaning Services/2026-08-15/M000442', DATE '2026-08-15', 'M000442', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-15/M000442', 'control', '800', 'M000442', NULL, 0.00, -216.50),
    ('bill/MCO Cleaning Services/2026-08-15/M000442', DATE '2026-08-15', 'M000442', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-15/M000442', 'line/1', '408', 'Office Cleaning - daily rate', 'INPUT', 0.00, 200.00),
    ('bill/MCO Cleaning Services/2026-08-15/M000442', DATE '2026-08-15', 'M000442', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-15/M000442', 'tax', '820', NULL, NULL, 0.00, 16.50),
    ('bill/ABC Furniture/2026-08-21/710', DATE '2026-08-21', '710', 'INVOICE', 'bill/ABC Furniture/2026-08-21/710', 'control', '800', '710', NULL, 0.00, -1000.00),
    ('bill/ABC Furniture/2026-08-21/710', DATE '2026-08-21', '710', 'INVOICE', 'bill/ABC Furniture/2026-08-21/710', 'line/1', '710', 'Coffee table for reception', 'INPUT', 0.00, 923.79),
    ('bill/ABC Furniture/2026-08-21/710', DATE '2026-08-21', '710', 'INVOICE', 'bill/ABC Furniture/2026-08-21/710', 'tax', '820', NULL, NULL, 0.00, 76.21),
    ('bill/Swanston Security/2026-08-21/AP', DATE '2026-08-21', 'AP', 'INVOICE', 'bill/Swanston Security/2026-08-21/AP', 'control', '800', 'AP', NULL, 0.00, -59.54),
    ('bill/Swanston Security/2026-08-21/AP', DATE '2026-08-21', 'AP', 'INVOICE', 'bill/Swanston Security/2026-08-21/AP', 'line/1', '453', 'Our share building doorman/security', 'INPUT', 0.00, 55.00),
    ('bill/Swanston Security/2026-08-21/AP', DATE '2026-08-21', 'AP', 'INVOICE', 'bill/Swanston Security/2026-08-21/AP', 'tax', '820', NULL, NULL, 0.00, 4.54),
    ('bill/MCO Cleaning Services/2026-08-22/M000456', DATE '2026-08-22', 'M000456', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-22/M000456', 'control', '800', 'M000456', NULL, 0.00, -216.50),
    ('bill/MCO Cleaning Services/2026-08-22/M000456', DATE '2026-08-22', 'M000456', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-22/M000456', 'line/1', '408', 'Office Cleaning - daily rate', 'INPUT', 0.00, 200.00),
    ('bill/MCO Cleaning Services/2026-08-22/M000456', DATE '2026-08-22', 'M000456', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-22/M000456', 'tax', '820', NULL, NULL, 0.00, 16.50),
    ('bill/MCO Cleaning Services/2026-08-29/M000463', DATE '2026-08-29', 'M000463', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-29/M000463', 'control', '800', 'M000463', NULL, 0.00, -216.50),
    ('bill/MCO Cleaning Services/2026-08-29/M000463', DATE '2026-08-29', 'M000463', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-29/M000463', 'line/1', '408', 'Office Cleaning - daily rate', 'INPUT', 0.00, 200.00),
    ('bill/MCO Cleaning Services/2026-08-29/M000463', DATE '2026-08-29', 'M000463', 'INVOICE', 'bill/MCO Cleaning Services/2026-08-29/M000463', 'tax', '820', NULL, NULL, 0.00, 16.50),
    ('bill/Truxton Property Management/2026-08-31/RENT', DATE '2026-08-31', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-08-31/RENT', 'control', '800', 'RENT', NULL, 0.00, -1181.25),
    ('bill/Truxton Property Management/2026-08-31/RENT', DATE '2026-08-31', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-08-31/RENT', 'line/1', '469', 'Monthy rent in advance', 'INPUT', 0.00, 1091.22),
    ('bill/Truxton Property Management/2026-08-31/RENT', DATE '2026-08-31', 'RENT', 'INVOICE', 'bill/Truxton Property Management/2026-08-31/RENT', 'tax', '820', NULL, NULL, 0.00, 90.03),
    ('bill/SMART Agency/2026-08-31/SM0210', DATE '2026-08-31', 'SM0210', 'INVOICE', 'bill/SMART Agency/2026-08-31/SM0210', 'control', '800', 'SM0210', NULL, 0.00, -2500.00),
    ('bill/SMART Agency/2026-08-31/SM0210', DATE '2026-08-31', 'SM0210', 'INVOICE', 'bill/SMART Agency/2026-08-31/SM0210', 'line/1', '400', 'Prototype media banner & print mockups for Oaktown Business Leader ad series', 'INPUT', 0.00, 2309.47),
    ('bill/SMART Agency/2026-08-31/SM0210', DATE '2026-08-31', 'SM0210', 'INVOICE', 'bill/SMART Agency/2026-08-31/SM0210', 'tax', '820', NULL, NULL, 0.00, 190.53),
    ('bill/Bayside Club/2026-09-05/', DATE '2026-09-05', NULL, 'INVOICE', 'bill/Bayside Club/2026-09-05/', 'control', '800', NULL, NULL, 0.00, -130.00),
    ('bill/Bayside Club/2026-09-05/', DATE '2026-09-05', NULL, 'INVOICE', 'bill/Bayside Club/2026-09-05/', 'line/1', '429', 'Room hire', 'INPUT', 0.00, 120.09),
    ('bill/Bayside Club/2026-09-05/', DATE '2026-09-05', NULL, 'INVOICE', 'bill/Bayside Club/2026-09-05/', 'tax', '820', NULL, NULL, 0.00, 9.91),
    ('bill/PC Complete/2026-09-05/', DATE '2026-09-05', NULL, 'INVOICE', 'bill/PC Complete/2026-09-05/', 'control', '800', NULL, NULL, 0.00, -2132.51),
    ('bill/PC Complete/2026-09-05/', DATE '2026-09-05', NULL, 'INVOICE', 'bill/PC Complete/2026-09-05/', 'line/1', '720', 'Laptop (Tracy)', 'INPUT', 0.00, 1969.99),
    ('bill/PC Complete/2026-09-05/', DATE '2026-09-05', NULL, 'INVOICE', 'bill/PC Complete/2026-09-05/', 'tax', '820', NULL, NULL, 0.00, 162.52),
    ('bill/MCO Cleaning Services/2026-09-05/M000471', DATE '2026-09-05', 'M000471', 'INVOICE', 'bill/MCO Cleaning Services/2026-09-05/M000471', 'control', '800', 'M000471', NULL, 0.00, -216.50),
    ('bill/MCO Cleaning Services/2026-09-05/M000471', DATE '2026-09-05', 'M000471', 'INVOICE', 'bill/MCO Cleaning Services/2026-09-05/M000471', 'line/1', '408', 'Office Cleaning - daily rate', 'INPUT', 0.00, 200.00),
    ('bill/MCO Cleaning Services/2026-09-05/M000471', DATE '2026-09-05', 'M000471', 'INVOICE', 'bill/MCO Cleaning Services/2026-09-05/M000471', 'tax', '820', NULL, NULL, 0.00, 16.50),
    ('bill/Gateway Motors/2026-09-06/', DATE '2026-09-06', NULL, 'INVOICE', 'bill/Gateway Motors/2026-09-06/', 'control', '800', NULL, NULL, 0.00, -411.35),
    ('bill/Gateway Motors/2026-09-06/', DATE '2026-09-06', NULL, 'INVOICE', 'bill/Gateway Motors/2026-09-06/', 'line/1', '449', 'Annual service company car', 'INPUT', 0.00, 380.00),
    ('bill/Gateway Motors/2026-09-06/', DATE '2026-09-06', NULL, 'INVOICE', 'bill/Gateway Motors/2026-09-06/', 'tax', '820', NULL, NULL, 0.00, 31.35),
    ('bill/Young Bros Transport/2026-09-07/', DATE '2026-09-07', NULL, 'INVOICE', 'bill/Young Bros Transport/2026-09-07/', 'control', '800', NULL, NULL, 0.00, -125.03),
    ('bill/Young Bros Transport/2026-09-07/', DATE '2026-09-07', NULL, 'INVOICE', 'bill/Young Bros Transport/2026-09-07/', 'line/1', '425', 'Delivery of new reception desk', 'INPUT', 0.00, 115.50),
    ('bill/Young Bros Transport/2026-09-07/', DATE '2026-09-07', NULL, 'INVOICE', 'bill/Young Bros Transport/2026-09-07/', 'tax', '820', NULL, NULL, 0.00, 9.53),
    ('bill/Xero/2026-09-07/AP', DATE '2026-09-07', 'AP', 'INVOICE', 'bill/Xero/2026-09-07/AP', 'control', '800', 'AP', NULL, 0.00, -31.39),
    ('bill/Xero/2026-09-07/AP', DATE '2026-09-07', 'AP', 'INVOICE', 'bill/Xero/2026-09-07/AP', 'line/1', '412', 'Monthly subscription', 'INPUT', 0.00, 29.00),
    ('bill/Xero/2026-09-07/AP', DATE '2026-09-07', 'AP', 'INVOICE', 'bill/Xero/2026-09-07/AP', 'tax', '820', NULL, NULL, 0.00, 2.39),
    ('bill/Bayside Wholesale/2026-09-08/GB1-White', DATE '2026-09-08', 'GB1-White', 'INVOICE', 'bill/Bayside Wholesale/2026-09-08/GB1-White', 'control', '800', 'GB1-White', NULL, 0.00, -840.00),
    ('bill/Bayside Wholesale/2026-09-08/GB1-White', DATE '2026-09-08', 'GB1-White', 'INVOICE', 'bill/Bayside Wholesale/2026-09-08/GB1-White', 'line/1', '300', 'Golf balls - white single', 'INPUT', 0.00, 775.98),
    ('bill/Bayside Wholesale/2026-09-08/GB1-White', DATE '2026-09-08', 'GB1-White', 'INVOICE', 'bill/Bayside Wholesale/2026-09-08/GB1-White', 'tax', '820', NULL, NULL, 0.00, 64.02),
    ('bill/PowerDirect/2026-09-10/Rpt', DATE '2026-09-10', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-09-10/Rpt', 'control', '800', 'Rpt', NULL, 0.00, -108.60),
    ('bill/PowerDirect/2026-09-10/Rpt', DATE '2026-09-10', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-09-10/Rpt', 'line/1', '445', 'Monthly power supply', 'INPUT', 0.00, 100.32),
    ('bill/PowerDirect/2026-09-10/Rpt', DATE '2026-09-10', 'Rpt', 'INVOICE', 'bill/PowerDirect/2026-09-10/Rpt', 'tax', '820', NULL, NULL, 0.00, 8.28),
    ('bill/Capital Cab Co/2026-09-11/CS815', DATE '2026-09-11', 'CS815', 'INVOICE', 'bill/Capital Cab Co/2026-09-11/CS815', 'control', '800', 'CS815', NULL, 0.00, -242.00),
    ('bill/Capital Cab Co/2026-09-11/CS815', DATE '2026-09-11', 'CS815', 'INVOICE', 'bill/Capital Cab Co/2026-09-11/CS815', 'line/1', '493', 'Taxi services', 'INPUT', 0.00, 223.56),
    ('bill/Capital Cab Co/2026-09-11/CS815', DATE '2026-09-11', 'CS815', 'INVOICE', 'bill/Capital Cab Co/2026-09-11/CS815', 'tax', '820', NULL, NULL, 0.00, 18.44),
    ('bill/Net Connect/2026-09-11/Rpt', DATE '2026-09-11', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-09-11/Rpt', 'control', '800', 'Rpt', NULL, 0.00, -54.13),
    ('bill/Net Connect/2026-09-11/Rpt', DATE '2026-09-11', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-09-11/Rpt', 'line/1', '489', 'Cable internet', 'INPUT', 0.00, 50.00),
    ('bill/Net Connect/2026-09-11/Rpt', DATE '2026-09-11', 'Rpt', 'INVOICE', 'bill/Net Connect/2026-09-11/Rpt', 'tax', '820', NULL, NULL, 0.00, 4.13),
    ('credit-note/CN-0014', DATE '2026-07-30', 'CN-0014', 'CREDITNOTE', 'credit-note/CN-0014', 'control', '610', 'CN-0014', NULL, 0.00, -541.25),
    ('credit-note/CN-0014', DATE '2026-07-30', 'CN-0014', 'CREDITNOTE', 'credit-note/CN-0014', 'line/1', '200', 'CREDIT Half day training - Microsoft Office and include in suite of training INV-0013', 'OUTPUT', 0.00, 500.00),
    ('credit-note/CN-0014', DATE '2026-07-30', 'CN-0014', 'CREDITNOTE', 'credit-note/CN-0014', 'tax', '820', NULL, NULL, 0.00, 41.25),
    ('credit-note/CN-0015', DATE '2026-08-21', 'CN-0015', 'CREDITNOTE', 'credit-note/CN-0015', 'control', '610', 'CN-0015', NULL, 0.00, -541.25),
    ('credit-note/CN-0015', DATE '2026-08-21', 'CN-0015', 'CREDITNOTE', 'credit-note/CN-0015', 'line/1', '200', 'Full credit - DUPLICATE of INV-0001', 'OUTPUT', 0.00, 500.00),
    ('credit-note/CN-0015', DATE '2026-08-21', 'CN-0015', 'CREDITNOTE', 'credit-note/CN-0015', 'tax', '820', NULL, NULL, 0.00, 41.25),
    ('credit-note/CN-0023', DATE '2026-08-16', 'CN-0023', 'CREDITNOTE', 'credit-note/CN-0023', 'control', '610', 'CN-0023', NULL, 0.00, -21.70),
    ('credit-note/CN-0023', DATE '2026-08-16', 'CN-0023', 'CREDITNOTE', 'credit-note/CN-0023', 'line/1', '200', '''Fish out of Water: Finding Your Brand'' - credit - charged in error - should be included overall project', 'OUTPUT2', 0.00, 19.95),
    ('credit-note/CN-0023', DATE '2026-08-16', 'CN-0023', 'CREDITNOTE', 'credit-note/CN-0023', 'tax', '820', NULL, NULL, 0.00, 1.75),
    ('credit-note/OG laptop', DATE '2026-07-27', 'OG laptop', 'CREDITNOTE', 'credit-note/OG laptop', 'control', '800', 'OG laptop', NULL, 0.00, 270.63),
    ('credit-note/OG laptop', DATE '2026-07-27', 'OG laptop', 'CREDITNOTE', 'credit-note/OG laptop', 'line/1', '453', 'Unable to supply DVD writer for laptop - backorder', 'INPUT', 0.00, -250.00),
    ('credit-note/OG laptop', DATE '2026-07-27', 'OG laptop', 'CREDITNOTE', 'credit-note/OG laptop', 'tax', '820', NULL, NULL, 0.00, -20.63),
    ('credit-note/Refund', DATE '2026-07-27', 'Refund', 'CREDITNOTE', 'credit-note/Refund', 'control', '800', 'Refund', NULL, 0.00, 25.44),
    ('credit-note/Refund', DATE '2026-07-27', 'Refund', 'CREDITNOTE', 'credit-note/Refund', 'line/1', '453', 'Refund as agreed due to window break when guard absent', 'INPUT', 0.00, -23.50),
    ('credit-note/Refund', DATE '2026-07-27', 'Refund', 'CREDITNOTE', 'credit-note/Refund', 'tax', '820', NULL, NULL, 0.00, -1.94),
    ('expense-claim/Xero Demo/2026-09-10', DATE '2026-09-10', NULL, 'EXPENSECLAIM', NULL, 'control', '801', NULL, NULL, 0.00, -115.95),
    ('expense-claim/Xero Demo/2026-09-10', DATE '2026-09-10', NULL, 'EXPENSECLAIM', NULL, 'line/1', '453', 'Xero Demo - Battery pack & power cable for home office', 'INPUT', 0.00, 107.11),
    ('expense-claim/Xero Demo/2026-09-10', DATE '2026-09-10', NULL, 'EXPENSECLAIM', NULL, 'tax/1', '820', NULL, NULL, 0.00, 8.84),
    ('expense-claim/Orlena Greenville/2026-08-11', DATE '2026-08-11', NULL, 'EXPENSECLAIM', NULL, 'control', '801', NULL, NULL, 0.00, -29.50),
    ('expense-claim/Orlena Greenville/2026-08-11', DATE '2026-08-11', NULL, 'EXPENSECLAIM', NULL, 'line/1', '461', 'Orlena Greenville - Print/bind report', 'INPUT', 0.00, 27.25),
    ('expense-claim/Orlena Greenville/2026-08-11', DATE '2026-08-11', NULL, 'EXPENSECLAIM', NULL, 'tax/1', '820', NULL, NULL, 0.00, 2.25),
    ('expense-claim/Orlena Greenville/2026-07-11', DATE '2026-07-11', NULL, 'EXPENSECLAIM', NULL, 'control', '801', NULL, NULL, 0.00, -34.90),
    ('expense-claim/Orlena Greenville/2026-07-11', DATE '2026-07-11', NULL, 'EXPENSECLAIM', NULL, 'line/1', '493', 'Orlena Greenville - Parking for MRE conference', 'INPUT', 0.00, 16.63),
    ('expense-claim/Orlena Greenville/2026-07-11', DATE '2026-07-11', NULL, 'EXPENSECLAIM', NULL, 'tax/1', '820', NULL, NULL, 0.00, 1.37),
    ('expense-claim/Orlena Greenville/2026-07-11', DATE '2026-07-11', NULL, 'EXPENSECLAIM', NULL, 'line/2', '493', 'Orlena Greenville - Breakfast before MRE conference', 'INPUT', 0.00, 15.61),
    ('expense-claim/Orlena Greenville/2026-07-11', DATE '2026-07-11', NULL, 'EXPENSECLAIM', NULL, 'tax/2', '820', NULL, NULL, 0.00, 1.29),
    ('opening-balance/journal/388', DATE '2026-06-21', 'Conversion Balance', 'MANUALJOURNAL', NULL, 'line/090', '090', 'Conversion Balance', NULL, 0.00, 4130.98),
    ('opening-balance/journal/388', DATE '2026-06-21', 'Conversion Balance', 'MANUALJOURNAL', NULL, 'line/840', '840', 'Conversion Balance', NULL, 0.00, -4130.98),
    ('bank-transaction/1', DATE '2026-09-11', NULL, 'BANKTRANSACTION', 'bank-transaction/1', 'coded', '489', 'Telus', 'INPUT', 8.38, 101.62),
    ('bank-transaction/1', DATE '2026-09-11', NULL, 'BANKTRANSACTION', 'bank-transaction/1', 'tax', '820', NULL, NULL, 0.00, 8.38),
    ('bank-transaction/1', DATE '2026-09-11', NULL, 'BANKTRANSACTION', 'bank-transaction/1', 'bank', '090', NULL, NULL, 0.00, -110.00),
    ('bank-transaction/2', DATE '2026-09-11', NULL, 'BANKTRANSACTION', 'bank-transaction/2', 'coded', '453', '7-Eleven', 'INPUT', 0.46, 5.54),
    ('bank-transaction/2', DATE '2026-09-11', NULL, 'BANKTRANSACTION', 'bank-transaction/2', 'tax', '820', NULL, NULL, 0.00, 0.46),
    ('bank-transaction/2', DATE '2026-09-11', NULL, 'BANKTRANSACTION', 'bank-transaction/2', 'bank', '090', NULL, NULL, 0.00, -6.00),
    ('bank-transaction/3', DATE '2026-09-10', 'M000471', 'BANKTRANSACTION', 'bank-transaction/3', 'control', '800', 'MCO Cleaning Services', NULL, 0.00, 216.50),
    ('bank-transaction/3', DATE '2026-09-10', 'M000471', 'BANKTRANSACTION', 'bank-transaction/3', 'bank', '090', NULL, NULL, 0.00, -216.50),
    ('bank-transaction/4', DATE '2026-09-09', 'Consulting | INV-0036', 'BANKTRANSACTION', 'bank-transaction/4', 'control', '610', 'Petrie McLoud Watson & Associates', NULL, 0.00, -2240.78),
    ('bank-transaction/4', DATE '2026-09-09', 'Consulting | INV-0036', 'BANKTRANSACTION', 'bank-transaction/4', 'bank', '090', NULL, NULL, 0.00, 2240.78),
    ('bank-transaction/5', DATE '2026-09-09', NULL, 'BANKTRANSACTION', 'bank-transaction/5', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/5', DATE '2026-09-09', NULL, 'BANKTRANSACTION', 'bank-transaction/5', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/5', DATE '2026-09-09', NULL, 'BANKTRANSACTION', 'bank-transaction/5', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/6', DATE '2026-09-07', 'Training | INV-0033', 'BANKTRANSACTION', 'bank-transaction/6', 'control', '610', 'Boom FM', NULL, 0.00, -541.25),
    ('bank-transaction/6', DATE '2026-09-07', 'Training | INV-0033', 'BANKTRANSACTION', 'bank-transaction/6', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/7', DATE '2026-09-07', 'Consulting | INV-0034', 'BANKTRANSACTION', 'bank-transaction/7', 'control', '610', 'Boom FM', NULL, 0.00, -3897.00),
    ('bank-transaction/7', DATE '2026-09-07', 'Consulting | INV-0034', 'BANKTRANSACTION', 'bank-transaction/7', 'bank', '090', NULL, NULL, 0.00, 3897.00),
    ('bank-transaction/8', DATE '2026-09-06', 'Website | INV-0032', 'BANKTRANSACTION', 'bank-transaction/8', 'control', '610', 'Bank West', NULL, 0.00, -324.75),
    ('bank-transaction/8', DATE '2026-09-06', 'Website | INV-0032', 'BANKTRANSACTION', 'bank-transaction/8', 'bank', '090', NULL, NULL, 0.00, 324.75),
    ('bank-transaction/9', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/9', 'coded', '453', '7-Eleven', 'INPUT', 0.61, 7.39),
    ('bank-transaction/9', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/9', 'tax', '820', NULL, NULL, 0.00, 0.61),
    ('bank-transaction/9', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/9', 'bank', '090', NULL, NULL, 0.00, -8.00),
    ('bank-transaction/10', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/10', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/10', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/10', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/10', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/10', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/11', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/11', 'coded', '453', '7-Eleven', 'INPUT', 0.91, 11.09),
    ('bank-transaction/11', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/11', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/11', DATE '2026-09-06', NULL, 'BANKTRANSACTION', 'bank-transaction/11', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/12', DATE '2026-09-05', 'M000463', 'BANKTRANSACTION', 'bank-transaction/12', 'control', '800', 'MCO Cleaning Services', NULL, 0.00, 216.50),
    ('bank-transaction/12', DATE '2026-09-05', 'M000463', 'BANKTRANSACTION', 'bank-transaction/12', 'bank', '090', NULL, NULL, 0.00, -216.50),
    ('bank-transaction/13', DATE '2026-09-03', 'Golf Balls | INV-0031', 'BANKTRANSACTION', 'bank-transaction/13', 'control', '610', 'City Agency', NULL, 0.00, -104.40),
    ('bank-transaction/13', DATE '2026-09-03', 'Golf Balls | INV-0031', 'BANKTRANSACTION', 'bank-transaction/13', 'bank', '090', NULL, NULL, 0.00, 104.40),
    ('bank-transaction/14', DATE '2026-09-03', NULL, 'BANKTRANSACTION', 'bank-transaction/14', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/14', DATE '2026-09-03', NULL, 'BANKTRANSACTION', 'bank-transaction/14', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/14', DATE '2026-09-03', NULL, 'BANKTRANSACTION', 'bank-transaction/14', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/15', DATE '2026-09-02', NULL, 'BANKTRANSACTION', 'bank-transaction/15', 'coded', '453', '7-Eleven', 'INPUT', 0.30, 3.70),
    ('bank-transaction/15', DATE '2026-09-02', NULL, 'BANKTRANSACTION', 'bank-transaction/15', 'tax', '820', NULL, NULL, 0.00, 0.30),
    ('bank-transaction/15', DATE '2026-09-02', NULL, 'BANKTRANSACTION', 'bank-transaction/15', 'bank', '090', NULL, NULL, 0.00, -4.00),
    ('bank-transaction/16', DATE '2026-08-31', NULL, 'BANKTRANSACTION', 'bank-transaction/16', 'coded', '453', '7-Eleven', 'INPUT', 0.42, 5.08),
    ('bank-transaction/16', DATE '2026-08-31', NULL, 'BANKTRANSACTION', 'bank-transaction/16', 'tax', '820', NULL, NULL, 0.00, 0.42),
    ('bank-transaction/16', DATE '2026-08-31', NULL, 'BANKTRANSACTION', 'bank-transaction/16', 'bank', '090', NULL, NULL, 0.00, -5.50),
    ('bank-transaction/17', DATE '2026-08-31', 'M000456', 'BANKTRANSACTION', 'bank-transaction/17', 'control', '800', 'MCO Cleaning Services', NULL, 0.00, 216.50),
    ('bank-transaction/17', DATE '2026-08-31', 'M000456', 'BANKTRANSACTION', 'bank-transaction/17', 'bank', '090', NULL, NULL, 0.00, -216.50),
    ('bank-transaction/18', DATE '2026-08-31', NULL, 'BANKTRANSACTION', 'bank-transaction/18', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/18', DATE '2026-08-31', NULL, 'BANKTRANSACTION', 'bank-transaction/18', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/18', DATE '2026-08-31', NULL, 'BANKTRANSACTION', 'bank-transaction/18', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/19', DATE '2026-08-30', NULL, 'BANKTRANSACTION', 'bank-transaction/19', 'coded', '453', '7-Eleven', 'INPUT', 0.95, 11.55),
    ('bank-transaction/19', DATE '2026-08-30', NULL, 'BANKTRANSACTION', 'bank-transaction/19', 'tax', '820', NULL, NULL, 0.00, 0.95),
    ('bank-transaction/19', DATE '2026-08-30', NULL, 'BANKTRANSACTION', 'bank-transaction/19', 'bank', '090', NULL, NULL, 0.00, -12.50),
    ('bank-transaction/20', DATE '2026-08-30', 'M000442', 'BANKTRANSACTION', 'bank-transaction/20', 'control', '800', 'MCO Cleaning Services', NULL, 0.00, 216.50),
    ('bank-transaction/20', DATE '2026-08-30', 'M000442', 'BANKTRANSACTION', 'bank-transaction/20', 'bank', '090', NULL, NULL, 0.00, -216.50),
    ('bank-transaction/21', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/21', 'coded', '453', '7-Eleven', 'INPUT', 0.46, 5.54),
    ('bank-transaction/21', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/21', 'tax', '820', NULL, NULL, 0.00, 0.46),
    ('bank-transaction/21', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/21', 'bank', '090', NULL, NULL, 0.00, -6.00),
    ('bank-transaction/22', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/22', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/22', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/22', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/22', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/22', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/23', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/23', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/23', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/23', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/23', DATE '2026-08-26', NULL, 'BANKTRANSACTION', 'bank-transaction/23', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/24', DATE '2026-08-24', NULL, 'BANKTRANSACTION', 'bank-transaction/24', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/24', DATE '2026-08-24', NULL, 'BANKTRANSACTION', 'bank-transaction/24', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/24', DATE '2026-08-24', NULL, 'BANKTRANSACTION', 'bank-transaction/24', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/25', DATE '2026-08-23', 'M000435', 'BANKTRANSACTION', 'bank-transaction/25', 'control', '800', 'MCO Cleaning Services', NULL, 0.00, 216.50),
    ('bank-transaction/25', DATE '2026-08-23', 'M000435', 'BANKTRANSACTION', 'bank-transaction/25', 'bank', '090', NULL, NULL, 0.00, -216.50),
    ('bank-transaction/26', DATE '2026-08-22', NULL, 'BANKTRANSACTION', 'bank-transaction/26', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/26', DATE '2026-08-22', NULL, 'BANKTRANSACTION', 'bank-transaction/26', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/26', DATE '2026-08-22', NULL, 'BANKTRANSACTION', 'bank-transaction/26', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/27', DATE '2026-08-21', NULL, 'BANKTRANSACTION', 'bank-transaction/27', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/27', DATE '2026-08-21', NULL, 'BANKTRANSACTION', 'bank-transaction/27', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/27', DATE '2026-08-21', NULL, 'BANKTRANSACTION', 'bank-transaction/27', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/28', DATE '2026-08-21', '710', 'BANKTRANSACTION', 'bank-transaction/28', 'control', '800', 'ABC Furniture', NULL, 0.00, 1000.00),
    ('bank-transaction/28', DATE '2026-08-21', '710', 'BANKTRANSACTION', 'bank-transaction/28', 'bank', '090', NULL, NULL, 0.00, -1000.00),
    ('bank-transaction/29', DATE '2026-08-21', 'Monthly Support | INV-0021', 'BANKTRANSACTION', 'bank-transaction/29', 'control', '610', 'Rex Media Group', NULL, 0.00, -541.25),
    ('bank-transaction/29', DATE '2026-08-21', 'Monthly Support | INV-0021', 'BANKTRANSACTION', 'bank-transaction/29', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/30', DATE '2026-08-21', 'Monthly Support | INV-0020', 'BANKTRANSACTION', 'bank-transaction/30', 'control', '610', 'Port & Philip Freight', NULL, 0.00, -541.25),
    ('bank-transaction/30', DATE '2026-08-21', 'Monthly Support | INV-0020', 'BANKTRANSACTION', 'bank-transaction/30', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/31', DATE '2026-08-21', NULL, 'BANKTRANSACTION', 'bank-transaction/31', 'control', '800', 'PowerDirect', NULL, 0.00, 1363.92),
    ('bank-transaction/31', DATE '2026-08-21', NULL, 'BANKTRANSACTION', 'bank-transaction/31', 'bank', '090', NULL, NULL, 0.00, -1363.92),
    ('bank-transaction/32', DATE '2026-08-21', 'Monthly Support | INV-0019', 'BANKTRANSACTION', 'bank-transaction/32', 'control', '610', 'Young Bros Transport', NULL, 0.00, -541.25),
    ('bank-transaction/32', DATE '2026-08-21', 'Monthly Support | INV-0019', 'BANKTRANSACTION', 'bank-transaction/32', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/33', DATE '2026-08-21', 'Dep', 'BANKTRANSACTION', 'bank-transaction/33', 'control', '800', 'Carlton Functions', NULL, 0.00, 1500.00),
    ('bank-transaction/33', DATE '2026-08-21', 'Dep', 'BANKTRANSACTION', 'bank-transaction/33', 'bank', '090', NULL, NULL, 0.00, -1500.00),
    ('bank-transaction/34', DATE '2026-08-21', 'Monthly Support | INV-0018', 'BANKTRANSACTION', 'bank-transaction/34', 'control', '610', 'Hamilton Smith Ltd', NULL, 0.00, -541.25),
    ('bank-transaction/34', DATE '2026-08-21', 'Monthly Support | INV-0018', 'BANKTRANSACTION', 'bank-transaction/34', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/35', DATE '2026-08-21', '08-4123', 'BANKTRANSACTION', 'bank-transaction/35', 'control', '800', 'Hoyt Productions', NULL, 0.00, 5953.75),
    ('bank-transaction/35', DATE '2026-08-21', '08-4123', 'BANKTRANSACTION', 'bank-transaction/35', 'bank', '090', NULL, NULL, 0.00, -5953.75),
    ('bank-transaction/36', DATE '2026-08-20', NULL, 'BANKTRANSACTION', 'bank-transaction/36', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/36', DATE '2026-08-20', NULL, 'BANKTRANSACTION', 'bank-transaction/36', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/36', DATE '2026-08-20', NULL, 'BANKTRANSACTION', 'bank-transaction/36', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/37', DATE '2026-08-20', NULL, 'BANKTRANSACTION', 'bank-transaction/37', 'coded', '473', '24 Locks', 'INPUT', 5.30, 64.20),
    ('bank-transaction/37', DATE '2026-08-20', NULL, 'BANKTRANSACTION', 'bank-transaction/37', 'tax', '820', NULL, NULL, 0.00, 5.30),
    ('bank-transaction/37', DATE '2026-08-20', NULL, 'BANKTRANSACTION', 'bank-transaction/37', 'bank', '090', NULL, NULL, 0.00, -69.50),
    ('bank-transaction/38', DATE '2026-08-20', NULL, 'BANKTRANSACTION', 'bank-transaction/38', 'coded', '420', 'Berry Brew', 'NONE', 0.00, 22.00),
    ('bank-transaction/38', DATE '2026-08-20', NULL, 'BANKTRANSACTION', 'bank-transaction/38', 'bank', '090', NULL, NULL, 0.00, -22.00),
    ('bank-transaction/39', DATE '2026-08-19', NULL, 'BANKTRANSACTION', 'bank-transaction/39', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/39', DATE '2026-08-19', NULL, 'BANKTRANSACTION', 'bank-transaction/39', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/39', DATE '2026-08-19', NULL, 'BANKTRANSACTION', 'bank-transaction/39', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/40', DATE '2026-08-18', NULL, 'BANKTRANSACTION', 'bank-transaction/40', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/40', DATE '2026-08-18', NULL, 'BANKTRANSACTION', 'bank-transaction/40', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/40', DATE '2026-08-18', NULL, 'BANKTRANSACTION', 'bank-transaction/40', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/41', DATE '2026-08-16', NULL, 'BANKTRANSACTION', 'bank-transaction/41', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/41', DATE '2026-08-16', NULL, 'BANKTRANSACTION', 'bank-transaction/41', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/41', DATE '2026-08-16', NULL, 'BANKTRANSACTION', 'bank-transaction/41', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/42', DATE '2026-08-15', NULL, 'BANKTRANSACTION', 'bank-transaction/42', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/42', DATE '2026-08-15', NULL, 'BANKTRANSACTION', 'bank-transaction/42', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/42', DATE '2026-08-15', NULL, 'BANKTRANSACTION', 'bank-transaction/42', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/43', DATE '2026-08-14', NULL, 'BANKTRANSACTION', 'bank-transaction/43', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/43', DATE '2026-08-14', NULL, 'BANKTRANSACTION', 'bank-transaction/43', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/43', DATE '2026-08-14', NULL, 'BANKTRANSACTION', 'bank-transaction/43', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/44', DATE '2026-08-14', NULL, 'BANKTRANSACTION', 'bank-transaction/44', 'coded', '461', 'Office Supplies Company', 'INPUT', 3.75, 45.45),
    ('bank-transaction/44', DATE '2026-08-14', NULL, 'BANKTRANSACTION', 'bank-transaction/44', 'tax', '820', NULL, NULL, 0.00, 3.75),
    ('bank-transaction/44', DATE '2026-08-14', NULL, 'BANKTRANSACTION', 'bank-transaction/44', 'bank', '090', NULL, NULL, 0.00, -49.20),
    ('bank-transaction/45', DATE '2026-08-13', NULL, 'BANKTRANSACTION', 'bank-transaction/45', 'coded', '493', 'Passing Places Parking', 'INPUT', 0.91, 11.09),
    ('bank-transaction/45', DATE '2026-08-13', NULL, 'BANKTRANSACTION', 'bank-transaction/45', 'tax', '820', NULL, NULL, 0.00, 0.91),
    ('bank-transaction/45', DATE '2026-08-13', NULL, 'BANKTRANSACTION', 'bank-transaction/45', 'bank', '090', NULL, NULL, 0.00, -12.00),
    ('bank-transaction/46', DATE '2026-08-12', NULL, 'BANKTRANSACTION', 'bank-transaction/46', 'coded', '449', 'Melrose Parking', 'INPUT', 11.32, 137.18),
    ('bank-transaction/46', DATE '2026-08-12', NULL, 'BANKTRANSACTION', 'bank-transaction/46', 'tax', '820', NULL, NULL, 0.00, 11.32),
    ('bank-transaction/46', DATE '2026-08-12', NULL, 'BANKTRANSACTION', 'bank-transaction/46', 'bank', '090', NULL, NULL, 0.00, -148.50),
    ('bank-transaction/47', DATE '2026-08-11', NULL, 'BANKTRANSACTION', 'bank-transaction/47', 'coded', '453', 'Woolworths Market', 'INPUT', 2.60, 31.50),
    ('bank-transaction/47', DATE '2026-08-11', NULL, 'BANKTRANSACTION', 'bank-transaction/47', 'tax', '820', NULL, NULL, 0.00, 2.60),
    ('bank-transaction/47', DATE '2026-08-11', NULL, 'BANKTRANSACTION', 'bank-transaction/47', 'bank', '090', NULL, NULL, 0.00, -34.10),
    ('bank-transaction/48', DATE '2026-08-11', 'Training | INV-0013', 'BANKTRANSACTION', 'bank-transaction/48', 'control', '610', 'Boom FM', NULL, 0.00, -1082.50),
    ('bank-transaction/48', DATE '2026-08-11', 'Training | INV-0013', 'BANKTRANSACTION', 'bank-transaction/48', 'bank', '090', NULL, NULL, 0.00, 1082.50),
    ('bank-transaction/49', DATE '2026-09-07', NULL, 'BANKTRANSACTION', 'bank-transaction/49', 'control', '800', 'Gateway Motors', NULL, 0.00, 411.35),
    ('bank-transaction/49', DATE '2026-09-07', NULL, 'BANKTRANSACTION', 'bank-transaction/49', 'bank', '090', NULL, NULL, 0.00, -411.35),
    ('bank-transaction/50', DATE '2026-08-31', 'RENT', 'BANKTRANSACTION', 'bank-transaction/50', 'control', '800', 'Truxton Property Management', NULL, 0.00, 1181.25),
    ('bank-transaction/50', DATE '2026-08-31', 'RENT', 'BANKTRANSACTION', 'bank-transaction/50', 'bank', '090', NULL, NULL, 0.00, -1181.25),
    ('bank-transaction/51', DATE '2026-08-11', 'Yr Ref W08-143 | INV-0016', 'BANKTRANSACTION', 'bank-transaction/51', 'control', '610', 'DIISR - Small Business Services', NULL, 0.00, -568.31),
    ('bank-transaction/51', DATE '2026-08-11', 'Yr Ref W08-143 | INV-0016', 'BANKTRANSACTION', 'bank-transaction/51', 'bank', '090', NULL, NULL, 0.00, 568.31),
    ('bank-transaction/52', DATE '2026-08-11', 'Yr Ref W08-143 | INV-0022', 'BANKTRANSACTION', 'bank-transaction/52', 'control', '610', 'DIISR - Small Business Services', NULL, 0.00, -216.50),
    ('bank-transaction/52', DATE '2026-08-11', 'Yr Ref W08-143 | INV-0022', 'BANKTRANSACTION', 'bank-transaction/52', 'bank', '090', NULL, NULL, 0.00, 216.50),
    ('bank-transaction/53', DATE '2026-08-11', 'P/O CRM08-12 | INV-0009', 'BANKTRANSACTION', 'bank-transaction/53', 'control', '610', 'Ridgeway University', NULL, 0.00, -6187.50),
    ('bank-transaction/53', DATE '2026-08-11', 'P/O CRM08-12 | INV-0009', 'BANKTRANSACTION', 'bank-transaction/53', 'bank', '090', NULL, NULL, 0.00, 6187.50),
    ('bank-transaction/54', DATE '2026-08-11', NULL, 'BANKTRANSACTION', 'bank-transaction/54', 'coded', '453', 'Orlena Greenville', 'INPUT', 2.25, 27.25),
    ('bank-transaction/54', DATE '2026-08-11', NULL, 'BANKTRANSACTION', 'bank-transaction/54', 'tax', '820', NULL, NULL, 0.00, 2.25),
    ('bank-transaction/54', DATE '2026-08-11', NULL, 'BANKTRANSACTION', 'bank-transaction/54', 'bank', '090', NULL, NULL, 0.00, -29.50),
    ('bank-transaction/55', DATE '2026-08-11', 'Portal Proj | INV-0011', 'BANKTRANSACTION', 'bank-transaction/55', 'control', '610', 'Petrie McLoud Watson & Associates', NULL, 0.00, -1407.25),
    ('bank-transaction/55', DATE '2026-08-11', 'Portal Proj | INV-0011', 'BANKTRANSACTION', 'bank-transaction/55', 'bank', '090', NULL, NULL, 0.00, 1407.25),
    ('bank-transaction/56', DATE '2026-08-11', NULL, 'BANKTRANSACTION', 'bank-transaction/56', 'control', '801', 'Orlena Greenville', NULL, 0.00, 29.50),
    ('bank-transaction/56', DATE '2026-08-11', NULL, 'BANKTRANSACTION', 'bank-transaction/56', 'bank', '090', NULL, NULL, 0.00, -29.50),
    ('bank-transaction/57', DATE '2026-08-08', 'AP', 'BANKTRANSACTION', 'bank-transaction/57', 'control', '800', 'Xero', NULL, 0.00, 31.39),
    ('bank-transaction/57', DATE '2026-08-08', 'AP', 'BANKTRANSACTION', 'bank-transaction/57', 'bank', '090', NULL, NULL, 0.00, -31.39),
    ('bank-transaction/58', DATE '2026-08-01', 'OG laptop', 'BANKTRANSACTION', 'bank-transaction/58', 'control', '800', 'PC Complete', NULL, 0.00, 270.63),
    ('bank-transaction/58', DATE '2026-08-01', 'OG laptop', 'BANKTRANSACTION', 'bank-transaction/58', 'bank', '090', NULL, NULL, 0.00, -270.63),
    ('bank-transaction/59', DATE '2026-08-01', 'Fee', 'BANKTRANSACTION', 'bank-transaction/59', 'coded', '404', 'Ridgeway Bank', 'NONE', 0.00, 15.00),
    ('bank-transaction/59', DATE '2026-08-01', 'Fee', 'BANKTRANSACTION', 'bank-transaction/59', 'bank', '090', NULL, NULL, 0.00, -15.00),
    ('bank-transaction/60', DATE '2026-07-30', '9781', 'BANKTRANSACTION', 'bank-transaction/60', 'control', '800', 'Net Connect', NULL, 0.00, 1463.88),
    ('bank-transaction/60', DATE '2026-07-30', '9781', 'BANKTRANSACTION', 'bank-transaction/60', 'bank', '090', NULL, NULL, 0.00, -1463.88),
    ('bank-transaction/61', DATE '2026-07-27', '945-OCon', 'BANKTRANSACTION', 'bank-transaction/61', 'control', '800', 'Central Copiers', NULL, 0.00, 900.00),
    ('bank-transaction/61', DATE '2026-07-27', '945-OCon', 'BANKTRANSACTION', 'bank-transaction/61', 'bank', '090', NULL, NULL, 0.00, -900.00),
    ('bank-transaction/62', DATE '2026-07-27', '5679', 'BANKTRANSACTION', 'bank-transaction/62', 'control', '800', 'MCO Cleaning Services', NULL, 0.00, 119.08),
    ('bank-transaction/62', DATE '2026-07-27', '5679', 'BANKTRANSACTION', 'bank-transaction/62', 'bank', '090', NULL, NULL, 0.00, -119.08),
    ('bank-transaction/63', DATE '2026-07-27', NULL, 'BANKTRANSACTION', 'bank-transaction/63', 'control', '801', 'Orlena Greenville', NULL, 0.00, 34.90),
    ('bank-transaction/63', DATE '2026-07-27', NULL, 'BANKTRANSACTION', 'bank-transaction/63', 'bank', '090', NULL, NULL, 0.00, -34.90),
    ('bank-transaction/64', DATE '2026-07-27', 'AP', 'BANKTRANSACTION', 'bank-transaction/64', 'control', '800', 'Swanston Security', NULL, 0.00, 34.10),
    ('bank-transaction/64', DATE '2026-07-27', 'AP', 'BANKTRANSACTION', 'bank-transaction/64', 'bank', '090', NULL, NULL, 0.00, -34.10),
    ('bank-transaction/65', DATE '2026-07-25', 'Workshop | INV-0007', 'BANKTRANSACTION', 'bank-transaction/65', 'control', '610', 'City Agency', NULL, 0.00, -593.23),
    ('bank-transaction/65', DATE '2026-07-25', 'Workshop | INV-0007', 'BANKTRANSACTION', 'bank-transaction/65', 'bank', '090', NULL, NULL, 0.00, 593.23),
    ('bank-transaction/66', DATE '2026-07-21', 'RENT', 'BANKTRANSACTION', 'bank-transaction/66', 'control', '800', 'Truxton Property Management', NULL, 0.00, 1181.25),
    ('bank-transaction/66', DATE '2026-07-21', 'RENT', 'BANKTRANSACTION', 'bank-transaction/66', 'bank', '090', NULL, NULL, 0.00, -1181.25),
    ('bank-transaction/67', DATE '2026-07-21', 'Rpt', 'BANKTRANSACTION', 'bank-transaction/67', 'control', '800', 'Net Connect', NULL, 0.00, 44.92),
    ('bank-transaction/67', DATE '2026-07-21', 'Rpt', 'BANKTRANSACTION', 'bank-transaction/67', 'bank', '090', NULL, NULL, 0.00, -44.92),
    ('bank-transaction/68', DATE '2026-07-21', NULL, 'BANKTRANSACTION', 'bank-transaction/68', 'control', '800', 'PC Complete', NULL, 0.00, 1682.74),
    ('bank-transaction/68', DATE '2026-07-21', NULL, 'BANKTRANSACTION', 'bank-transaction/68', 'bank', '090', NULL, NULL, 0.00, -1682.74),
    ('bank-transaction/69', DATE '2026-07-21', NULL, 'BANKTRANSACTION', 'bank-transaction/69', 'control', '610', 'Bank West', NULL, 0.00, -1299.00),
    ('bank-transaction/69', DATE '2026-07-21', NULL, 'BANKTRANSACTION', 'bank-transaction/69', 'bank', '090', NULL, NULL, 0.00, 1299.00),
    ('bank-transaction/70', DATE '2026-07-21', 'Rpt', 'BANKTRANSACTION', 'bank-transaction/70', 'control', '800', 'PowerDirect', NULL, 0.00, 119.08),
    ('bank-transaction/70', DATE '2026-07-21', 'Rpt', 'BANKTRANSACTION', 'bank-transaction/70', 'bank', '090', NULL, NULL, 0.00, -119.08),
    ('bank-transaction/71', DATE '2026-07-20', NULL, 'BANKTRANSACTION', 'bank-transaction/71', 'control', '610', 'Hamilton Smith Ltd', NULL, 0.00, -541.25),
    ('bank-transaction/71', DATE '2026-07-20', NULL, 'BANKTRANSACTION', 'bank-transaction/71', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/72', DATE '2026-07-20', NULL, 'BANKTRANSACTION', 'bank-transaction/72', 'control', '610', 'Rex Media Group', NULL, 0.00, -541.25),
    ('bank-transaction/72', DATE '2026-07-20', NULL, 'BANKTRANSACTION', 'bank-transaction/72', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/73', DATE '2026-07-20', NULL, 'BANKTRANSACTION', 'bank-transaction/73', 'control', '610', 'Port & Philip Freight', NULL, 0.00, -541.25),
    ('bank-transaction/73', DATE '2026-07-20', NULL, 'BANKTRANSACTION', 'bank-transaction/73', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/74', DATE '2026-07-20', NULL, 'BANKTRANSACTION', 'bank-transaction/74', 'control', '610', 'Young Bros Transport', NULL, 0.00, -541.25),
    ('bank-transaction/74', DATE '2026-07-20', NULL, 'BANKTRANSACTION', 'bank-transaction/74', 'bank', '090', NULL, NULL, 0.00, 541.25),
    ('bank-transaction/75', DATE '2026-07-20', 'Chq 409', 'BANKTRANSACTION', 'bank-transaction/75', 'coded', '449', 'Melrose Parking', 'INPUT', 11.32, 137.18),
    ('bank-transaction/75', DATE '2026-07-20', 'Chq 409', 'BANKTRANSACTION', 'bank-transaction/75', 'tax', '820', NULL, NULL, 0.00, 11.32),
    ('bank-transaction/75', DATE '2026-07-20', 'Chq 409', 'BANKTRANSACTION', 'bank-transaction/75', 'bank', '090', NULL, NULL, 0.00, -148.50),
    ('bank-transaction/76', DATE '2026-07-18', NULL, 'BANKTRANSACTION', 'bank-transaction/76', 'coded', '420', 'Berry Brew', 'NONE', 0.00, 15.60),
    ('bank-transaction/76', DATE '2026-07-18', NULL, 'BANKTRANSACTION', 'bank-transaction/76', 'bank', '090', NULL, NULL, 0.00, -15.60),
    ('bank-transaction/77', DATE '2026-07-15', 'Gift', 'BANKTRANSACTION', 'bank-transaction/77', 'coded', '429', 'Brunswick Petals', 'INPUT', 3.81, 46.19),
    ('bank-transaction/77', DATE '2026-07-15', 'Gift', 'BANKTRANSACTION', 'bank-transaction/77', 'tax', '820', NULL, NULL, 0.00, 3.81),
    ('bank-transaction/77', DATE '2026-07-15', 'Gift', 'BANKTRANSACTION', 'bank-transaction/77', 'bank', '090', NULL, NULL, 0.00, -50.00),
    ('bank-transaction/78', DATE '2026-07-11', NULL, 'BANKTRANSACTION', 'bank-transaction/78', 'coded', '453', 'Woolworths Market', 'INPUT', 4.97, 60.23),
    ('bank-transaction/78', DATE '2026-07-11', NULL, 'BANKTRANSACTION', 'bank-transaction/78', 'tax', '820', NULL, NULL, 0.00, 4.97),
    ('bank-transaction/78', DATE '2026-07-11', NULL, 'BANKTRANSACTION', 'bank-transaction/78', 'bank', '090', NULL, NULL, 0.00, -65.20),
    ('bank-transaction/79', DATE '2026-07-08', NULL, 'BANKTRANSACTION', 'bank-transaction/79', 'coded', '420', 'Espresso 31', 'NONE', 0.00, 16.00),
    ('bank-transaction/79', DATE '2026-07-08', NULL, 'BANKTRANSACTION', 'bank-transaction/79', 'bank', '090', NULL, NULL, 0.00, -16.00),
    ('bank-transaction/80', DATE '2026-07-08', NULL, 'BANKTRANSACTION', 'bank-transaction/80', 'control', '800', 'Xero', NULL, 0.00, 31.39),
    ('bank-transaction/80', DATE '2026-07-08', NULL, 'BANKTRANSACTION', 'bank-transaction/80', 'bank', '090', NULL, NULL, 0.00, -31.39),
    ('bank-transaction/81', DATE '2026-07-06', 'Eft', 'BANKTRANSACTION', 'bank-transaction/81', 'coded', '461', 'Office Supplies Company', 'INPUT', 1.79, 21.71),
    ('bank-transaction/81', DATE '2026-07-06', 'Eft', 'BANKTRANSACTION', 'bank-transaction/81', 'tax', '820', NULL, NULL, 0.00, 1.79),
    ('bank-transaction/81', DATE '2026-07-06', 'Eft', 'BANKTRANSACTION', 'bank-transaction/81', 'bank', '090', NULL, NULL, 0.00, -23.50),
    ('bank-transaction/82', DATE '2026-07-01', 'Fee', 'BANKTRANSACTION', 'bank-transaction/82', 'coded', '404', 'Ridgeway Bank', 'NONE', 0.00, 15.00),
    ('bank-transaction/82', DATE '2026-07-01', 'Fee', 'BANKTRANSACTION', 'bank-transaction/82', 'bank', '090', NULL, NULL, 0.00, -15.00);

-- ---------------------------------------------------------------------------
-- 1. Invoice numbering.
-- 
-- invoices has UNIQUE(organisation_id, invoice_number), keyed on the assumption
-- that a document number identifies a document.  Xero's captured bills break
-- that assumption: three bills are numbered AP, three RENT, six Rpt, and several
-- are numbered not at all.  00023 never wrote an invoices row, so the constraint
-- has never been exercised; this is the first migration to write one.  The
-- constraint is replaced by the same uniqueness limited to the side that really
-- has it -- sales invoices -- so the capture lands unedited rather than being
-- renumbered to fit.  The Down half puts the constraint back.
-- ---------------------------------------------------------------------------
ALTER TABLE invoices DROP CONSTRAINT invoices_organisation_id_invoice_number_key;
CREATE UNIQUE INDEX invoices_org_accrec_number_key
    ON invoices (organisation_id, invoice_number)
 WHERE type = 'ACCREC' AND invoice_number IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 2. The contacts every document names.
-- 
-- contacts.csv holds 51 contacts; 00023 wrote 21 of them (the ones its bank
-- transactions named).  All 51 are written here, so the ages the documents
-- produce can be grouped by contact.  The 21 keep their ids and gain their
-- captured type and email address; the other 30 are new.  Neither a phone number
-- nor a town is written: the schema holds them in contact_phones and
-- contact_addresses, both of which require a type, and the capture has no type to
-- give -- see docs/xero-import-00024.md.
-- ---------------------------------------------------------------------------
INSERT INTO contacts (contact_id, organisation_id, name, is_customer, is_supplier,
                      email_address)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/contact/' || c.name),
       '6823b27b-c48f-4099-bb27-4202a4f496a2',
       c.name, c.is_customer, c.is_supplier, c.email
  FROM xr_contact c
ON CONFLICT (contact_id) DO UPDATE
   SET name          = EXCLUDED.name,
       is_customer   = EXCLUDED.is_customer,
       is_supplier   = EXCLUDED.is_supplier,
       email_address = EXCLUDED.email_address,
       updated_date_utc = now();

-- ---------------------------------------------------------------------------
-- 3. The invoices and the bills.
-- 
-- One row per captured document, in the state the capture holds it: DRAFT and
-- VOIDED and DELETED documents are written too, because Xero holds them and the
-- status is what keeps them out of the ledger.  Step 5 posts only the ones Xero
-- posted.
-- 
-- invoice_number is the captured number.  A bill Xero numbers blank stays NULL:
-- the capture has no number for it and none is invented.
-- ---------------------------------------------------------------------------
INSERT INTO invoices (invoice_id, organisation_id, type, contact_id, invoice_number,
                      reference, currency_code, status, line_amount_types, date,
                      due_date, sub_total, total_tax, total, amount_paid, amount_due)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || d.doc_key),
       '6823b27b-c48f-4099-bb27-4202a4f496a2', d.kind,
       (SELECT c.contact_id FROM contacts c
         WHERE c.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND c.name = d.contact),
       d.doc_number, d.ref, d.currency, d.status, 'Exclusive', d.doc_date, d.due_date,
       d.sub_total, d.total_tax, d.total, d.amount_paid, d.amount_due
  FROM xr_doc d;

INSERT INTO invoice_line_items (line_item_id, invoice_id, sort_order, description,
                                quantity, unit_amount, account_code, account_id,
                                tax_type, tax_amount, line_amount)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || l.doc_key || '/line/' || l.ord::text),
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || l.doc_key),
       l.ord, l.description, l.quantity, l.unit_amount, l.account_code,
       (SELECT a.account_id FROM accounts a
         WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = l.account_code),
       l.tax_type, l.tax_amount, l.line_amount
  FROM xr_doc_line l;

-- ---------------------------------------------------------------------------
-- 4. The credit notes, the payments and the expense claims.
-- 
-- The five credit notes 00023 could not hold.  ACCRECCREDIT notes are credits
-- against a sale (they credit 610 and 820); ACCPAYCREDIT notes are credits a
-- supplier gave (they debit 800 and 820).  Their tax split is the sum of the
-- captured lines, and it equals the captured total on all five.
-- 
-- payments.csv is written as payments rows: they record which document each
-- settlement cleared.  They carry no posting of their own -- the money side of
-- each is the bank transaction the capture already holds, named "Payment: ...".
-- A payment whose document number is blank or repeated across that contact's
-- bills is written with a NULL invoice_id rather than guessed onto one.
-- 
-- expense_claims is the last source document the reports read.  Xero's Aged
-- Payables Summary prints an "Expense Claims" block under the trade payables
-- (docs/xero-reference/aged-payables-summary.txt: Total 8,502.71 = 8,386.76 +
-- 115.95), and internal/repository/report.go::AgedExpenseClaims builds it from
-- this table -- status SUBMITTED/AUTHORISED, amount_due > 0, aged from
-- COALESCE(payment_due_date, reporting_date).  So the claims have to exist as
-- rows and not only as ledger lines.
-- 
-- payment_due_date is left NULL: expense-claims.csv carries no due date and none
-- is invented.  reporting_date is the claim's own date, which is the date the
-- 801 posting is dated with, so a claim lands in the same ageing column as its
-- ledger line.
-- 
-- expense_claims.user_id points at users, and the renderer labels the row with
-- the user's first_name + last_name (falling back to the email) -- with no user
-- at all it prints "Unassigned expense claim".  The claimants the CSV names are
-- therefore created as users.  Their names are the CSV's own `contact` value
-- split on the first space, so the label prints the captured name back.  users
-- demands a UNIQUE NOT NULL email and a NOT NULL password_hash, neither of which
-- any captured file holds: the email is derived from that same name and the
-- password_hash is "!", which is not a bcrypt hash and so cannot be logged in
-- with.  Both derived cells are named in docs/xero-import-00024.md; they are the
-- only cells in this migration that are not transcribed from a captured file.
-- 
-- No organisation_users row is written for either user: the claim's own
-- organisation_id is what scopes them, and a membership row would add them to the
-- members list, which no captured file shows.
-- ---------------------------------------------------------------------------
INSERT INTO credit_notes (credit_note_id, organisation_id, type, contact_id,
                          credit_note_number, status, line_amount_types, date,
                          sub_total, total_tax, total, remaining_credit)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || n.cn_key),
       '6823b27b-c48f-4099-bb27-4202a4f496a2', n.cn_type,
       (SELECT c.contact_id FROM contacts c
         WHERE c.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND c.name = n.contact),
       n.cn_number, n.cn_status, 'Exclusive', n.cn_date,
       n.sub_total, n.total_tax, n.total, n.remaining
  FROM xr_credit_note n;

INSERT INTO credit_note_line_items (line_item_id, credit_note_id, sort_order,
                                    description, quantity, unit_amount, account_code,
                                    tax_type, tax_amount, line_amount)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || l.cn_key || '/line/' || l.ord::text),
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || l.cn_key),
       l.ord, l.description, l.quantity, l.unit_amount, l.account_code,
       l.tax_type, l.tax_amount, l.line_amount
  FROM xr_credit_note_line l;

INSERT INTO payments (payment_id, organisation_id, invoice_id, credit_note_id,
                      account_id, payment_type, status, date, amount, is_reconciled)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/payment/' || p.n::text),
       '6823b27b-c48f-4099-bb27-4202a4f496a2',
       -- the document the payment names, but only when that name picks one out;
       -- a blank or repeated document number leaves this NULL rather than
       -- attaching the payment to the wrong document.
       (SELECT CASE WHEN count(*) = 1 THEN (array_agg(i.invoice_id))[1] END
          FROM invoices i JOIN contacts c ON c.contact_id = i.contact_id
         WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
           AND c.name = p.contact AND i.type = p.doc_type
           AND i.invoice_number = NULLIF(p.doc_number, '')),
       (SELECT CASE WHEN count(*) = 1 THEN (array_agg(n.credit_note_id))[1] END
          FROM credit_notes n JOIN contacts c ON c.contact_id = n.contact_id
         WHERE n.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
           AND c.name = p.contact
           AND n.credit_note_number = NULLIF(p.doc_number, '')),
       (SELECT a.account_id FROM accounts a
         WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = p.account_code),
       CASE WHEN p.doc_type = 'ACCREC' THEN 'ACCRECPAYMENT' ELSE 'ACCPAYPAYMENT' END,
       'AUTHORISED', p.pay_date, p.amount, TRUE
  FROM xr_payment p;
INSERT INTO users (user_id, email, password_hash, first_name, last_name)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/user/' || c.contact),
       lower(replace(c.contact, ' ', '.')) || '@demo.local', '!',
       split_part(c.contact, ' ', 1),
       nullif(substr(c.contact, position(' ' IN c.contact) + 1), '')
  FROM (SELECT DISTINCT contact FROM xr_claim) c;

INSERT INTO expense_claims (expense_claim_id, organisation_id, user_id, status,
                            payment_due_date, reporting_date, total, amount_due,
                            amount_paid)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/' || c.claim_key),
       '6823b27b-c48f-4099-bb27-4202a4f496a2',
       (SELECT u.user_id FROM users u
         WHERE u.email = lower(replace(c.contact, ' ', '.')) || '@demo.local'),
       c.status, NULL, c.claim_date, c.total, c.amount_due, c.paid_amount
  FROM xr_claim c;

-- ---------------------------------------------------------------------------
-- 5. The 82 coded bank transactions and the ledger they post to.
-- 
-- 00023 wrote the first 48 and posted every one of them to the account the CSV
-- records.  For the 26 of those that settle a bill or an invoice, that account is
-- the account of the document the money paid, not the control account the ledger
-- clears -- so 00023 could not reach Xero's Trial Balance, and its two bank-ledger
-- legs per transaction, with no tax leg, could not either.  Both are superseded:
-- the 82 rows are written from scratch and each one is posted by the rule in
-- reconciliation.md section 2.
-- 
-- The deletes name fixed ids -- the 48 journal ids 00023 built from
-- "goxero/xero-ref/bank-transaction/<n>/journal", the 82 line-item ids, the 82
-- bank transaction ids, and 00023's opening-balance journal id.  They do not name
-- a date range or a source_type, so a bank transaction another session added to
-- this organisation is not touched.
-- ---------------------------------------------------------------------------
DELETE FROM bank_transaction_line_items
 WHERE line_item_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || g::text || '/line/1') FROM generate_series(1, 82) g);

-- gl_journal_lines.journal_id is ON DELETE CASCADE, so the lines go with them.
DELETE FROM gl_journals
 WHERE journal_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || g::text || '/journal') FROM generate_series(1, 48) g)
    OR journal_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/opening-balance/journal');

DELETE FROM bank_transactions
 WHERE bank_transaction_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || g::text) FROM generate_series(1, 82) g);

INSERT INTO bank_transactions (bank_transaction_id, organisation_id, contact_id,
                               bank_account_id, type, is_reconciled, date, reference,
                               currency_code, status, line_amount_types, sub_total,
                               total_tax, total)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || b.n::text),
       '6823b27b-c48f-4099-bb27-4202a4f496a2',
       (SELECT c.contact_id FROM contacts c
         WHERE c.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND c.name = b.contact),
       (SELECT a.account_id FROM accounts a
         WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'),
       CASE WHEN b.is_payment THEN 'SPEND' WHEN b.direction = 'out' THEN 'SPEND' ELSE 'RECEIVE' END,
       TRUE, b.tx_date, b.reference, 'USD', 'AUTHORISED', 'Exclusive',
       b.sub_total, b.total_tax, b.total
  FROM xr_bank b;

-- The coded line names the account the journal posts to.  unit_amount is the
-- tax-exclusive part so that quantity x unit_amount = line_amount and
-- sub_total + total_tax = total, which is what line_amount_types = 'Exclusive'
-- asserts.  For a settlement row the captured `coded_code` -- the document's own
-- account -- is not the ledger's account, so it is recorded in the description
-- of the line that carries it in the ledger, and the ledger's account is used.
INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id,
                                         description, quantity, unit_amount,
                                         account_code, account_id, tax_type,
                                         tax_amount, line_amount)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || b.n::text || '/line/1'),
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || b.n::text),
       CASE WHEN b.is_payment AND b.coded_code <> b.leg_code
            THEN b.description || ' (Xero coded this row to ' || b.coded_code || ')'
            ELSE b.description END,
       1, b.sub_total, b.leg_code,
       (SELECT a.account_id FROM accounts a
         WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = b.leg_code),
       b.leg_tax_type, b.total_tax, b.sub_total
  FROM xr_bank b;

-- ---------------------------------------------------------------------------
-- 6. The ledger.
-- 
-- One gl_journals row per journal, one gl_journal_lines row per line, both keyed
-- by uuid_generate_v5() of the journal key so a down/up cycle reproduces them
-- exactly.  source_id points at the document the journal came from, so a journal
-- can be traced back to the invoice or bank transaction that produced it.
-- 
-- gl_journal_lines.gross_amount is net + tax, which is the convention
-- internal/repository/gl.go::postJournalLines uses.
-- 
-- The raw all-time total of net_amount is NOT forced to the trial balance's
-- 42,595.46: a trial balance nets accounts, and the ledger does not.  What the
-- assertion in section 8 checks is the pair of figures that must hold -- every
-- journal sums to zero, and total debits equal total credits.
-- ---------------------------------------------------------------------------
INSERT INTO gl_journals (journal_id, organisation_id, reference, source_type,
                         source_id, journal_date)
SELECT DISTINCT ON (g.journal_key)
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || g.journal_key),
       '6823b27b-c48f-4099-bb27-4202a4f496a2', g.reference, g.source_type,
       CASE WHEN g.source_key IS NULL THEN NULL
            WHEN g.source_type = 'BANKTRANSACTION'
            THEN uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/' || g.source_key)
            ELSE uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || g.source_key) END,
       g.jdate
  FROM xr_gl g
 ORDER BY g.journal_key, g.line_key;

INSERT INTO gl_journal_lines (line_id, journal_id, account_id, description, tax_type,
                              tax_amount, net_amount, gross_amount)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || g.journal_key || '/' || g.line_key),
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || g.journal_key),
       a.account_id, g.description, g.tax_type, g.tax_amount, g.net_amount,
       g.net_amount + g.tax_amount
  FROM xr_gl g
  JOIN accounts a ON a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
                 AND a.code = g.account_code;

SELECT setval('gl_journals_journal_number_seq',
              GREATEST((SELECT COALESCE(max(journal_number), 0) FROM gl_journals), 1));

-- ---------------------------------------------------------------------------
-- 7. The organisation's own settings.
-- 
-- Checked column by column against docs/xero-reference/org-settings.txt:
--   * country_code -- Xero says Canada (CNTRY/CA), the row said US.  Written.
--   * timezone     -- Xero says "(UTC-05:00) Eastern Time (US & Canada)", the row
--                     said UTC.  Xero's own label is written, because that is
--                     the value the settings screen stores and displays
--                     (web/src/routes/app/settings/financial/+page.svelte uses the
--                     same labels, and Eastern Time is its default).
--   * tax_number   -- Xero says "101-2-303", the row was empty.  Written.
--                     (The captured "Tax ID display name" of "Tax reg" has no
--                     column.)
--   * short_code   -- Xero says "!!6Sp3", the row said "DEMO".  Written.
--   * name, base_currency, financial_year_end_* -- already agree; not touched.
--   * legal_name   -- left alone: org-settings.txt names an organisation and a
--                     trading name, both identical to the row's name, and
--                     publishes no separate legal name to write.
-- Settings the schema cannot hold are listed in docs/xero-import-00024.md.
-- ---------------------------------------------------------------------------
UPDATE organisations
   SET country_code = 'CA',
       timezone     = '(UTC-05:00) Eastern Time (US & Canada)',
       tax_number   = '101-2-303',
       short_code   = '!!6Sp3',
       updated_at   = now()
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2';

-- ---------------------------------------------------------------------------
-- 8. What this migration asserts before it commits.
-- 
-- A migration that claims to reproduce a published report should fail rather than
-- commit if it does not.  These are the checks, and they run on the rows just
-- written.
-- 
-- They are measured over the journals THIS migration posted, not over the whole
-- organisation, because the organisation is not this migration's alone: another
-- session may be coding bank statement lines in it at the same time.  Those rows
-- are not part of the Xero reference dataset, are not deleted here, and do move
-- the organisation-wide balances off Xero's -- so the last check prints every
-- account where they do, with both figures, instead of pretending they are not
-- there.  docs/xero-import-00024.md carries the same comparison for both
-- scopes.
-- 
--   (i)   Every gl_journals row of this organisation has lines that sum to zero,
--         and total debits equal total credits across the whole ledger.
--   (ii)  Every one of the 25 accounts on Xero's Trial Balance carries Xero's
--         figure over this migration's journals, and those journals' 25-account
--         debits and credits total 42,595.46 each.  (The raw all-time ledger
--         total is larger: a trial balance nets accounts within the year and the
--         raw ledger does not.  Both are printed in docs/xero-import-00024.md.)
--   (iii) The documents and the ledger agree: the amount still due on the
--         captured sales invoices is the balance of account 610, the amount
--         still due on the captured bills is the balance of account 800, and the
--         amount still due on the captured expense claims is the balance of
--         account 801 Unpaid Expense Claims.
--   (iv)  Organisation-wide, every account that is not on Xero's figure is
--         named, with the amount it is out by.  This is a notice, not a failure:
--         it is a report of what else is in the organisation, not a defect in
--         this import.
-- ---------------------------------------------------------------------------
CREATE TEMP TABLE xr_tb (code text, amount numeric) ON COMMIT DROP;
INSERT INTO xr_tb (code, amount) VALUES
    ('200', -29539.18), ('300',    775.98), ('400',   9657.05), ('404',     30.00),
    ('408',   1110.00), ('412',     87.00), ('420',   1553.60), ('425',    105.50),
    ('429',    166.28), ('445',    335.82), ('449',    654.36), ('453',    862.48),
    ('461',     94.41), ('469',   3273.66), ('473',   1896.70), ('489',    236.37),
    ('493',    433.24), ('090',   7430.22), ('610',   9194.51), ('710',    923.79),
    ('720',   3774.49), ('800',  -8386.76), ('801',   -115.95), ('820',   -422.59),
    ('840',  -4130.98);

DO $verify$
DECLARE
    bad        int;
    dr         numeric;
    cr         numeric;
    ar_docs    numeric;
    ap_docs    numeric;
    ar_ledger  numeric;
    ap_ledger  numeric;
    mismatched text;
    elsewhere  text;
    claim_docs   numeric;
    claim_ledger numeric;
BEGIN
    -- (i) the ledger balances, organisation-wide
    SELECT count(*) INTO bad
      FROM (SELECT journal_id FROM gl_journal_lines
             GROUP BY journal_id HAVING sum(net_amount) <> 0) t;
    IF bad <> 0 THEN
        RAISE EXCEPTION '% journals do not sum to zero', bad;
    END IF;

    SELECT coalesce(sum(net_amount) FILTER (WHERE net_amount > 0), 0),
           coalesce(-sum(net_amount) FILTER (WHERE net_amount < 0), 0)
      INTO dr, cr
      FROM gl_journal_lines l
      JOIN gl_journals j USING (journal_id)
     WHERE j.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2';
    IF dr <> cr THEN
        RAISE EXCEPTION 'ledger does not balance: debits % credits %', dr, cr;
    END IF;
    RAISE NOTICE 'ledger, organisation-wide: debits = credits = %', dr;

    -- (ii) every account on Xero's Trial Balance, over this migration's journals
    SELECT string_agg(t.code || ' expected ' || t.amount || ' found ' || coalesce(got.bal, 0), ', ')
      INTO mismatched
      FROM xr_tb t
      LEFT JOIN (SELECT a.code, sum(l.net_amount) AS bal
                   FROM gl_journal_lines l
                   JOIN accounts a USING (account_id)
                  WHERE l.journal_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || g.journal_key) FROM xr_gl g)
                  GROUP BY a.code) got ON got.code = t.code
     WHERE coalesce(got.bal, 0) <> t.amount;
    IF mismatched IS NOT NULL THEN
        RAISE EXCEPTION 'trial balance mismatch: %', mismatched;
    END IF;

    -- A trial balance totals account BALANCES, not ledger lines: each of the
    -- 25 accounts is netted first (a sales account carries both the invoice
    -- credit and the receipt debit), and only then are the debits and the
    -- credits added up.  Summing the lines of those 25 accounts without
    -- netting them gives the remainder of the other 33 accounts, which is the
    -- 108,142.54 this check would otherwise compare against 42,595.46.
    SELECT coalesce(sum(s.bal) FILTER (WHERE s.bal > 0), 0),
           coalesce(-sum(s.bal) FILTER (WHERE s.bal < 0), 0)
      INTO dr, cr
      FROM (SELECT sum(l.net_amount) AS bal
              FROM gl_journal_lines l
              JOIN accounts a USING (account_id)
             WHERE a.code IN (SELECT code FROM xr_tb)
               AND l.journal_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || g.journal_key) FROM xr_gl g)
             GROUP BY a.code) s;    IF dr <> 42595.46 OR cr <> 42595.46 THEN
        RAISE EXCEPTION 'trial balance total is % / %, not 42595.46', dr, cr;
    END IF;
    RAISE NOTICE 'trial balance, Xero reference journals: 25 accounts match Xero, total % / %', dr, cr;

    -- (iii) documents and ledger agree.  The document side applies the same
    -- filter the aged reports apply -- AUTHORISED, and something still owing --
    -- because that is the population those reports measure.  Without it the
    -- two DRAFT sales invoices and the DRAFT/VOIDED/DELETED bills would be
    -- counted as receivables and payables, which is exactly what the ledger
    -- refuses to do.
    SELECT coalesce(sum(amount_due), 0) INTO ar_docs FROM invoices
     WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND type = 'ACCREC' AND status = 'AUTHORISED' AND amount_due > 0;
    SELECT coalesce(sum(amount_due), 0) INTO ap_docs FROM invoices
     WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND type = 'ACCPAY' AND status = 'AUTHORISED' AND amount_due > 0;

    SELECT coalesce(sum(l.net_amount), 0) INTO ar_ledger
      FROM gl_journal_lines l JOIN accounts a USING (account_id)
     WHERE a.code = '610'
       AND l.journal_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || g.journal_key) FROM xr_gl g);
    SELECT coalesce(sum(l.net_amount), 0) INTO ap_ledger
      FROM gl_journal_lines l JOIN accounts a USING (account_id)
     WHERE a.code = '800'
       AND l.journal_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || g.journal_key) FROM xr_gl g);
    -- 800 is a credit balance, so it is compared with the sign of the payables.
    IF ar_docs <> ar_ledger OR ap_docs <> -ap_ledger THEN
        RAISE EXCEPTION 'documents and ledger disagree: AR % vs %, AP % vs %',
                        ar_docs, ar_ledger, ap_docs, -ap_ledger;
    END IF;
    RAISE NOTICE 'aged receivables: documents % = ledger 610 %', ar_docs, ar_ledger;
    RAISE NOTICE 'aged payables:    documents % = ledger 800 %', ap_docs, -ap_ledger;

    -- The expense claims the same way: what the claims still owe IS the 801
    -- liability in the ledger.  801 is a credit balance, so it is compared
    -- with the sign of the claims.
    SELECT coalesce(sum(amount_due), 0) INTO claim_docs FROM expense_claims
     WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2';
    SELECT coalesce(sum(l.net_amount), 0) INTO claim_ledger
      FROM gl_journal_lines l JOIN accounts a USING (account_id)
     WHERE a.code = '801'
       AND l.journal_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || g.journal_key) FROM xr_gl g);
    IF claim_docs <> -claim_ledger THEN
        RAISE EXCEPTION 'expense claims and ledger disagree: claims % vs 801 %',
                        claim_docs, -claim_ledger;
    END IF;
    RAISE NOTICE 'aged expense claims: documents % = ledger 801 %', claim_docs, -claim_ledger;

    -- (iv) what else is in the organisation
    SELECT string_agg(t.code || ': ' || coalesce(got.bal, 0) || ' (Xero ' || t.amount || ', out by ' || (coalesce(got.bal, 0) - t.amount) || ')', '; ')
      INTO elsewhere
      FROM xr_tb t
      LEFT JOIN (SELECT a.code, sum(l.net_amount) AS bal
                   FROM gl_journal_lines l
                   JOIN gl_journals j USING (journal_id)
                   JOIN accounts a USING (account_id)
                  WHERE j.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
                  GROUP BY a.code) got ON got.code = t.code
     WHERE coalesce(got.bal, 0) <> t.amount;
    IF elsewhere IS NOT NULL THEN
        RAISE NOTICE 'organisation-wide, these accounts do not carry the Xero figure because the organisation also holds journal rows this migration did not write: %', elsewhere;
    ELSE
        RAISE NOTICE 'organisation-wide, all 25 Xero accounts carry the Xero figure';
    END IF;
END
$verify$;

-- The staging tables are ON COMMIT DROP, so the commit takes them with it.
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Undo of the import above: remove every row it created for the demo
-- organisation and put back what it displaced, so that `down` returns the
-- organisation to the state 00023 left it in and a following `up` reproduces
-- this migration exactly.
--
-- What is restored, with their original ids:
--   * the 48 bank transactions 00023 wrote, posted as 00023 posted them --
--     coded leg on the account the CSV records, no tax leg;
--   * 00023's derived opening balance journal of 8,654.01;
--   * the 21 contacts 00023 wrote, with the flags it gave them;
--   * the organisation's country, time zone, tax number and short code.
-- What is removed: everything else this migration wrote.
--
-- Note that 00023's opening balance comes back here.  That is correct for an
-- undo -- `down` restores the previous migration's state -- but it means the
-- 8,654.01 only stops being the organisation's opening balance while 00024 is
-- applied.  A `up` re-replaces it.

CREATE TEMP TABLE xr_undo_doc (doc_key text) ON COMMIT DROP;
INSERT INTO xr_undo_doc (doc_key) VALUES
    ('invoice/INV-0001'),
    ('invoice/INV-0002'),
    ('invoice/INV-0003'),
    ('invoice/INV-0004'),
    ('invoice/INV-0005'),
    ('invoice/INV-0006'),
    ('invoice/INV-0007'),
    ('invoice/INV-0008'),
    ('invoice/INV-0009'),
    ('invoice/INV-0010'),
    ('invoice/INV-0011'),
    ('invoice/INV-0012'),
    ('invoice/INV-0013'),
    ('invoice/INV-0016'),
    ('invoice/INV-0017'),
    ('invoice/INV-0018'),
    ('invoice/INV-0019'),
    ('invoice/INV-0020'),
    ('invoice/INV-0021'),
    ('invoice/INV-0022'),
    ('invoice/INV-0024'),
    ('invoice/INV-0025'),
    ('invoice/INV-0026'),
    ('invoice/INV-0027'),
    ('invoice/INV-0028'),
    ('invoice/INV-0029'),
    ('invoice/INV-0030'),
    ('invoice/INV-0031'),
    ('invoice/INV-0032'),
    ('invoice/INV-0033'),
    ('invoice/INV-0034'),
    ('invoice/INV-0036'),
    ('bill/Xero/2026-07-08/AP'),
    ('bill/Truxton Property Management/2026-07-11/RENT'),
    ('bill/PowerDirect/2026-07-11/Rpt'),
    ('bill/Net Connect/2026-07-12/Rpt'),
    ('bill/Central Copiers/2026-07-19/945-OCon'),
    ('bill/Net Connect/2026-07-20/9781'),
    ('bill/PC Complete/2026-07-21/'),
    ('bill/MCO Cleaning Services/2026-07-21/5679'),
    ('bill/Swanston Security/2026-07-21/AP'),
    ('bill/SMART Agency/2026-07-21/SM0195'),
    ('bill/PC Complete/2026-08-01/OG laptop'),
    ('bill/Xero/2026-08-08/AP'),
    ('bill/MCO Cleaning Services/2026-08-08/M000435'),
    ('bill/Hoyt Productions/2026-08-11/08-4123'),
    ('bill/Carlton Functions/2026-08-11/Dep'),
    ('bill/Truxton Property Management/2026-08-11/RENT'),
    ('bill/PowerDirect/2026-08-11/Rpt'),
    ('bill/Net Connect/2026-08-12/Rpt'),
    ('bill/MCO Cleaning Services/2026-08-15/M000442'),
    ('bill/ABC Furniture/2026-08-21/710'),
    ('bill/Swanston Security/2026-08-21/AP'),
    ('bill/MCO Cleaning Services/2026-08-22/M000456'),
    ('bill/MCO Cleaning Services/2026-08-29/M000463'),
    ('bill/Truxton Property Management/2026-08-31/RENT'),
    ('bill/SMART Agency/2026-08-31/SM0210'),
    ('bill/Bayside Club/2026-09-05/'),
    ('bill/PC Complete/2026-09-05/'),
    ('bill/MCO Cleaning Services/2026-09-05/M000471'),
    ('bill/Gateway Motors/2026-09-06/'),
    ('bill/Young Bros Transport/2026-09-07/'),
    ('bill/Xero/2026-09-07/AP'),
    ('bill/Bayside Wholesale/2026-09-08/GB1-White'),
    ('bill/PowerDirect/2026-09-10/Rpt'),
    ('bill/Capital Cab Co/2026-09-11/CS815'),
    ('bill/Net Connect/2026-09-11/Rpt'),
    ('bill/Swanston Security/2026-09-20/AP'),
    ('bill/Xero/2026-10-08/AP'),
    ('bill/Truxton Property Management/2026-10-11/RENT'),
    ('bill/PowerDirect/2026-10-11/Rpt'),
    ('bill/Net Connect/2026-10-12/Rpt'),
    ('bill/Swanston Security/2026-10-21/AP'),
    ('bill/Xero/2026-11-08/AP'),
    ('bill/Truxton Property Management/2026-11-11/RENT'),
    ('bill/PowerDirect/2026-11-11/Rpt'),
    ('bill/Net Connect/2026-11-12/Rpt'),
    ('bill/Swanston Security/2026-11-19/AP');

CREATE TEMP TABLE xr_undo_claim (claim_key text, contact text) ON COMMIT DROP;
INSERT INTO xr_undo_claim (claim_key, contact) VALUES
    ('expense-claim/Xero Demo/2026-09-10', 'Xero Demo'),
    ('expense-claim/Orlena Greenville/2026-08-11', 'Orlena Greenville'),
    ('expense-claim/Orlena Greenville/2026-07-11', 'Orlena Greenville');

CREATE TEMP TABLE xr_undo_cn (cn_key text) ON COMMIT DROP;
INSERT INTO xr_undo_cn (cn_key) VALUES
    ('credit-note/CN-0014'),
    ('credit-note/CN-0015'),
    ('credit-note/CN-0023'),
    ('credit-note/OG laptop'),
    ('credit-note/Refund');

CREATE TEMP TABLE xr_undo_gl (journal_key text) ON COMMIT DROP;
INSERT INTO xr_undo_gl (journal_key) VALUES
    ('bank-transaction/1'),
    ('bank-transaction/10'),
    ('bank-transaction/11'),
    ('bank-transaction/12'),
    ('bank-transaction/13'),
    ('bank-transaction/14'),
    ('bank-transaction/15'),
    ('bank-transaction/16'),
    ('bank-transaction/17'),
    ('bank-transaction/18'),
    ('bank-transaction/19'),
    ('bank-transaction/2'),
    ('bank-transaction/20'),
    ('bank-transaction/21'),
    ('bank-transaction/22'),
    ('bank-transaction/23'),
    ('bank-transaction/24'),
    ('bank-transaction/25'),
    ('bank-transaction/26'),
    ('bank-transaction/27'),
    ('bank-transaction/28'),
    ('bank-transaction/29'),
    ('bank-transaction/3'),
    ('bank-transaction/30'),
    ('bank-transaction/31'),
    ('bank-transaction/32'),
    ('bank-transaction/33'),
    ('bank-transaction/34'),
    ('bank-transaction/35'),
    ('bank-transaction/36'),
    ('bank-transaction/37'),
    ('bank-transaction/38'),
    ('bank-transaction/39'),
    ('bank-transaction/4'),
    ('bank-transaction/40'),
    ('bank-transaction/41'),
    ('bank-transaction/42'),
    ('bank-transaction/43'),
    ('bank-transaction/44'),
    ('bank-transaction/45'),
    ('bank-transaction/46'),
    ('bank-transaction/47'),
    ('bank-transaction/48'),
    ('bank-transaction/49'),
    ('bank-transaction/5'),
    ('bank-transaction/50'),
    ('bank-transaction/51'),
    ('bank-transaction/52'),
    ('bank-transaction/53'),
    ('bank-transaction/54'),
    ('bank-transaction/55'),
    ('bank-transaction/56'),
    ('bank-transaction/57'),
    ('bank-transaction/58'),
    ('bank-transaction/59'),
    ('bank-transaction/6'),
    ('bank-transaction/60'),
    ('bank-transaction/61'),
    ('bank-transaction/62'),
    ('bank-transaction/63'),
    ('bank-transaction/64'),
    ('bank-transaction/65'),
    ('bank-transaction/66'),
    ('bank-transaction/67'),
    ('bank-transaction/68'),
    ('bank-transaction/69'),
    ('bank-transaction/7'),
    ('bank-transaction/70'),
    ('bank-transaction/71'),
    ('bank-transaction/72'),
    ('bank-transaction/73'),
    ('bank-transaction/74'),
    ('bank-transaction/75'),
    ('bank-transaction/76'),
    ('bank-transaction/77'),
    ('bank-transaction/78'),
    ('bank-transaction/79'),
    ('bank-transaction/8'),
    ('bank-transaction/80'),
    ('bank-transaction/81'),
    ('bank-transaction/82'),
    ('bank-transaction/9'),
    ('bill/ABC Furniture/2026-08-21/710'),
    ('bill/Bayside Club/2026-09-05/'),
    ('bill/Bayside Wholesale/2026-09-08/GB1-White'),
    ('bill/Capital Cab Co/2026-09-11/CS815'),
    ('bill/Carlton Functions/2026-08-11/Dep'),
    ('bill/Central Copiers/2026-07-19/945-OCon'),
    ('bill/Gateway Motors/2026-09-06/'),
    ('bill/Hoyt Productions/2026-08-11/08-4123'),
    ('bill/MCO Cleaning Services/2026-07-21/5679'),
    ('bill/MCO Cleaning Services/2026-08-08/M000435'),
    ('bill/MCO Cleaning Services/2026-08-15/M000442'),
    ('bill/MCO Cleaning Services/2026-08-22/M000456'),
    ('bill/MCO Cleaning Services/2026-08-29/M000463'),
    ('bill/MCO Cleaning Services/2026-09-05/M000471'),
    ('bill/Net Connect/2026-07-12/Rpt'),
    ('bill/Net Connect/2026-07-20/9781'),
    ('bill/Net Connect/2026-08-12/Rpt'),
    ('bill/Net Connect/2026-09-11/Rpt'),
    ('bill/PC Complete/2026-07-21/'),
    ('bill/PC Complete/2026-08-01/OG laptop'),
    ('bill/PC Complete/2026-09-05/'),
    ('bill/PowerDirect/2026-07-11/Rpt'),
    ('bill/PowerDirect/2026-08-11/Rpt'),
    ('bill/PowerDirect/2026-09-10/Rpt'),
    ('bill/SMART Agency/2026-07-21/SM0195'),
    ('bill/SMART Agency/2026-08-31/SM0210'),
    ('bill/Swanston Security/2026-07-21/AP'),
    ('bill/Swanston Security/2026-08-21/AP'),
    ('bill/Truxton Property Management/2026-07-11/RENT'),
    ('bill/Truxton Property Management/2026-08-11/RENT'),
    ('bill/Truxton Property Management/2026-08-31/RENT'),
    ('bill/Xero/2026-07-08/AP'),
    ('bill/Xero/2026-08-08/AP'),
    ('bill/Xero/2026-09-07/AP'),
    ('bill/Young Bros Transport/2026-09-07/'),
    ('credit-note/CN-0014'),
    ('credit-note/CN-0015'),
    ('credit-note/CN-0023'),
    ('credit-note/OG laptop'),
    ('credit-note/Refund'),
    ('expense-claim/Orlena Greenville/2026-07-11'),
    ('expense-claim/Orlena Greenville/2026-08-11'),
    ('expense-claim/Xero Demo/2026-09-10'),
    ('invoice/INV-0001'),
    ('invoice/INV-0002'),
    ('invoice/INV-0003'),
    ('invoice/INV-0004'),
    ('invoice/INV-0005'),
    ('invoice/INV-0006'),
    ('invoice/INV-0007'),
    ('invoice/INV-0008'),
    ('invoice/INV-0009'),
    ('invoice/INV-0010'),
    ('invoice/INV-0011'),
    ('invoice/INV-0012'),
    ('invoice/INV-0013'),
    ('invoice/INV-0016'),
    ('invoice/INV-0017'),
    ('invoice/INV-0018'),
    ('invoice/INV-0019'),
    ('invoice/INV-0020'),
    ('invoice/INV-0021'),
    ('invoice/INV-0022'),
    ('invoice/INV-0024'),
    ('invoice/INV-0025'),
    ('invoice/INV-0026'),
    ('invoice/INV-0027'),
    ('invoice/INV-0028'),
    ('invoice/INV-0031'),
    ('invoice/INV-0032'),
    ('invoice/INV-0033'),
    ('invoice/INV-0034'),
    ('invoice/INV-0036'),
    ('opening-balance/journal/388');

CREATE TEMP TABLE xr_prev (
    n            int,
    tx_date      date,
    contact      text,
    description  text,
    reference    text,
    amount       numeric,
    direction    text,
    tax_type     text,
    coded_code   text
) ON COMMIT DROP;

-- The first 48 bank-transactions.csv rows, exactly as 00023 read them: the
-- account the capture records, tax_type from the rate name, and tax_amount 0.
INSERT INTO xr_prev (n, tx_date, contact, description, reference, amount, direction, tax_type, coded_code) VALUES
    (1, DATE '2026-09-11', 'Telus', 'Telus', NULL, -110.00, 'out', 'INPUT', '489'),
    (2, DATE '2026-09-11', '7-Eleven', '7-Eleven', NULL, -6.00, 'out', 'INPUT', '453'),
    (3, DATE '2026-09-10', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000471', -216.50, 'out', 'INPUT', '408'),
    (4, DATE '2026-09-09', 'Petrie McLoud Watson & Associates', 'Payment: Petrie McLoud Watson & Associates', 'Consulting | INV-0036', 2240.78, 'in', 'OUTPUT', '200'),
    (5, DATE '2026-09-09', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (6, DATE '2026-09-07', 'Boom FM', 'Payment: Boom FM', 'Training | INV-0033', 541.25, 'in', 'OUTPUT', '200'),
    (7, DATE '2026-09-07', 'Boom FM', 'Payment: Boom FM', 'Consulting | INV-0034', 3897.00, 'in', 'OUTPUT', '200'),
    (8, DATE '2026-09-06', 'Bank West', 'Payment: Bank West', 'Website | INV-0032', 324.75, 'in', 'OUTPUT', '200'),
    (9, DATE '2026-09-06', '7-Eleven', '7-Eleven', NULL, -8.00, 'out', 'INPUT', '453'),
    (10, DATE '2026-09-06', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (11, DATE '2026-09-06', '7-Eleven', '7-Eleven', NULL, -12.00, 'out', 'INPUT', '453'),
    (12, DATE '2026-09-05', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000463', -216.50, 'out', 'INPUT', '408'),
    (13, DATE '2026-09-03', 'City Agency', 'Payment: City Agency', 'Golf Balls | INV-0031', 104.40, 'in', 'OUTPUT2', '200'),
    (14, DATE '2026-09-03', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (15, DATE '2026-09-02', '7-Eleven', '7-Eleven', NULL, -4.00, 'out', 'INPUT', '453'),
    (16, DATE '2026-08-31', '7-Eleven', '7-Eleven', NULL, -5.50, 'out', 'INPUT', '453'),
    (17, DATE '2026-08-31', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000456', -216.50, 'out', 'INPUT', '408'),
    (18, DATE '2026-08-31', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (19, DATE '2026-08-30', '7-Eleven', '7-Eleven', NULL, -12.50, 'out', 'INPUT', '453'),
    (20, DATE '2026-08-30', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000442', -216.50, 'out', 'INPUT', '408'),
    (21, DATE '2026-08-26', '7-Eleven', '7-Eleven', NULL, -6.00, 'out', 'INPUT', '453'),
    (22, DATE '2026-08-26', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (23, DATE '2026-08-26', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (24, DATE '2026-08-24', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (25, DATE '2026-08-23', 'MCO Cleaning Services', 'Payment: MCO Cleaning Services', 'M000435', -216.50, 'out', 'INPUT', '408'),
    (26, DATE '2026-08-22', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (27, DATE '2026-08-21', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (28, DATE '2026-08-21', 'ABC Furniture', 'Payment: ABC Furniture', '710', -1000.00, 'out', 'INPUT', '710'),
    (29, DATE '2026-08-21', 'Rex Media Group', 'Payment: Rex Media Group', 'Monthly Support | INV-0021', 541.25, 'in', 'OUTPUT', '200'),
    (30, DATE '2026-08-21', 'Port & Philip Freight', 'Payment: Port & Philip Freight', 'Monthly Support | INV-0020', 541.25, 'in', 'OUTPUT', '200'),
    (31, DATE '2026-08-21', 'PowerDirect', 'Payment: multiple items', NULL, -1363.92, 'out', 'INPUT', '445'),
    (32, DATE '2026-08-21', 'Young Bros Transport', 'Payment: Young Bros Transport', 'Monthly Support | INV-0019', 541.25, 'in', 'OUTPUT', '200'),
    (33, DATE '2026-08-21', 'Carlton Functions', 'Payment: Carlton Functions', 'Dep', -1500.00, 'out', 'NONE', '420'),
    (34, DATE '2026-08-21', 'Hamilton Smith Ltd', 'Payment: Hamilton Smith Ltd', 'Monthly Support | INV-0018', 541.25, 'in', 'OUTPUT', '200'),
    (35, DATE '2026-08-21', 'Hoyt Productions', 'Payment: Hoyt Productions', '08-4123', -5953.75, 'out', 'INPUT', '400'),
    (36, DATE '2026-08-20', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (37, DATE '2026-08-20', '24 Locks', '24 Locks', NULL, -69.50, 'out', 'INPUT', '473'),
    (38, DATE '2026-08-20', 'Berry Brew', 'Berry Brew', NULL, -22.00, 'out', 'NONE', '420'),
    (39, DATE '2026-08-19', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (40, DATE '2026-08-18', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (41, DATE '2026-08-16', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (42, DATE '2026-08-15', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (43, DATE '2026-08-14', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (44, DATE '2026-08-14', 'Office Supplies Company', 'Office Supplies Company', NULL, -49.20, 'out', 'INPUT', '461'),
    (45, DATE '2026-08-13', 'Passing Places Parking', 'Passing Places Parking', NULL, -12.00, 'out', 'INPUT', '493'),
    (46, DATE '2026-08-12', 'Melrose Parking', 'Melrose Parking', NULL, -148.50, 'out', 'INPUT', '449'),
    (47, DATE '2026-08-11', 'Woolworths Market', 'Woolworths Market', NULL, -34.10, 'out', 'INPUT', '453'),
    (48, DATE '2026-08-11', 'Boom FM', 'Payment: Boom FM', 'Training | INV-0013', 1082.50, 'in', 'OUTPUT', '200');

CREATE TEMP TABLE xr_undo_contact (name text) ON COMMIT DROP;
INSERT INTO xr_undo_contact (name) VALUES
    ('Abby & Wells'),
    ('Adam Michkevich'),
    ('Basket Case'),
    ('Bayside Club'),
    ('Bayside Wholesale'),
    ('Brunswick Petals'),
    ('Capital Cab Co'),
    ('Central Copiers'),
    ('City Limousines'),
    ('Coco Cafe'),
    ('DIISR - Small Business Services'),
    ('Dimples Warehouse'),
    ('Eastside Club'),
    ('Epicenter Cafe'),
    ('Espresso 31'),
    ('Fulton Airport Parking'),
    ('Gable Print'),
    ('Gateway Motors'),
    ('Luna Cafe'),
    ('Marine Systems'),
    ('Net Connect'),
    ('Orlena Greenville'),
    ('PC Complete'),
    ('RITE Agency'),
    ('Ridgeway Bank'),
    ('Ridgeway University'),
    ('SMART Agency'),
    ('Swanston Security'),
    ('Truxton Property Management'),
    ('Xero');

CREATE TEMP TABLE xr_prev_contact (name text, is_customer boolean, is_supplier boolean) ON COMMIT DROP;
INSERT INTO xr_prev_contact (name, is_customer, is_supplier) VALUES
    ('24 Locks', FALSE, TRUE),
    ('7-Eleven', FALSE, TRUE),
    ('ABC Furniture', FALSE, TRUE),
    ('Bank West', TRUE, FALSE),
    ('Berry Brew', FALSE, TRUE),
    ('Boom FM', TRUE, FALSE),
    ('Carlton Functions', FALSE, TRUE),
    ('City Agency', TRUE, FALSE),
    ('Hamilton Smith Ltd', TRUE, FALSE),
    ('Hoyt Productions', FALSE, TRUE),
    ('MCO Cleaning Services', FALSE, TRUE),
    ('Melrose Parking', FALSE, TRUE),
    ('Office Supplies Company', FALSE, TRUE),
    ('Passing Places Parking', FALSE, TRUE),
    ('Petrie McLoud Watson & Associates', TRUE, FALSE),
    ('Port & Philip Freight', TRUE, FALSE),
    ('PowerDirect', FALSE, TRUE),
    ('Rex Media Group', TRUE, FALSE),
    ('Telus', FALSE, TRUE),
    ('Woolworths Market', FALSE, TRUE),
    ('Young Bros Transport', TRUE, FALSE);

-- ---------------------------------------------------------------------------
-- 1. The documents and the expense claims.
-- 
-- Deleting the invoices takes their line items with them
-- (invoice_line_items.invoice_id is ON DELETE CASCADE), and the credit notes take
-- theirs.  payments rows go first so nothing points at a half-deleted document.
-- ---------------------------------------------------------------------------
DELETE FROM payments
 WHERE payment_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/payment/' || g::text) FROM generate_series(1, 44) g);

DELETE FROM credit_notes
 WHERE credit_note_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || k.cn_key) FROM xr_undo_cn k);

DELETE FROM invoices
 WHERE invoice_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || k.doc_key) FROM xr_undo_doc k);

-- The expense claims and the claimants they name.  The claims go first:
-- expense_claims.user_id is ON DELETE SET NULL, so deleting the users before
-- the claims would quietly clear the link instead of removing the claim.
DELETE FROM expense_claims
 WHERE expense_claim_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/' || c.claim_key) FROM xr_undo_claim c);

DELETE FROM users
 WHERE user_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/user/' || c.contact) FROM xr_undo_claim c);

-- ---------------------------------------------------------------------------
-- 2. Invoice numbering, back to the schema 00006 declared.
-- 
-- The constraint can only come back once the bills it cannot hold are gone, which
-- is why this runs after the deletes above.
-- ---------------------------------------------------------------------------
DROP INDEX invoices_org_accrec_number_key;
ALTER TABLE invoices ADD CONSTRAINT invoices_organisation_id_invoice_number_key
    UNIQUE (organisation_id, invoice_number);

-- ---------------------------------------------------------------------------
-- 3. The contacts.
-- 
-- The 30 this migration added go; the 21 00023 wrote get their flags and their
-- empty email address back.
-- ---------------------------------------------------------------------------
DELETE FROM contacts
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
   AND contact_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/contact/' || c.name)
                        FROM xr_undo_contact c);

UPDATE contacts c
   SET is_customer = p.is_customer,
       is_supplier = p.is_supplier,
       email_address = NULL,
       updated_date_utc = now()
  FROM xr_prev_contact p
 WHERE c.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
   AND c.contact_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/contact/' || p.name);

-- ---------------------------------------------------------------------------
-- 4. The bank transactions, back to 00023's 48 and 00023's posting of them.
-- ---------------------------------------------------------------------------
DELETE FROM bank_transaction_line_items
 WHERE line_item_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || g::text || '/line/1') FROM generate_series(1, 82) g);

DELETE FROM bank_transactions
 WHERE bank_transaction_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || g::text) FROM generate_series(1, 82) g);

INSERT INTO bank_transactions (bank_transaction_id, organisation_id, contact_id,
                               bank_account_id, type, is_reconciled, date, reference,
                               currency_code, status, line_amount_types, sub_total,
                               total_tax, total)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text),
       '6823b27b-c48f-4099-bb27-4202a4f496a2',
       (SELECT c.contact_id FROM contacts c
         WHERE c.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND c.name = s.contact),
       (SELECT a.account_id FROM accounts a
         WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090'),
       CASE s.direction WHEN 'out' THEN 'SPEND' ELSE 'RECEIVE' END,
       TRUE, s.tx_date, s.reference, 'USD', 'AUTHORISED', 'Exclusive',
       s.amount, 0, s.amount
  FROM xr_prev s;

INSERT INTO bank_transaction_line_items (line_item_id, bank_transaction_id,
                                         description, quantity, unit_amount,
                                         account_code, account_id, tax_type,
                                         tax_amount, line_amount)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text || '/line/1'),
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text),
       s.description, 1, s.amount, s.coded_code,
       (SELECT a.account_id FROM accounts a
         WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = s.coded_code),
       s.tax_type, 0, s.amount
  FROM xr_prev s;

-- ---------------------------------------------------------------------------
-- 5. The ledger, back to 00023's journals.
-- ---------------------------------------------------------------------------
DELETE FROM gl_journals
 WHERE journal_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/' || k.journal_key) FROM xr_undo_gl k);

-- 00023's one journal per transaction ...
INSERT INTO gl_journals (journal_id, organisation_id, reference, source_type, source_id,
                         journal_date)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text || '/journal'),
       '6823b27b-c48f-4099-bb27-4202a4f496a2', s.reference, 'BANKTRANSACTION',
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text),
       s.tx_date
  FROM xr_prev s;

-- ... its bank leg ...
INSERT INTO gl_journal_lines (line_id, journal_id, account_id, description, tax_type,
                              tax_amount, net_amount, gross_amount)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text || '/gl/bank'),
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text || '/journal'),
       b.account_id, NULL, NULL, 0, s.amount, s.amount
  FROM xr_prev s
  CROSS JOIN (SELECT a.account_id FROM accounts a
               WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = '090') b;

-- ... and its coded leg.
INSERT INTO gl_journal_lines (line_id, journal_id, account_id, description, tax_type,
                              tax_amount, net_amount, gross_amount)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text || '/gl/coded'),
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/bank-transaction/' || s.n::text || '/journal'),
       a.account_id, s.description, s.tax_type, 0, -s.amount, -s.amount
  FROM xr_prev s
  JOIN accounts a ON a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
                 AND a.code = s.coded_code;

-- 00023's derived opening balance, as it derived it.
INSERT INTO gl_journals (journal_id, organisation_id, reference, source_type, source_id,
                         journal_date)
VALUES (uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/opening-balance/journal'),
        '6823b27b-c48f-4099-bb27-4202a4f496a2', 'Opening balance brought forward',
        'MANUALJOURNAL', NULL, DATE '2025-12-31');

INSERT INTO gl_journal_lines (line_id, journal_id, account_id, description, tax_type,
                              tax_amount, net_amount, gross_amount)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/opening-balance/' || v.code),
       uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/opening-balance/journal'),
       a.account_id, 'Opening balance brought forward', NULL, 0, v.net, v.net
  FROM (VALUES ('090', 8654.01), ('840', -8654.01)) AS v(code, net)
  JOIN accounts a ON a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code = v.code;

SELECT setval('gl_journals_journal_number_seq',
              GREATEST((SELECT COALESCE(max(journal_number), 0) FROM gl_journals), 1));

-- ---------------------------------------------------------------------------
-- 6. The organisation's settings, back to what they were.
-- ---------------------------------------------------------------------------
UPDATE organisations
   SET country_code = 'US',
       timezone     = 'UTC',
       tax_number   = NULL,
       short_code   = 'DEMO',
       updated_at   = now()
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2';
-- +goose StatementEnd
