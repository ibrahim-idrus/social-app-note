CREATE TABLE note_shares (
    note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (note_id, user_id)
);
CREATE INDEX note_shares_user ON note_shares(user_id, note_id);
