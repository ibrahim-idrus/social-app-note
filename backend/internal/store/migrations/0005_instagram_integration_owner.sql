ALTER TABLE instagram_integrations
ADD COLUMN owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
