CREATE TABLE facebook_verification_replies (
    external_message_id TEXT PRIMARY KEY,
    social_identity_id INTEGER NOT NULL REFERENCES social_identities(id) ON DELETE CASCADE,
    attempted_at TEXT,
    result TEXT NOT NULL CHECK (result IN ('claimed', 'sent', 'system_failure'))
);
