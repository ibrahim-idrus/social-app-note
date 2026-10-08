CREATE TABLE social_message_sync_queue (
    id INTEGER PRIMARY KEY,
    social_identity_id INTEGER NOT NULL REFERENCES social_identities(id) ON DELETE CASCADE,
    platform TEXT NOT NULL CHECK (platform IN ('instagram','facebook')),
    external_message_id TEXT NOT NULL,
    provider_sent_at TEXT NOT NULL,
    sender_id TEXT NOT NULL,
    recipient_id TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    attachments_json TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','failed')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error_code TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(platform, external_message_id)
);
CREATE INDEX social_message_sync_queue_drain ON social_message_sync_queue(social_identity_id, provider_sent_at, external_message_id);
CREATE TABLE social_message_sync_locks (
    social_identity_id INTEGER PRIMARY KEY REFERENCES social_identities(id) ON DELETE CASCADE,
    claimed_at TEXT NOT NULL,
    lease_expires_at TEXT NOT NULL
);
