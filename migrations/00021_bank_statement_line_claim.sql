-- +goose Up
-- +goose StatementBegin
-- A statement line is claimed while a request turns it into a bank transaction,
-- so a double-submit cannot post the same line twice. `claimed_at` is what makes
-- that claim surrenderable: a request killed mid-flight must not strand the line
-- out of the inbox for good, so a claim older than the staleness window may be
-- taken over by the next caller.
ALTER TABLE bank_statement_lines ADD COLUMN claimed_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE bank_statement_lines DROP COLUMN IF EXISTS claimed_at;
-- +goose StatementEnd
