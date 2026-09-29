ALTER TABLE notes ADD COLUMN instagram_attachments_json TEXT;

CREATE TABLE instagram_note_events (
    external_message_id TEXT PRIMARY KEY,
    note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE
);
