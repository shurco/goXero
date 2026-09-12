-- +goose Up
-- +goose StatementBegin
-- Per-connection secrets and incremental sync state.
--
-- GoCardless authenticates with an application-level secret_id/secret_key, so a
-- connection only ever needed its opaque requisition id. Plaid authenticates per
-- Item with an access_token issued during consent, which is a bearer credential
-- for the user's bank data — it is sealed with AES-256-GCM (BANKFEED_ENCRYPTION_KEY)
-- before it touches the database, so `external_secret` is ciphertext, never a
-- usable token.
--
-- `sync_cursor` belongs to the connection rather than the account: Plaid's
-- /transactions/sync cursor tracks an Item, so the same cursor covers every
-- account under one consent.
ALTER TABLE bank_feed_connections
    ADD COLUMN external_secret BYTEA,
    ADD COLUMN sync_cursor     TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE bank_feed_connections
    DROP COLUMN IF EXISTS sync_cursor,
    DROP COLUMN IF EXISTS external_secret;
-- +goose StatementEnd
