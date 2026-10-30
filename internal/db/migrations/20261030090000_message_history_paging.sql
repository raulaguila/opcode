-- +goose Up
CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages(session_id,created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_messages_session_created;
