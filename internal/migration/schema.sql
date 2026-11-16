-- Applied explicitly by jobtracker-migrate, never by the web server.
CREATE SCHEMA IF NOT EXISTS jobtracker;
CREATE TABLE IF NOT EXISTS jobtracker.schema_versions (
    version integer PRIMARY KEY CHECK (version = 1)
);
CREATE TABLE IF NOT EXISTS jobtracker.users (
    id uuid PRIMARY KEY,
    issuer text NOT NULL CHECK (issuer <> ''),
    subject text NOT NULL CHECK (subject <> ''),
    UNIQUE (issuer, subject)
);
CREATE TABLE IF NOT EXISTS jobtracker.jobs (
    owner_id uuid NOT NULL REFERENCES jobtracker.users(id),
    id text NOT NULL CHECK (id ~ '^[0-9a-f]{64}$'),
    document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object' AND document->>'id' IS NOT NULL AND document->>'id' = id),
    source_json bytea NOT NULL,
    PRIMARY KEY (owner_id, id)
);
-- A particular backup cannot accidentally be imported under another account.
CREATE TABLE IF NOT EXISTS jobtracker.archive_imports (
    archive_sha256 text PRIMARY KEY CHECK (archive_sha256 ~ '^[0-9a-f]{64}$'),
    owner_id uuid NOT NULL REFERENCES jobtracker.users(id),
    record_count integer NOT NULL CHECK (record_count > 0),
    completed_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO jobtracker.schema_versions(version) VALUES (1) ON CONFLICT DO NOTHING;
