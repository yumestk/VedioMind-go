-- +goose Up
CREATE TABLE analysis_jobs (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 media_id bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 owner_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 snapshot_version integer NOT NULL,
 pipeline_version integer NOT NULL,
 stage text NOT NULL DEFAULT 'SUMMARY',
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING','RUNNING','RETRY_WAIT','SUCCEEDED','FAILED')),
 lease_owner text,
 lease_version bigint NOT NULL DEFAULT 0,
 lease_until timestamptz,
 claims integer NOT NULL DEFAULT 0,
 calls jsonb NOT NULL DEFAULT '{}',
 total_calls integer NOT NULL DEFAULT 0,
 next_run_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 started_at timestamptz,
 deadline_at timestamptz,
 error_code text,
 error_message text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX analysis_active_media ON analysis_jobs(media_id) WHERE status IN ('PENDING','RUNNING','RETRY_WAIT');
CREATE INDEX analysis_runnable ON analysis_jobs(next_run_at) WHERE status IN ('PENDING','RETRY_WAIT');
CREATE TABLE analysis_artifacts (
 media_id bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 snapshot_version integer NOT NULL,
 kind text NOT NULL,
 processor_version integer NOT NULL,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(media_id,snapshot_version,kind,processor_version)
);
-- +goose Down
DROP TABLE analysis_artifacts, analysis_jobs;
