-- +goose Up
CREATE TABLE agent_workflows (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    root TEXT NOT NULL,
    state TEXT NOT NULL CHECK(length(state) <= 262144)
);

-- +goose Down
DROP TABLE agent_workflows;
