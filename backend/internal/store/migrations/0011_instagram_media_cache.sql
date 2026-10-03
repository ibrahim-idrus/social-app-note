CREATE TABLE instagram_media_cache (
    note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    attachment_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('fetching','available','failed')),
    media_json TEXT,
    attempted_at TEXT NOT NULL,
    cached_at TEXT,
    PRIMARY KEY(note_id, attachment_key)
);
