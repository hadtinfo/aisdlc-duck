-- +goose Up
-- Gộp Java V1..V6 thành schema cuối.

CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    login         text NOT NULL UNIQUE,
    display_name  text NOT NULL,
    email         text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    role          text NOT NULL DEFAULT 'dev' CHECK (role IN ('dev', 'lead'))
);

CREATE TABLE repos (
    id            text PRIMARY KEY,
    last_seen_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE features (
    repo_id             text NOT NULL,
    feature_key         text NOT NULL,
    snapshot            jsonb NOT NULL,
    captured_at         timestamptz NOT NULL,
    client_id           text NOT NULL,
    clock_skew          boolean NOT NULL DEFAULT false,
    first_sender_login  text NOT NULL,
    last_sender_login   text NOT NULL,
    synced_at           timestamptz NOT NULL DEFAULT now(),
    risk_level          text,
    risk_policy_version int,
    PRIMARY KEY (repo_id, feature_key)
);

CREATE TABLE events (
    id            bigserial PRIMARY KEY,
    repo_id       text NOT NULL,
    feature_key   text,
    type          text NOT NULL,
    at            timestamptz NOT NULL,
    step          text,
    path          text,
    out_of_scope  boolean NOT NULL DEFAULT false,
    sender_login  text NOT NULL,
    detail        text,
    received_at   timestamptz NOT NULL DEFAULT now(),
    session_id    text
);

CREATE INDEX events_repo_feature_at ON events (repo_id, feature_key, at);
CREATE INDEX events_at ON events (at);

CREATE TABLE tokens (
    id            bigserial PRIMARY KEY,
    user_id       bigint NOT NULL REFERENCES users(id),
    kind          text NOT NULL CHECK (kind IN ('login', 'ingest', 'approve', 'ci')),
    label         text NOT NULL,
    sha256        text NOT NULL UNIQUE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz,
    revoked_at    timestamptz
);
CREATE INDEX tokens_user ON tokens (user_id);

CREATE TABLE sessions (
    id_sha256   text PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users(id),
    csrf        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL
);
CREATE INDEX sessions_expires ON sessions (expires_at);

CREATE TABLE repo_settings (
    repo_id    text PRIMARY KEY,
    approvals  text NOT NULL CHECK (approvals IN ('local', 'web')),
    set_by     text NOT NULL,
    set_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE submissions (
    id            bigserial PRIMARY KEY,
    repo_id       text NOT NULL,
    feature_key   text NOT NULL,
    gate          text NOT NULL,
    hash          text NOT NULL,
    findings      jsonb NOT NULL,
    artifacts     jsonb NOT NULL,
    submitted_by  text NOT NULL,
    submitted_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX submissions_gate ON submissions (repo_id, feature_key, gate, id);

CREATE TABLE approvals (
    id                 bigserial PRIMARY KEY,
    submission_id      bigint NOT NULL REFERENCES submissions(id),
    repo_id            text NOT NULL,
    feature_key        text NOT NULL,
    gate               text NOT NULL,
    hash               text NOT NULL,
    approver           text NOT NULL,
    channel            text NOT NULL,
    at                 timestamptz NOT NULL DEFAULT now(),
    evaluated_tree     text,
    flags              jsonb NOT NULL DEFAULT '[]',
    assumptions_seen   jsonb NOT NULL DEFAULT '[]',
    original_approver  text,
    original_at        timestamptz,
    confirmed_at       timestamptz,
    revoked_at         timestamptz,
    revoked_by         text,
    rule               text,
    CONSTRAINT approvals_channel_check CHECK (channel IN ('web', 'terminal', 'import', 'auto', 'claude-code'))
);
CREATE INDEX approvals_live ON approvals (repo_id, feature_key, gate) WHERE revoked_at IS NULL;

CREATE TABLE feedback (
    id           bigserial PRIMARY KEY,
    repo_id      text NOT NULL,
    feature_key  text NOT NULL,
    gate         text NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('REJECTED', 'REOPENED')),
    by_login     text NOT NULL,
    at           timestamptz NOT NULL DEFAULT now(),
    comment      text NOT NULL
);

CREATE TABLE audit (
    id           bigserial PRIMARY KEY,
    at           timestamptz NOT NULL DEFAULT now(),
    login        text,
    ip           text,
    channel      text NOT NULL,
    action       text NOT NULL,
    repo_id      text,
    feature_key  text,
    target       text,
    detail       jsonb NOT NULL DEFAULT '{}'
);

-- +goose StatementBegin
CREATE FUNCTION audit_no_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit chỉ được thêm';
END
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_no_change BEFORE UPDATE OR DELETE ON audit
    FOR EACH ROW EXECUTE FUNCTION audit_no_change();
CREATE TRIGGER audit_no_truncate BEFORE TRUNCATE ON audit
    FOR EACH STATEMENT EXECUTE FUNCTION audit_no_change();

CREATE TABLE feature_control (
    repo_id      text NOT NULL,
    feature_key  text NOT NULL,
    paused       boolean NOT NULL DEFAULT false,
    paused_by    text,
    paused_at    timestamptz,
    PRIMARY KEY (repo_id, feature_key)
);

CREATE TABLE instructions (
    id           bigserial PRIMARY KEY,
    repo_id      text NOT NULL,
    feature_key  text NOT NULL,
    by_login     text NOT NULL,
    at           timestamptz NOT NULL DEFAULT now(),
    text         text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 2000),
    source       text NOT NULL CHECK (source IN ('web', 'feedback')),
    feedback_id  bigint REFERENCES feedback(id),
    delivered_at timestamptz
);
CREATE INDEX instructions_feature ON instructions (repo_id, feature_key, id);

CREATE TABLE pending_actions (
    id           bigserial PRIMARY KEY,
    repo_id      text NOT NULL,
    feature_key  text NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('ACCEPT_STEP', 'ALLOW_SCOPE')),
    step         text NOT NULL,
    path         text,
    end_tree     text,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    status       text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'APPLIED', 'STALE')),
    resolved_at  timestamptz,
    reason       text
);
CREATE INDEX pending_actions_feature ON pending_actions (repo_id, feature_key, id);

CREATE TABLE risk_policy_versions (
    repo_id     text NOT NULL,
    version     int NOT NULL,
    body        text NOT NULL,
    hash        text NOT NULL,
    author      text NOT NULL,
    reason      text NOT NULL,
    source      text NOT NULL CHECK (source IN ('import', 'web', 'preset-update')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, version)
);

-- +goose Down
-- Không hỗ trợ lùi.
