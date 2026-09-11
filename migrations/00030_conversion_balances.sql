-- +goose Up
-- Conversion balances: the opening balances an organisation brings in when it
-- moves to goXero, and the date they are stated as at.
--
-- WHY THIS EXISTS.  The screen at /app/settings/conversion-balances collected
-- the balances, the conversion date and the lock, and then saved none of them:
-- its Save handler waited 300 ms and raised an alert saying persistence needed
-- a dedicated endpoint that did not exist.  The two columns below and the
-- journal the screen now owns are that endpoint's storage.
--
-- WHY THE BALANCES HAVE NO TABLE OF THEIR OWN.  An opening balance is not a
-- second fact standing beside the ledger -- it IS a posting, and the only place
-- it can reach a Trial Balance or a Balance Sheet is gl_journal_lines.  A
-- conversion_balances table would hold the same figures a second time, free to
-- drift from the ledger they are meant to be.  So the balances are the lines of
-- one gl_journals row per organisation, identified by
-- (source_type='CONVERSIONBALANCE', source_id=organisation_id): a key naming
-- the single journal the screen owns, so saving replaces it instead of standing
-- a second set of opening balances next to the first.
--
-- The date and the lock ARE new state -- they describe the journal without
-- being part of it -- and they sit on the organisation, with the rest of its
-- financial settings.  A NULL conversion_date means the organisation has not
-- converted: it has no opening balances to state.
--
-- THE REFERENCE ORGANISATION ALREADY HAS ITS CONVERSION BALANCE, and this
-- migration hands it to the screen rather than letting the screen post a second
-- one beside it.  Its opening balance is journal 388 "Conversion Balance" of
-- 21 Jun 2026, which 00024 posted from
-- migrations/data/xero/opening-balances.csv (Dr 090 4,130.98 / Cr 840
-- 4,130.98).  That journal is re-sourced below to the key the screen edits, and
-- the date it states becomes the organisation's conversion date.  No amount
-- moves, so every report keeps printing Xero's own figures -- which
-- internal/handlers/reports_parity_integration_test.go checks to the cent.
-- +goose StatementBegin
ALTER TABLE organisations
    ADD COLUMN conversion_date DATE,
    ADD COLUMN conversion_balances_locked BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE gl_journals
   SET source_type = 'CONVERSIONBALANCE',
       source_id   = organisation_id
 WHERE journal_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/opening-balance/journal/388');

UPDATE organisations o
   SET conversion_date = j.journal_date
  FROM gl_journals j
 WHERE j.organisation_id = o.organisation_id
   AND j.source_type = 'CONVERSIONBALANCE'
   AND j.source_id = o.organisation_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE gl_journals
   SET source_type = 'MANUALJOURNAL',
       source_id   = NULL
 WHERE journal_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/gl/opening-balance/journal/388');

ALTER TABLE organisations
    DROP COLUMN conversion_date,
    DROP COLUMN conversion_balances_locked;
-- +goose StatementEnd
