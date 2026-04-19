-- +goose Up
-- +goose StatementBegin
CREATE TABLE bank_rules (
    bank_rule_id    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organisation_id UUID NOT NULL REFERENCES organisations(organisation_id) ON DELETE CASCADE,
    rule_type       VARCHAR(20) NOT NULL CHECK (rule_type IN ('SPEND', 'RECEIVE', 'TRANSFER')),
    name            VARCHAR(255) NOT NULL,
    definition      JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bank_rules_org ON bank_rules(organisation_id);
CREATE INDEX idx_bank_rules_org_type ON bank_rules(organisation_id, rule_type);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bank_rules;
-- +goose StatementEnd
