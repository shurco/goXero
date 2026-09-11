-- +goose Up
-- +goose StatementBegin
-- Xero's "Discuss": a note left against a statement line, which is where the
-- question "why is this line coded like this?" gets answered while the line is
-- still in the inbox. The author's name is copied rather than joined so the
-- note survives the user being removed from the organisation.
CREATE TABLE bank_statement_line_comments (
    comment_id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id   UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    statement_line_id UUID NOT NULL REFERENCES bank_statement_lines(statement_line_id) ON DELETE CASCADE,
    user_id           UUID REFERENCES users(user_id) ON DELETE SET NULL,
    author_name       VARCHAR(255),
    body              TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bank_stmt_line_comments_line
    ON bank_statement_line_comments(statement_line_id, created_at);
-- +goose StatementEnd

-- +goose StatementBegin
-- Auto-reconcile, in Xero's two halves: which lines the button reconciled by
-- itself, and whether this account reconciles on import without being asked.
-- `auto_reconciled_at` is stamped by AutoReconcile alone, so the count the
-- banner reports is a fact about what happened rather than an inference from
-- the line looking reconciled.
ALTER TABLE bank_statement_lines ADD COLUMN auto_reconciled_at TIMESTAMPTZ;
ALTER TABLE accounts ADD COLUMN auto_reconcile BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE accounts DROP COLUMN IF EXISTS auto_reconcile;
ALTER TABLE bank_statement_lines DROP COLUMN IF EXISTS auto_reconciled_at;
DROP TABLE IF EXISTS bank_statement_line_comments;
-- +goose StatementEnd
