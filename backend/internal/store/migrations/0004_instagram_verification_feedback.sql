ALTER TABLE social_identities ADD COLUMN verification_result TEXT NOT NULL DEFAULT 'waiting'
    CHECK (verification_result IN ('waiting', 'invalid_code', 'expired', 'system_failure'));
ALTER TABLE social_identities ADD COLUMN verification_result_at TEXT;

CREATE TABLE instagram_verification_replies (
    external_message_id TEXT PRIMARY KEY,
    social_identity_id INTEGER NOT NULL REFERENCES social_identities(id) ON DELETE CASCADE,
    attempted_at TEXT,
    result TEXT NOT NULL CHECK (result IN ('claimed', 'sent', 'system_failure'))
);
