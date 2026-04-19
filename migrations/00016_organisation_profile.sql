-- +goose Up
-- +goose StatementBegin
-- Extended organisation details for settings UI (addresses, phones, social, invoice display prefs).
ALTER TABLE organisations ADD COLUMN IF NOT EXISTS description TEXT;
ALTER TABLE organisations ADD COLUMN IF NOT EXISTS profile JSONB NOT NULL DEFAULT '{}'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE organisations DROP COLUMN IF EXISTS profile;
ALTER TABLE organisations DROP COLUMN IF EXISTS description;
-- +goose StatementEnd
