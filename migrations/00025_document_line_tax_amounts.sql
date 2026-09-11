-- +goose Up
-- +goose StatementBegin
-- Put the tax back on the document lines.
--
-- 00024 built the ledger from a literal VALUES table in which every document
-- line was written with tax_amount 0.00, so the tax a document carries lives
-- only on its own `820` leg and on the line-item table behind it.  The Sales
-- Tax report reads gl_journal_lines and nothing else, so it sees none of it:
-- `docs/sales-tax-tax-amount.md` sections 5.1 and 6 measure the gap and
-- specify this backfill.
--
-- WHAT IT WRITES.  One column, gl_journal_lines.tax_amount, on the document
-- lines of the three document journals 00024 posted: source_type INVOICE,
-- CREDITNOTE, EXPENSECLAIM.  A document line is the line that names a rate
-- (tax_type IS NOT NULL) and is not the tax leg itself (account code not 820).
-- The `820` legs already carry the right figure and are what this backfill
-- reconciles to; the control legs (610 / 800 / 801) name no rate and are not
-- candidates.  That is 83 lines in the reference dataset: 74 invoice, 5 credit
-- note, 4 expense claim.
--
-- WHERE THE VALUE COMES FROM.  invoice_line_items.tax_amount for an invoice
-- line, credit_note_line_items.tax_amount for a credit-note line, and for the
-- three EXPENSECLAIM journals -- which 00024 wrote with a NULL source_id and
-- whose receipts tables are empty -- the sibling `820` leg of the same
-- journal, paired by the ordinal 00024 laid the journal out with (`line/N`
-- beside `tax/N`).  The line-item joins are keyed on the account, the absolute
-- line amount and the description, and are verified 1:1 against this
-- organisation's own documents (section 6.2, E15): every one of the 83
-- candidate lines matches exactly one source line item.
--
-- THE SIGN.  tax_amount = source_tax * CASE WHEN <income class> THEN
-- sign(-net_amount) ELSE sign(net_amount) END -- positive when the line moves
-- in the document's own direction, negative when the document reverses it, so
-- a customer credit note subtracts from Tax Collected instead of adding to it.
-- Section 6.3 shows this is the only rule that reproduces both the 33 bank
-- lines 00024 already filled and Xero's own captured General Ledger.
--
-- WHY THE PREDICATE MAKES `down` EXACT.  The candidate predicate is a function
-- of the documents, the journal's source_type, the account and the line's own
-- description and amount.  It does not read tax_amount, and the value written
-- is likewise a pure expression of the source line item -- never of the
-- current tax_amount -- so a second `up` recomputes the same number for the
-- same row and changes nothing, which is the sense in which this migration is
-- safe to run where it has already run.  Because 00024 wrote every document
-- line with tax_amount 0.00, every row the predicate selects carried 0.00
-- before this migration; the set of rows `up` changes is therefore exactly the
-- set it selects, and `down` restores those rows -- and only those rows -- by
-- setting them back to 0.00.  The four lines bank coding has added since
-- (journals 1612-1615, section 8) are BANKTRANSACTION journals, which the
-- predicate does not select, and their lines stay untouched in both
-- directions.
--
-- WHAT IT CANNOT TOUCH.  No net_amount, gross_amount, account_id, journal or
-- document row is written, so no account balance moves and account 820 -- the
-- tax control account -- is not written at all.  The nine `NONE` lines whose
-- rate is 0% keep their correct 0.00, and no line is invented a value: a line
-- with no source document is left at 0.00 rather than estimated.
--
-- Ids are resolved by join, not by a list, so this is a statement about the
-- organisation's own rows and about no other row.
-- ---------------------------------------------------------------------------
WITH acct AS (
    -- The class of the account a line hit, and the side it belongs to.  Only
    -- the income classes reverse the source amount's sign (see THE SIGN).
    SELECT a.account_id, a.code, a.type
      FROM accounts a
     WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
),
claim_journals AS (
    SELECT j.journal_id
      FROM gl_journals j
     WHERE j.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND j.source_type = 'EXPENSECLAIM'
),
claim_pairs AS (
    -- An expense claim's 00024 journal has a NULL source_id and no receipt
    -- behind it, so the sibling `820` leg is the only surviving record of the
    -- claim's tax.  Pair the coded lines with the tax legs by the ordinal the
    -- journal was written in; the journal total is exact even though the two
    -- legs of a two-line claim are indistinguishable from each other.
    SELECT d.line_id AS doc_line_id, t.amount AS tax
      FROM (SELECT l.line_id, l.journal_id,
                   row_number() OVER (PARTITION BY l.journal_id ORDER BY l.line_id) AS rn
              FROM gl_journal_lines l
              JOIN accounts a ON a.account_id = l.account_id
             WHERE l.journal_id IN (SELECT journal_id FROM claim_journals)
               AND a.code NOT IN ('801', '820')) d
      JOIN (SELECT l.line_id, l.journal_id, l.net_amount AS amount,
                   row_number() OVER (PARTITION BY l.journal_id ORDER BY l.line_id) AS rn
              FROM gl_journal_lines l
              JOIN accounts a ON a.account_id = l.account_id
             WHERE l.journal_id IN (SELECT journal_id FROM claim_journals)
               AND a.code = '820') t
        ON t.journal_id = d.journal_id AND t.rn = d.rn
),
doc AS (
    SELECT l.line_id,
           COALESCE(il.tax_amount, cnl.tax_amount, cp.tax)
             * CASE WHEN a.type IN ('REVENUE', 'SALES') THEN sign(-l.net_amount)
                    ELSE sign(l.net_amount) END AS tax_amount
      FROM gl_journal_lines l
      JOIN gl_journals j ON j.journal_id = l.journal_id
      JOIN acct a        ON a.account_id = l.account_id
      LEFT JOIN invoice_line_items il
             ON j.source_type = 'INVOICE' AND il.invoice_id = j.source_id
            AND il.account_id = l.account_id AND il.line_amount = abs(l.net_amount)
            AND COALESCE(il.description, '') = COALESCE(l.description, '')
      LEFT JOIN credit_note_line_items cnl
             ON j.source_type = 'CREDITNOTE' AND cnl.credit_note_id = j.source_id
            AND cnl.account_code = a.code AND cnl.line_amount = abs(l.net_amount)
            AND COALESCE(cnl.description, '') = COALESCE(l.description, '')
      LEFT JOIN claim_pairs cp ON cp.doc_line_id = l.line_id
     WHERE j.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND j.source_type IN ('INVOICE', 'CREDITNOTE', 'EXPENSECLAIM')
       AND COALESCE(l.tax_type, '') <> ''
       AND a.code <> '820'
       -- Leave a line whose document cannot be found at 0.00: an unsourced
       -- line is unmeasured, not zero-tax (section 6.4).
       AND (il.line_item_id IS NOT NULL OR cnl.line_item_id IS NOT NULL
            OR cp.doc_line_id IS NOT NULL)
)
UPDATE gl_journal_lines l
   SET tax_amount = doc.tax_amount
  FROM doc
 WHERE l.line_id = doc.line_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Restore exactly the rows `up` changed: the same candidate predicate selects
-- the same 83 document lines -- it never reads tax_amount, so nothing about the
-- value `up` wrote can move it -- and every one of them carried 0.00 before
-- this migration.  Nothing outside that set is written: not the `820` or
-- control legs, which name no rate, and not the BANKTRANSACTION lines of
-- journals 1612-1615, whose journals are not document journals.
WITH acct AS (
    SELECT a.account_id, a.code, a.type
      FROM accounts a
     WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
),
claim_journals AS (
    SELECT j.journal_id
      FROM gl_journals j
     WHERE j.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND j.source_type = 'EXPENSECLAIM'
),
claim_pairs AS (
    SELECT d.line_id AS doc_line_id
      FROM (SELECT l.line_id, l.journal_id,
                   row_number() OVER (PARTITION BY l.journal_id ORDER BY l.line_id) AS rn
              FROM gl_journal_lines l
              JOIN accounts a ON a.account_id = l.account_id
             WHERE l.journal_id IN (SELECT journal_id FROM claim_journals)
               AND a.code NOT IN ('801', '820')) d
      JOIN (SELECT l.line_id, l.journal_id,
                   row_number() OVER (PARTITION BY l.journal_id ORDER BY l.line_id) AS rn
              FROM gl_journal_lines l
              JOIN accounts a ON a.account_id = l.account_id
             WHERE l.journal_id IN (SELECT journal_id FROM claim_journals)
               AND a.code = '820') t
        ON t.journal_id = d.journal_id AND t.rn = d.rn
),
doc AS (
    SELECT l.line_id
      FROM gl_journal_lines l
      JOIN gl_journals j ON j.journal_id = l.journal_id
      JOIN acct a        ON a.account_id = l.account_id
      LEFT JOIN invoice_line_items il
             ON j.source_type = 'INVOICE' AND il.invoice_id = j.source_id
            AND il.account_id = l.account_id AND il.line_amount = abs(l.net_amount)
            AND COALESCE(il.description, '') = COALESCE(l.description, '')
      LEFT JOIN credit_note_line_items cnl
             ON j.source_type = 'CREDITNOTE' AND cnl.credit_note_id = j.source_id
            AND cnl.account_code = a.code AND cnl.line_amount = abs(l.net_amount)
            AND COALESCE(cnl.description, '') = COALESCE(l.description, '')
      LEFT JOIN claim_pairs cp ON cp.doc_line_id = l.line_id
     WHERE j.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND j.source_type IN ('INVOICE', 'CREDITNOTE', 'EXPENSECLAIM')
       AND COALESCE(l.tax_type, '') <> ''
       AND a.code <> '820'
       AND (il.line_item_id IS NOT NULL OR cnl.line_item_id IS NOT NULL
            OR cp.doc_line_id IS NOT NULL)
)
UPDATE gl_journal_lines l
   SET tax_amount = 0.00
  FROM doc
 WHERE l.line_id = doc.line_id;
-- +goose StatementEnd
