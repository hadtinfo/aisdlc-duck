-- +goose Up
CREATE TABLE daily_limit (
    account_id   VARCHAR(20) PRIMARY KEY REFERENCES account (id),
    limit_amount BIGINT      NOT NULL,
    used_amount  BIGINT      NOT NULL DEFAULT 0
);
