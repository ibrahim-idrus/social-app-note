CREATE TABLE instagram_integrations (
    id TEXT PRIMARY KEY CHECK (id = 'prototype'),
    instagram_user_id TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    access_token TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO instagram_integrations (id, instagram_user_id, username, status)
VALUES ('prototype', '17841400000000000', 'notedesk_inbox', 'active');

ALTER TABLE social_identities ADD COLUMN verification_code_hash BLOB;
ALTER TABLE social_identities ADD COLUMN verification_expires_at TEXT;
ALTER TABLE social_identities ADD COLUMN verification_consumed_at TEXT;
