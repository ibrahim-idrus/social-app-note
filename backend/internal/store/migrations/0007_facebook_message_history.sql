ALTER TABLE facebook_message_receipts ADD COLUMN message_text TEXT NOT NULL DEFAULT '';
ALTER TABLE facebook_message_receipts ADD COLUMN status TEXT NOT NULL DEFAULT 'ignored'
    CHECK (status IN ('note_created', 'activated', 'ignored', 'duplicate', 'error', 'expired', 'invalid_code'));
ALTER TABLE facebook_message_receipts ADD COLUMN note_id INTEGER REFERENCES notes(id) ON DELETE SET NULL;
