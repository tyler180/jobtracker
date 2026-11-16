# Archive migration rehearsal

The running web application still uses its JSON archive. The separate
`jobtracker-migrate` command prepares PostgreSQL storage; it does not enable
authentication, redirect traffic, change Kubernetes, or contact Authentik.

## Data model and guarantees

- `jobtracker.users` has an internal UUID and a unique `(issuer, subject)` identity.
  Use the exact issuer and subject from a validated OIDC login. Do not substitute
  an email, username, or Authentik's numeric administrative user ID.
- `jobtracker.jobs` keys each job by `(owner_id, id)`. Different people can save
  the same posting independently. The whole document is stored in JSONB, including
  unknown fields, dates, descriptions, notes, milestones, and original saved time.
  `source_json` also retains the exact original bytes for reconciliation.
- `jobtracker.archive_imports` binds a successfully imported backup checksum to
  its owner. This prevents assigning the same backup to a different account.
- An import is one transaction: optional schema creation, user creation, jobs,
  reconciliation, and the completion record commit together. The migration tool
  serializes its own runs with a transaction advisory lock. Source files are never
  extracted or written. Malformed archives, mismatched checksums, duplicate keys,
  ID mismatches, and unexpected files fail validation.
- An unchanged rerun inserts zero duplicate records and verifies every record
  again. Conflicting records fail the entire import without overwriting data.
  Rerunning a completed backup also refuses to recreate subsequently deleted jobs.
  An unknown commit outcome is reported as an error; rerun the same backup and
  identity to reconcile. A completion marker alone is not proof of correctness.

The application now has a separate PostgreSQL backend and OIDC middleware. Its
owner-scoped repository reads the principal from a verified server session. All
HTTP routes use this repository in PostgreSQL mode. The original `Records` layer
remains a migration test helper. SQL ownership filters do not protect against a
caller possessing the migration/admin database credentials.

## Reproduce the isolated rehearsal

Requires Go 1.26.11+, Docker, Bash, and Python 3. Use an absolute backup path and its
SHA-256 from the separately verified backup report. Choose a new report folder
under `.local-backups`, which is ignored by Git:

```sh
bash scripts/test-archive-migration.sh \
  /absolute/path/to/archive.tar.gz \
  EXPECTED_BACKUP_SHA256 \
  /absolute/path/to/.local-backups/migration-rehearsal-unique-name
```

The script starts digest-pinned PostgreSQL 18 with a random loopback-only host
port and a disposable volume. It uses local trust authentication only for this
temporary database. It runs the full race test suite, PostgreSQL integration
tests against synthetic fixtures and the real backup, vet, a CLI build, and first
and repeat CLI imports. It saves aggregate evidence and removes the container and
its data volume on exit. The integration test refuses remote hosts or database
names that do not end in `_test`, and drops only the disposable test schema.

The tests check exact original bytes and all JSON fields, repeat imports,
conflict rollback after an earlier insert, incorrect owner rebinding, second-user
access attempts for every real record, same posting IDs owned by different
users, and preservation of edits/deletions against stale backup reruns. No real
identity or credentials are required. Reports contain counts and checksums, not
job descriptions or notes. Protect the backup itself as personal data.

## Command usage

Without `-apply`, the command only validates and summarizes a backup:

```sh
go run ./cmd/jobtracker-migrate \
  -archive /absolute/path/to/archive.tar.gz -sha256 EXPECTED_BACKUP_SHA256
```

To import into an explicitly selected database, set `MIGRATION_DATABASE_URL`
securely in the process environment. Use `-apply -apply-schema` on the first run
and `-apply` on subsequent runs, with `-issuer` and `-subject` supplied explicitly.
To initialize an empty database before any account signs in, use
`go run ./cmd/jobtracker-migrate -init-schema -apply` with
`MIGRATION_DATABASE_URL` set to the schema owner connection. This creates schema
version 2 without an archive or owner. Repeating it is safe.
The web server never runs this schema initialization on startup. Do not place
credentials in command flags or tracked configuration.

## Before production cutover

Local tests validate the import plus signed OIDC login and HTTP ownership, using a
test provider and a restricted database role. They do not validate the live
Authentik provider, NAS connectivity, or a deployed cutover.
Remaining work:

1. Configure the live Authentik OIDC provider and confirm its enrollment/access
   policy. Verify the intended owner's issuer/subject using `/auth/me` after a real
   sign-in on an isolated staging instance. Never grant legacy data to whichever
   user signs in first.
2. Validate the deployed sign-in and HTTP ownership behavior in staging with two
   real accounts. Keep the separate fictional read-only demo.
3. Provision dedicated NAS database roles: a schema/migration owner and a runtime
   role limited to required data operations. Configure secrets and connectivity.
4. Rehearse restore/rollback and the actual deployment configuration. Stop archive
   writes for the final backup/import so later JSON edits cannot be lost. Compare
   all records and counts before switching traffic.
5. Keep the old archive and its backup. A rollback after accepting new PostgreSQL
   writes needs reconciliation/export of those writes; restarting the old JSON
   app alone would lose them. This tool is not a continuous two-way sync.

PostgreSQL target rows may have additional records belonging to the same owner;
verification covers every source record, not an assumption that the whole target
is empty. For the final initial cutover, independently assert the expected target
owner count and ensure no other writers are running.
