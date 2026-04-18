-- +goose Up
-- +goose StatementBegin
-- The bcrypt in 00009 / fixtures 00001 did not verify against "admin123"; fix existing rows.
UPDATE users
SET password_hash = '$2a$10$pAimBhbqKiEBvTXqKhhWlOfbgNNFoa5o3GlGLR9EGxKh5hedcwUVK',
    updated_at = now()
WHERE email IN ('admin@demo.local', 'fixture-dev@goxero.test');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
