-- +goose Up
-- Ngôn ngữ giao diện web người dùng đã chọn; NULL = chưa chọn (UI theo localStorage / trình duyệt).
ALTER TABLE users ADD COLUMN locale text CHECK (locale IN ('vi', 'en'));

-- +goose Down
ALTER TABLE users DROP COLUMN locale;
