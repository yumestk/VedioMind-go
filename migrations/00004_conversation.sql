-- +goose Up
CREATE TABLE conversations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 media_id bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 owner_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 snapshot_version integer NOT NULL,
 revision bigint NOT NULL DEFAULT 0,
 occupancy_token text,
 occupancy_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE question_requests (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 owner_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 request_key text NOT NULL,
 fingerprint text NOT NULL,
 media_id bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 conversation_id bigint REFERENCES conversations(id) ON DELETE CASCADE,
 question text NOT NULL,
 status text NOT NULL CHECK(status IN ('RUNNING','FAILED','SUCCEEDED')),
 attempt_token text,
 expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(owner_user_id,request_key)
);
CREATE TABLE messages (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 conversation_id bigint NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 question_request_id bigint NOT NULL REFERENCES question_requests(id) ON DELETE CASCADE,
 role text NOT NULL CHECK(role IN ('user','assistant')),
 content text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(question_request_id,role)
);
CREATE TABLE citations (
 message_id bigint NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
 ordinal integer NOT NULL,
 media_id bigint NOT NULL,
 snapshot_version integer NOT NULL,
 segment_id integer NOT NULL,
 text text NOT NULL,
 start_ms bigint NOT NULL,
 end_ms bigint NOT NULL,
 PRIMARY KEY(message_id,ordinal),
 FOREIGN KEY(media_id,snapshot_version,segment_id) REFERENCES transcript_segments(media_id,snapshot_version,id) ON DELETE CASCADE
);
CREATE INDEX messages_conversation ON messages(conversation_id,id);
CREATE INDEX conversations_media ON conversations(media_id,id);
-- +goose Down
DROP TABLE citations, messages, question_requests, conversations;
