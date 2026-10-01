CREATE TABLE instagram_note_events_repaired (
    external_message_id TEXT PRIMARY KEY,
    note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE
);

INSERT INTO instagram_note_events_repaired (external_message_id, note_id)
SELECT external_message_id, note_id FROM instagram_note_events;

DROP TABLE instagram_note_events;
ALTER TABLE instagram_note_events_repaired RENAME TO instagram_note_events;
