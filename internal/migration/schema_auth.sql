-- Version 2 adds server-side sign-in state and revocable sessions.
ALTER TABLE jobtracker.schema_versions DROP CONSTRAINT IF EXISTS schema_versions_version_check;
ALTER TABLE jobtracker.schema_versions ADD CONSTRAINT schema_versions_version_check CHECK (version BETWEEN 1 AND 2);
CREATE TABLE IF NOT EXISTS jobtracker.login_attempts (
    state_hash text PRIMARY KEY,
    browser_hash text NOT NULL,
    nonce text NOT NULL,
    verifier text NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS login_attempts_expiry ON jobtracker.login_attempts(expires_at);
CREATE TABLE IF NOT EXISTS jobtracker.sessions (
    token_hash text PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES jobtracker.users(id),
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry ON jobtracker.sessions(expires_at);
INSERT INTO jobtracker.schema_versions(version) VALUES(2) ON CONFLICT DO NOTHING;
