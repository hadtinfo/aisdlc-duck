-- +goose Up
CREATE TABLE transfer (
    id           BIGSERIAL PRIMARY KEY,
    from_account VARCHAR(32) NOT NULL,
    to_account   VARCHAR(32) NOT NULL,
    amount       BIGINT      NOT NULL
);
