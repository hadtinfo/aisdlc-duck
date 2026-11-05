CREATE TABLE account (
    id         VARCHAR(20)  PRIMARY KEY,
    owner_name VARCHAR(100) NOT NULL
);

CREATE TABLE customer (
    id   INT PRIMARY KEY,
    name VARCHAR(100)
);
