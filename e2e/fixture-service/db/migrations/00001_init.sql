-- +goose Up
CREATE TABLE account (
    id         VARCHAR(20)  PRIMARY KEY,
    owner_name VARCHAR(100) NOT NULL
);
