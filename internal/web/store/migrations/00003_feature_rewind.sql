-- +goose Up
-- Rewind: đóng "lần làm" hiện tại của một tính năng. Bản ghi cũ giữ nguyên, đánh dấu rewind_id để lưu trữ.
CREATE TABLE feature_rewinds (
    id           bigserial PRIMARY KEY,
    repo_id      text NOT NULL,
    feature_key  text NOT NULL,
    from_attempt int,
    to_attempt   int,
    checkpoint   text,
    by_login     text,
    at           timestamptz DEFAULT now(),
    snapshot     jsonb,
    request_id   text NOT NULL,
    UNIQUE (repo_id, feature_key, request_id)
);

ALTER TABLE features ADD COLUMN attempt int NOT NULL DEFAULT 1;
ALTER TABLE submissions ADD COLUMN rewind_id bigint NULL REFERENCES feature_rewinds(id);
ALTER TABLE feedback ADD COLUMN rewind_id bigint NULL REFERENCES feature_rewinds(id);

ALTER TABLE feedback DROP CONSTRAINT feedback_kind_check;
ALTER TABLE feedback ADD CONSTRAINT feedback_kind_check CHECK (kind IN ('REJECTED', 'REOPENED', 'REWOUND'));

ALTER TABLE pending_actions DROP CONSTRAINT pending_actions_status_check;
ALTER TABLE pending_actions ADD CONSTRAINT pending_actions_status_check
    CHECK (status IN ('PENDING', 'APPLIED', 'STALE', 'CANCELLED'));

-- +goose Down
ALTER TABLE pending_actions DROP CONSTRAINT pending_actions_status_check;
ALTER TABLE pending_actions ADD CONSTRAINT pending_actions_status_check CHECK (status IN ('PENDING', 'APPLIED', 'STALE'));
DELETE FROM feedback WHERE kind = 'REWOUND';
ALTER TABLE feedback DROP CONSTRAINT feedback_kind_check;
ALTER TABLE feedback ADD CONSTRAINT feedback_kind_check CHECK (kind IN ('REJECTED', 'REOPENED'));
ALTER TABLE feedback DROP COLUMN rewind_id;
ALTER TABLE submissions DROP COLUMN rewind_id;
ALTER TABLE features DROP COLUMN attempt;
DROP TABLE feature_rewinds;
