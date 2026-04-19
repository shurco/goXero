-- +goose Up
-- +goose StatementBegin
-- Files inbox: organisation-level attachments (subject_type ORGFILE, subject_id = organisation_id).
ALTER TABLE attachments ADD COLUMN IF NOT EXISTS file_folder VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_attachments_orgfile_folder
    ON attachments (organisation_id, file_folder)
    WHERE subject_type = 'ORGFILE';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_attachments_orgfile_folder;
ALTER TABLE attachments DROP COLUMN IF EXISTS file_folder;
-- +goose StatementEnd
