-- +goose Up
-- +goose StatementBegin
-- Items endpoint - products or inventory.
CREATE TABLE items (
    item_id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    code            VARCHAR(30) NOT NULL,
    name            VARCHAR(100),
    description     TEXT,
    purchase_description TEXT,
    is_tracked_as_inventory BOOLEAN NOT NULL DEFAULT FALSE,
    is_sold         BOOLEAN NOT NULL DEFAULT TRUE,
    is_purchased    BOOLEAN NOT NULL DEFAULT TRUE,
    inventory_asset_account_code VARCHAR(10),
    quantity_on_hand NUMERIC(18,4) NOT NULL DEFAULT 0,
    -- Sales details
    sales_unit_price       NUMERIC(18,4),
    sales_account_code     VARCHAR(10),
    sales_tax_type         VARCHAR(50),
    -- Purchase details
    purchase_unit_price    NUMERIC(18,4),
    purchase_account_code  VARCHAR(10),
    purchase_tax_type      VARCHAR(50),
    total_cost_pool        NUMERIC(18,4),
    updated_date_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(organisation_id, code)
);

CREATE INDEX idx_items_organisation_id ON items(organisation_id);
CREATE INDEX idx_items_code ON items(organisation_id, code);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS items;
-- +goose StatementEnd
