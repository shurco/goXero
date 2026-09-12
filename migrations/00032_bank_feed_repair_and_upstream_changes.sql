-- +goose Up
-- +goose StatementBegin
-- Two things the feed could not express before.
--
-- `session_ref` is the consent session we are currently waiting on — Plaid calls
-- it a link token, GoCardless a requisition id. It needs a column of its own
-- because `external_reference` cannot play that part: that starts life as the
-- session handle too, but consent overwrites it with the durable id (Plaid's
-- item_id), and Plaid's SESSION_FINISHED webhook arrives out of band knowing
-- nothing but the session handle. A set value therefore also means "a session is
-- in flight", which is what a completion is looked up by — not what tells a
-- first-time link from a repair, which is `external_reference` and
-- `external_secret` being set (the consent is already durable, so the session is
-- Plaid update mode repairing it).
ALTER TABLE bank_feed_connections
    ADD COLUMN session_ref TEXT,
    -- The institution's country, as the user searched for it. Kept because a
    -- repair session has to name it again (Plaid checks country_codes against
    -- the Item) and because the connections screen shows it.
    ADD COLUMN country VARCHAR(2);

-- What the bank says about a line that has already been coded.
--
-- By the time a bank restates or withdraws a transaction, the line has been
-- turned into a bank transaction and is part of the books — so the feed neither
-- rewrites it nor deletes it out from under the ledger. It parks the bank's
-- version here instead and leaves the difference for the user to reconcile.
-- `upstream_change` itself is derived when the line is read (by comparing these
-- figures with what was booked), so the notice clears itself the moment the two
-- agree again, and needs no flag to keep in step.
ALTER TABLE bank_statement_lines
    ADD COLUMN upstream_amount     NUMERIC(18,4), -- the bank's version of the amount
    ADD COLUMN upstream_posted_at  DATE,          -- and of the date it posted
    ADD COLUMN upstream_changed_at TIMESTAMPTZ,   -- when that version last changed
    -- Set when the bank withdrew a line we had already booked. Unlike the
    -- figures above this one cannot be derived from anything, so it is stored —
    -- and can be dismissed by the user, because the posted twin arriving as a
    -- fresh line is exactly what makes the withdrawal a non-event.
    ADD COLUMN upstream_removed_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE bank_statement_lines
    DROP COLUMN IF EXISTS upstream_removed_at,
    DROP COLUMN IF EXISTS upstream_changed_at,
    DROP COLUMN IF EXISTS upstream_posted_at,
    DROP COLUMN IF EXISTS upstream_amount;

ALTER TABLE bank_feed_connections
    DROP COLUMN IF EXISTS country,
    DROP COLUMN IF EXISTS session_ref;
-- +goose StatementEnd
