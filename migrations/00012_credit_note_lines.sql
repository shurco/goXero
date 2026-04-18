-- +goose Up
-- +goose StatementBegin
-- Line items for credit notes were missing in 00006; add them now so the
-- CreditNotes API can persist `LineItems[]` exactly like invoices do.
CREATE TABLE IF NOT EXISTS credit_note_line_items (
    line_item_id    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    credit_note_id  UUID NOT NULL REFERENCES credit_notes(credit_note_id) ON DELETE CASCADE,
    sort_order      INT NOT NULL DEFAULT 0,
    description     TEXT,
    quantity        NUMERIC(18,4) NOT NULL DEFAULT 1,
    unit_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    item_code       VARCHAR(30),
    account_code    VARCHAR(10),
    tax_type        VARCHAR(50),
    tax_amount      NUMERIC(18,4) NOT NULL DEFAULT 0,
    line_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    discount_rate   NUMERIC(9,4),
    discount_amount NUMERIC(18,4)
);

CREATE INDEX IF NOT EXISTS idx_credit_note_line_items_credit_note_id
    ON credit_note_line_items(credit_note_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS credit_note_line_items;
-- +goose StatementEnd
