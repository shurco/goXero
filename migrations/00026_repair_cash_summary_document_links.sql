-- +goose Up
-- +goose StatementBegin
-- Cash Summary, part 1: the document links the report reads.
--
-- docs/cash-summary-xero-rule.md section 1 says what a Cash Summary is: Xero
-- does not report the movement of the control accounts.  A receipt that clears
-- an invoice is reported as the movement of the income accounts that invoice
-- was coded to; a payment that clears a bill is reported as the movement of the
-- expense accounts that bill was coded to.  The report therefore has to look
-- THROUGH 610 Accounts Receivable, 800 Accounts Payable and 801 Unpaid Expense
-- Claims at the document each cash movement settled -- and it does that by
-- joining payments.invoice_id.  It guesses nothing at run time.  Where a link is
-- missing the amount is reported as not attributed to a document, with its
-- total.  So the links have to be right in the data, and that is this migration.
--
-- 1. WHY THEY ARE MISSING.
--    00024 wrote payments.invoice_id by matching the captured document number
--    on the payment row against the contact's documents.  Deliberately, it
--    wrote NULL when the contact has more than one document with that number
--    ("a blank or repeated document number leaves this NULL rather than
--    attaching the payment to the wrong document"), and also when there is no
--    number at all.  Twelve of the 24 ACCPAYPAYMENT rows came out NULL; all 20
--    ACCRECPAYMENT rows are linked.  In Xero those twelve payments each cleared
--    exactly one bill.  Nothing here is a new posting: a payment row carries no
--    ledger entry of its own -- the money side is the bank transaction the same
--    capture already holds -- so this migration writes no gl_journals or
--    gl_journal_lines row, moves no account balance and changes no amount.
--    It writes payments.invoice_id and, for the five credit notes, the
--    allocation the capture implies.
--
-- 2. THE KEY EACH OF THE TWELVE SATISFIES.  No row is guessed.  Each link is
--    named below with the evidence that picks it out, and every one of those
--    pieces is a captured value: the payment's amount and date (payments.csv),
--    the bill's number, date, total and amount_paid (bills.csv), and the bank
--    transaction's own reference, which is the same string as the CSV's
--    document_number and is what the bank grid shows beside the payment.
--
--    (a) contact + document number + amount -- 4 rows.  The number is not
--        enough on its own: Net Connect has three bills numbered "Rpt" and
--        PowerDirect three.  Within one contact and one number the captured
--        amount picks out exactly one bill.
--          payment 23  Net Connect,   "Rpt",  44.92, 21 Jul -> bill/Net Connect/2026-07-12/Rpt
--          payment 25  PowerDirect,   "Rpt", 119.08, 21 Jul -> bill/PowerDirect/2026-07-11/Rpt
--          payment 16  Net Connect,   "Rpt",  46.82, 21 Aug -> bill/Net Connect/2026-08-12/Rpt
--          payment 18  PowerDirect,   "Rpt", 135.85, 21 Aug -> bill/PowerDirect/2026-08-11/Rpt
--
--    (b) contact + document number + amount + the bill is the earliest one
--        still unsettled -- 5 rows.  Truxton has three bills numbered "RENT"
--        and Xero has two numbered "AP", and every one of them carries the same
--        amount, so the amount does not separate them.  The captured dates do:
--        a payment cannot settle a bill dated after it, and of the bills that
--        can be settled the earliest is the one still open -- the later ones
--        are settled by the later payments in this same list.  That is the
--        only assignment of these five payments to these five bills under which
--        no payment settles a bill that did not yet exist and no bill is
--        settled twice.
--          payment 43  Xero,       "AP",    31.39,  8 Jul -> bill/Xero/2026-07-08/AP
--          payment 42  Xero,       "AP",    31.39,  8 Aug -> bill/Xero/2026-08-08/AP
--          payment 26  Truxton,    "RENT", 1181.25, 21 Jul -> bill/Truxton .../2026-07-11/RENT
--          payment 20  Truxton,    "RENT", 1181.25, 21 Aug -> bill/Truxton .../2026-08-11/RENT
--          payment 36  Truxton,    "RENT", 1181.25, 31 Aug -> bill/Truxton .../2026-08-31/RENT
--
--    (c) contact + the bill's own settled total -- 3 rows.  These three have no
--        usable document number: two have a blank one on both sides -- the CSV
--        cell is empty and the bank transaction has no reference either -- and
--        the third is numbered "AP" while the bank grid's reference for the
--        same money is also "AP", but the amount does not match any Swanston
--        bill's total.  What picks these out is that the cash is the whole of
--        what the bill owes once the credit note that settled the rest of it is
--        added back: the captured amount_paid is the cash, and
--        total - amount_paid is exactly the captured credit note's total.
--          payment 24  PC Complete,    blank, 1682.74, 21 Jul
--                        -> bill/PC Complete/2026-07-21/
--                           1682.74 cash + 270.63 "OG laptop" note = 1953.37, the bill's total
--          payment 31  Swanston Security, "AP",  34.10, 27 Jul
--                        -> bill/Swanston Security/2026-07-21/AP
--                           34.10 cash + 25.44 "Refund" note = 59.54, the bill's total
--          payment 41  Gateway Motors,   blank,  411.35,  7 Sep
--                        -> bill/Gateway Motors/2026-09-06/
--                           411.35 cash, no note, and 411.35 is the bill's total
--
-- 3. THE FIVE CREDIT-NOTE ALLOCATIONS.
--    credit_note_allocations is empty and every invoices.amount_credited is 0,
--    but the capture says what the notes settled, and uniquely.  The rule is
--    one sentence, and it is checked below rather than assumed: a credit note
--    that is PAID with nothing left on it, and that no payment row already
--    records, is covered by exactly one document of the same contact whose
--    unpaid remainder is exactly the note's total.  Four of the five captured
--    notes are in that population and the check finds exactly one document for
--    each.  The fifth, PC Complete's "OG laptop" note, is the one note a
--    payment row already records: 00024 matched payment 1 to it by number, and
--    that same row carries the "OG laptop" bill of the same name.  The rule
--    above therefore does not reach it -- the NOT EXISTS clause is what makes
--    this count four -- and it is named explicitly in the table below instead.
--    What it settles is not its own bill but PC Complete's 2026-07-21 bill, the
--    one payment 24 paid 1682.74 in cash against; this note covers the
--    remaining 270.63 of that bill's 1953.37, the arithmetic section (c) states.
--      credit-note/CN-0015 -> invoice/INV-0005   541.25 = 541.25 - 0     "Full credit - DUPLICATE of INV-0001"
--      credit-note/CN-0014 -> invoice/INV-0010   541.25 = 541.25 - 0     "CREDIT Half day training ... INV-0013"
--      credit-note/CN-0023 -> invoice/INV-0022    21.70 = 238.20 - 216.50 credit for an item charged in error
--      credit-note/OG laptop -> bill/PC Complete/2026-07-21/  270.63 = 1953.37 - 1682.74
--      credit-note/Refund    -> bill/Swanston Security/2026-07-21/AP
--                                                25.44 = 59.54 - 34.10
--    The amount written is the credit note's own total, read from the note --
--    no figure is retyped here.  The date written is the note's captured date.
--
-- 4. WHAT IT DOES NOT DO.  A payment whose document link cannot be evidenced
--    stays NULL and the Cash Summary states the amount as unattributed; that is
--    the case for every ACCRECPAYMENT, which 00024 already linked, and for any
--    payment another session adds.  Two things the capture cannot settle are
--    therefore left exactly as they are:
--      - No credit note is allocated anywhere the rule above does not pick out
--        one document uniquely, and no allocation is inferred from "the
--        contact's oldest open invoice".  Where the Cash Summary needs an
--        allocation it is because the rule needs one; the report says so.
--      - No payment is split across several bills.  bills.csv shows one bill
--        per payment here, and a payment row has one invoice_id column: a
--        payment that covered two bills could not be recorded in it at all.
--        Nothing in this organisation's capture is in that state -- every
--        amount above matches one bill's amount_paid exactly -- so no row is
--        left half-recorded.
--
-- 5. IDEMPOTENT IN EFFECT, AND THE DOWN IS EXACT.
--    Up touches a payment only where invoice_id IS NULL, an invoice only where
--    amount_credited = 0, and inserts allocations under fixed uuid_generate_v5
--    keys with ON CONFLICT DO NOTHING, so a second run changes nothing.  Down
--    reverses exactly what Up did because every predicate it uses is one Up
--    required: it clears invoice_id only on the twelve named payments, and only
--    where the column holds the id Up set it to; it deletes only the five
--    allocation ids Up could have created; and it zeroes amount_credited only
--    where the column holds the note total Up wrote.  A link or an allocation
--    another session adds is not named here and is not touched, in either
--    direction.
-- ---------------------------------------------------------------------------

CREATE TEMP TABLE xr_cs_link (n int, doc_key text) ON COMMIT DROP;
INSERT INTO xr_cs_link (n, doc_key) VALUES
    -- (a) contact + document number + amount
    (23, 'bill/Net Connect/2026-07-12/Rpt'),
    (25, 'bill/PowerDirect/2026-07-11/Rpt'),
    (16, 'bill/Net Connect/2026-08-12/Rpt'),
    (18, 'bill/PowerDirect/2026-08-11/Rpt'),
    -- (b) contact + document number + amount + earliest still unsettled
    (43, 'bill/Xero/2026-07-08/AP'),
    (42, 'bill/Xero/2026-08-08/AP'),
    (26, 'bill/Truxton Property Management/2026-07-11/RENT'),
    (20, 'bill/Truxton Property Management/2026-08-11/RENT'),
    (36, 'bill/Truxton Property Management/2026-08-31/RENT'),
    -- (c) contact + the bill's own settled total
    (24, 'bill/PC Complete/2026-07-21/'),
    (31, 'bill/Swanston Security/2026-07-21/AP'),
    (41, 'bill/Gateway Motors/2026-09-06/');

UPDATE payments p
   SET invoice_id = i.invoice_id
  FROM xr_cs_link k
  JOIN invoices i
    ON i.invoice_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || k.doc_key)
 WHERE p.payment_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/payment/' || k.n::text)
   AND p.invoice_id IS NULL;

-- ---------------------------------------------------------------------------
-- The five credit-note allocations, and the amount_credited they imply.
-- ---------------------------------------------------------------------------
CREATE TEMP TABLE xr_cs_alloc (cn_key text, doc_key text) ON COMMIT DROP;
INSERT INTO xr_cs_alloc (cn_key, doc_key) VALUES
    ('credit-note/CN-0015', 'invoice/INV-0005'),
    ('credit-note/CN-0014', 'invoice/INV-0010'),
    ('credit-note/CN-0023', 'invoice/INV-0022'),
    ('credit-note/OG laptop', 'bill/PC Complete/2026-07-21/'),
    ('credit-note/Refund', 'bill/Swanston Security/2026-07-21/AP');

INSERT INTO credit_note_allocations (allocation_id, credit_note_id, invoice_id,
                                     amount, date)
SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/credit-note-allocation/' || a.cn_key || '/to/' || a.doc_key),
       cn.credit_note_id,
       i.invoice_id,
       cn.total,
       cn.date
  FROM xr_cs_alloc a
  JOIN credit_notes cn
    ON cn.credit_note_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || a.cn_key)
  JOIN invoices i
    ON i.invoice_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || a.doc_key)
ON CONFLICT (allocation_id) DO NOTHING;

UPDATE invoices i
   SET amount_credited = cn.total
  FROM xr_cs_alloc a
  JOIN credit_notes cn
    ON cn.credit_note_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || a.cn_key)
 WHERE i.invoice_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || a.doc_key)
   AND i.amount_credited = 0;

-- ---------------------------------------------------------------------------
-- Checks.  Each one fails the migration rather than leaving a half-repaired
-- ledger behind, and each one is a statement about the DATA, not about the
-- statements above: (i) re-derives the allocation rule organisation-wide,
-- (ii) re-reads the state the report will read.
-- ---------------------------------------------------------------------------
DO $verify$
DECLARE
    unlinked    int;
    wrong_bill  text;
    no_match    int;
    ambiguous   text;
    allocs      int;
    uncredited  int;
    pay_linked  int;
BEGIN
    -- (i) The allocation rule, re-derived over every credit note in the
    -- organisation rather than over the five named above.  A PAID note with
    -- nothing left on it that no payment row records must be covered by
    -- exactly one document -- one, not zero and not two.  If this ever fails,
    -- the five allocations below are not evidenced and must not be written.
    SELECT count(*) INTO no_match
      FROM credit_notes cn
     WHERE cn.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND cn.status = 'PAID' AND cn.remaining_credit = 0
       AND NOT EXISTS (SELECT 1 FROM payments p WHERE p.credit_note_id = cn.credit_note_id)
       AND (SELECT count(*) FROM invoices i
             WHERE i.organisation_id = cn.organisation_id
               AND i.contact_id = cn.contact_id
               AND i.type = CASE cn.type WHEN 'ACCRECCREDIT' THEN 'ACCREC' ELSE 'ACCPAY' END
               AND i.status = 'PAID' AND i.amount_due = 0
               AND i.total - i.amount_paid = cn.total) <> 1;
    IF no_match <> 0 THEN
        RAISE EXCEPTION '% credit note(s) are not covered by exactly one document; the allocation rule does not hold', no_match;
    END IF;

    SELECT string_agg(cn.credit_note_number || ' -> ' ||
                      (SELECT count(*)::text FROM invoices i
                        WHERE i.organisation_id = cn.organisation_id
                          AND i.contact_id = cn.contact_id
                          AND i.type = CASE cn.type WHEN 'ACCRECCREDIT' THEN 'ACCREC' ELSE 'ACCPAY' END
                          AND i.status = 'PAID' AND i.amount_due = 0
                          AND i.total - i.amount_paid = cn.total), ', ')
      INTO ambiguous
      FROM credit_notes cn
     WHERE cn.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND cn.status = 'PAID' AND cn.remaining_credit = 0
       AND NOT EXISTS (SELECT 1 FROM payments p WHERE p.credit_note_id = cn.credit_note_id);
    RAISE NOTICE 'credit notes awaiting allocation, each with its document count: %', ambiguous;

    -- (ii) Every one of the twelve payments named above now carries a bill, and
    -- it is the bill named.  Every OTHER ACCPAYPAYMENT still carries whatever
    -- it carried before: this migration sets invoice_id on twelve rows only.
    SELECT count(*) INTO unlinked
      FROM xr_cs_link k
     WHERE NOT EXISTS (SELECT 1 FROM payments p
                        WHERE p.payment_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/payment/' || k.n::text)
                          AND p.invoice_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || k.doc_key));
    IF unlinked <> 0 THEN
        RAISE EXCEPTION '% of the 12 named payments did not link to the named bill', unlinked;
    END IF;

    SELECT count(*) INTO pay_linked
      FROM payments
     WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
       AND payment_type = 'ACCPAYPAYMENT' AND invoice_id IS NOT NULL;
    IF pay_linked <> 24 THEN
        RAISE EXCEPTION 'expected 24 of 24 ACCPAYPAYMENT rows linked, found %', pay_linked;
    END IF;
    RAISE NOTICE 'ACCPAYPAYMENT: 24 of 24 linked (12 already, 12 repaired here)';

    -- The twelve bills are settled: their captured amount_paid is the cash, and
    -- for the three that a credit note also settled, cash + note = total.  A
    -- link to a bill whose outstanding amount is not what the payment carried
    -- would be the wrong bill.
    SELECT string_agg(k.n::text || ' -> ' || k.doc_key, ', ') INTO wrong_bill
      FROM xr_cs_link k
      JOIN payments p ON p.payment_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/payment/' || k.n::text)
      JOIN invoices i ON i.invoice_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || k.doc_key)
     WHERE i.amount_paid <> p.amount OR i.amount_due <> 0;
    IF wrong_bill IS NOT NULL THEN
        RAISE EXCEPTION 'linked to a bill whose captured amount_paid is not the payment: %', wrong_bill;
    END IF;

    -- (iii) The five allocations exist, and the documents they settle carry the
    -- note's total as credited.
    SELECT count(*) INTO allocs
      FROM xr_cs_alloc a
      JOIN credit_note_allocations ca
        ON ca.allocation_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/credit-note-allocation/' || a.cn_key || '/to/' || a.doc_key);
    IF allocs <> 5 THEN
        RAISE EXCEPTION 'expected 5 credit note allocations, found %', allocs;
    END IF;

    SELECT count(*) INTO uncredited
      FROM xr_cs_alloc a
      JOIN credit_notes cn ON cn.credit_note_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || a.cn_key)
      JOIN invoices i ON i.invoice_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || a.doc_key)
     WHERE i.amount_credited <> cn.total;
    IF uncredited <> 0 THEN
        RAISE EXCEPTION '% allocated documents do not carry the credit note total as amount_credited', uncredited;
    END IF;
    RAISE NOTICE 'credit note allocations: 5 written, 5 documents carry their note total as amount_credited';
END
$verify$;

-- The staging tables are ON COMMIT DROP, so the commit takes them with it.
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Reverses exactly the twelve links and the five allocations above, and nothing
-- else.  Every predicate names a row Up wrote together with the value Up gave
-- it, so a row another session has since changed -- a link re-pointed, a
-- credited amount raised -- no longer matches and is left alone rather than
-- reverted to a value it never held.  In the other direction nothing else in
-- the organisation is named: the twelve payments were the only rows with a NULL
-- invoice_id in this set, and the five allocation ids are fixed uuid_generate_v5
-- keys, so no row another session added can be swept up by either statement.
-- The link table is a CTE rather than a temp table so that a run of Up
-- immediately followed by Down in one session cannot see Up's staging table.
WITH link (n, doc_key) AS (VALUES
    (23, 'bill/Net Connect/2026-07-12/Rpt'),
    (25, 'bill/PowerDirect/2026-07-11/Rpt'),
    (16, 'bill/Net Connect/2026-08-12/Rpt'),
    (18, 'bill/PowerDirect/2026-08-11/Rpt'),
    (43, 'bill/Xero/2026-07-08/AP'),
    (42, 'bill/Xero/2026-08-08/AP'),
    (26, 'bill/Truxton Property Management/2026-07-11/RENT'),
    (20, 'bill/Truxton Property Management/2026-08-11/RENT'),
    (36, 'bill/Truxton Property Management/2026-08-31/RENT'),
    (24, 'bill/PC Complete/2026-07-21/'),
    (31, 'bill/Swanston Security/2026-07-21/AP'),
    (41, 'bill/Gateway Motors/2026-09-06/')
)
UPDATE payments p
   SET invoice_id = NULL
  FROM link k
 WHERE p.payment_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/payment/' || k.n::text)
   AND p.invoice_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || k.doc_key);

WITH alloc (cn_key, doc_key) AS (VALUES
    ('credit-note/CN-0015', 'invoice/INV-0005'),
    ('credit-note/CN-0014', 'invoice/INV-0010'),
    ('credit-note/CN-0023', 'invoice/INV-0022'),
    ('credit-note/OG laptop', 'bill/PC Complete/2026-07-21/'),
    ('credit-note/Refund', 'bill/Swanston Security/2026-07-21/AP')
)
UPDATE invoices i
   SET amount_credited = 0
  FROM alloc a
  JOIN credit_notes cn
    ON cn.credit_note_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || a.cn_key)
 WHERE i.invoice_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/document/' || a.doc_key)
   AND i.amount_credited = cn.total;

WITH alloc (cn_key, doc_key) AS (VALUES
    ('credit-note/CN-0015', 'invoice/INV-0005'),
    ('credit-note/CN-0014', 'invoice/INV-0010'),
    ('credit-note/CN-0023', 'invoice/INV-0022'),
    ('credit-note/OG laptop', 'bill/PC Complete/2026-07-21/'),
    ('credit-note/Refund', 'bill/Swanston Security/2026-07-21/AP')
)
DELETE FROM credit_note_allocations
 WHERE allocation_id IN (SELECT uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/credit-note-allocation/' || a.cn_key || '/to/' || a.doc_key)
                           FROM alloc a);
-- +goose StatementEnd
