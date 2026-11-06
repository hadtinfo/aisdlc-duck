-- +goose Up
ALTER TABLE transfer ADD COLUMN note VARCHAR(200);
