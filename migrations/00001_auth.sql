-- +goose Up
CREATE TABLE users (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 email text NOT NULL UNIQUE,
 password_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE rate_limit_counters (
 scope text NOT NULL,
 window_start bigint NOT NULL,
 count integer NOT NULL,
 PRIMARY KEY(scope,window_start)
);
-- +goose Down
DROP TABLE rate_limit_counters, sessions, users;
