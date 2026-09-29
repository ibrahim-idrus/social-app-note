ALTER TABLE social_identities RENAME TO social_identities_before_facebook;
ALTER TABLE notes RENAME TO notes_before_facebook;
ALTER TABLE instagram_verification_replies RENAME TO instagram_verification_replies_before_facebook;

CREATE TABLE social_identities (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform TEXT NOT NULL CHECK (platform IN ('instagram', 'facebook')),
    platform_user_id TEXT,
    username TEXT NOT NULL,
    normalized_username TEXT NOT NULL,
    display_name TEXT,
    avatar_url TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'disabled')),
    verified_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    verification_code_hash BLOB,
    verification_expires_at TEXT,
    verification_consumed_at TEXT,
    verification_result TEXT NOT NULL DEFAULT 'waiting'
        CHECK (verification_result IN ('waiting', 'invalid_code', 'expired', 'system_failure')),
    verification_result_at TEXT,
    UNIQUE (user_id, platform)
);

CREATE UNIQUE INDEX social_identities_instagram_username
ON social_identities(normalized_username) WHERE platform = 'instagram';
CREATE UNIQUE INDEX social_identities_platform_user_id
ON social_identities(platform, platform_user_id) WHERE platform_user_id IS NOT NULL;

CREATE TABLE notes (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    social_identity_id INTEGER REFERENCES social_identities(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    content_markdown TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('manual', 'instagram', 'facebook')),
    external_message_id TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    instagram_attachments_json TEXT,
    UNIQUE (source, external_message_id)
);

CREATE TABLE instagram_verification_replies (
    external_message_id TEXT PRIMARY KEY,
    social_identity_id INTEGER NOT NULL REFERENCES social_identities(id) ON DELETE CASCADE,
    attempted_at TEXT,
    result TEXT NOT NULL CHECK (result IN ('claimed', 'sent', 'system_failure'))
);

INSERT INTO social_identities (
    id, user_id, platform, platform_user_id, username, normalized_username,
    display_name, avatar_url, status, verified_at, created_at, updated_at,
    verification_code_hash, verification_expires_at, verification_consumed_at,
    verification_result, verification_result_at
)
SELECT id, user_id, platform, platform_user_id, username, normalized_username,
    display_name, avatar_url, status, verified_at, created_at, updated_at,
    verification_code_hash, verification_expires_at, verification_consumed_at,
    verification_result, verification_result_at
FROM social_identities_before_facebook;
INSERT INTO notes (
    id, user_id, social_identity_id, title, content_markdown, source,
    external_message_id, created_at, updated_at, instagram_attachments_json
)
SELECT id, user_id, social_identity_id, title, content_markdown, source,
    external_message_id, created_at, updated_at, instagram_attachments_json
FROM notes_before_facebook;
INSERT INTO instagram_verification_replies (external_message_id, social_identity_id, attempted_at, result)
SELECT external_message_id, social_identity_id, attempted_at, result
FROM instagram_verification_replies_before_facebook;

DROP TABLE instagram_verification_replies_before_facebook;
DROP TABLE notes_before_facebook;
DROP TABLE social_identities_before_facebook;

CREATE INDEX notes_user_created_at ON notes(user_id, created_at DESC);

CREATE TABLE facebook_integrations (
    id TEXT PRIMARY KEY CHECK (id = 'configured_page'),
    page_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE facebook_message_receipts (
    external_message_id TEXT PRIMARY KEY,
    page_id TEXT NOT NULL,
    psid TEXT NOT NULL,
    received_at TEXT NOT NULL
);
