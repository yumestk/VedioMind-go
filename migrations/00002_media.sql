-- +goose Up
CREATE TABLE media (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 owner_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 platform text NOT NULL CHECK(platform IN ('BILIBILI','YOUTUBE')),
 external_id text NOT NULL,
 canonical_url text NOT NULL,
 title text NOT NULL,
 duration_ms bigint NOT NULL CHECK(duration_ms >= 0),
 source text NOT NULL,
 language text NOT NULL,
 snapshot_version integer NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(owner_user_id,platform,external_id)
);
CREATE TABLE transcript_segments (
 media_id bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 snapshot_version integer NOT NULL,
 id integer NOT NULL CHECK(id>0),
 position integer NOT NULL CHECK(position>=0),
 start_ms bigint NOT NULL CHECK(start_ms>=0),
 end_ms bigint NOT NULL CHECK(end_ms>=start_ms),
 text text NOT NULL CHECK(length(text)>0),
 PRIMARY KEY(media_id,snapshot_version,id),
 UNIQUE(media_id,position)
);
-- +goose Down
DROP TABLE transcript_segments, media;
