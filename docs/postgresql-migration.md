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

1. The live production Authentik OIDC provider and Jobtracker application have been
   created with a strict production callback and authorization-code grant. Access is
   bound only to the intended owner. The provider discovery endpoint responds, but
   the production provider identity is verified as documented below. Keep
   production on JSON until that identity is confirmed; never grant legacy data to
   whichever user signs in first.
2. Staging sign-in and HTTP ownership behavior were validated with two accounts.
   Keep the separate fictional read-only demo.
3. Provision dedicated NAS database roles: a schema/migration owner and a runtime
   role limited to required data operations. The empty `jobtracker` database and
   restricted `jobtracker_owner` and `jobtracker_app` login roles now exist on the
   NAS. Their credentials are set; schema v2 is initialized; the documented runtime
   grants are applied. Both roles connected successfully with verified TLS. The
   SOPS-encrypted application, migration, and CA Secrets are present in the local
   GitOps working tree at base `a320a9f`. They are uncommitted and not deployed;
   production still needs reviewed Deployment configuration before it can use them.
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

## Production OIDC identity verification (November 16, 2026)

The production authorization-code flow completed as `tyler180` using the existing
strict production callback, confidential client, and S256 PKCE. A temporary verifier
validated state, the ID token signature, issuer, audience, expiry, and nonce.
No tokens or credentials were printed. This verified the provider identity; the
production application callback and session handling still need deployment validation.

Verified issuer: `https://auth.k8s.749rmw.com/application/o/jobtracker/`

Verified subject: `2b645094a778a5fdd2bde69079f2fbb080af48357bb0217b7362d8b79822aebe`

Use this exact pair for the final import. No data was imported during verification.

## Final production import (November 16, 2026)

The JSON Deployment was scaled to zero after pausing Jobtracker Argo CD
reconciliation with `argocd.argoproj.io/skip-reconcile=true`. The live context was
`talos-physical`, namespace `jobtracker`, image `v0.2.37`.

A read-only helper mounted the original PVC after the application pod terminated.
All 145 archived files matched the frozen source SHA-256 manifest. The archive
contains 144 jobs and passed migration CLI validation.

- Backup: `.local-backups/production-cutover-20261116/archive.tar.gz`
- SHA-256: `822edbf7fe79adc9b400de081f7996a123d179c96426677a2f78caa118447480`
- Initial target check: zero users, jobs, and import ledger entries.
- First import: 144 inserted, 144 verified against raw bytes and JSON fields.
- Repeat import: zero inserted, 144 unchanged and verified.
- Production totals: one owner, 144 jobs, assigned to the verified identity above.
- Reports: `backup-verification.json`, `first-import.json`, `repeat-import.json`
  in the same ignored backup directory.

The original JSON PVC and backup are preserved. The Deployment remains at zero
replicas and reconciliation is paused to prevent further JSON writes. The next
step is the reviewed PostgreSQL deployment and real application login validation.
Do not resume JSON writes before that decision; new writes would require a new
backup/import reconciliation. Restore Argo reconciliation only after the desired
GitOps configuration is ready.

## Production deployment accepted (November 16, 2026)

GitOps PR #61 deployed the PostgreSQL overlay and SOPS Secrets. Argo CD reports
Jobtracker Synced and Healthy; the Deployment has a ready Service endpoint.
Reconciliation resumed after the Secrets synced successfully.

A real production browser login completed through Authentik. `/auth/me` returned
HTTP 200 with the exact verified issuer/subject recorded above and internal user
UUID `7672c560-728e-472d-be15-351c4d52c777`. `/api/jobs` returned HTTP 200 and all
144 imported jobs; an unauthenticated request returned HTTP 401. The runtime
Secret is injected, the database CA is mounted read-only, and the JSON PVC remains
preserved. Production now accepts PostgreSQL writes. Any rollback must reconcile
new database writes before restarting JSON storage.

## Primary user URL (November 16, 2026)

The user-facing origin is now `https://jobtracker.749rmw.com`, deployed by GitOps
PR #62. The encrypted PUBLIC_URL and the strict Authentik callback use this origin.
The Authentik application launch URL also uses it; issuer and subject are unchanged.

Nginx Proxy Manager at `http://192.168.1.55:81/` retains its existing HTTPS
upstream to the cluster, Host/SNI `jobtracker.k8s.749rmw.com`, and explicitly sets
`X-Forwarded-Host: jobtracker.749rmw.com`. The production gateway route matches
that public-host marker and rewrites the backend Host to the primary origin.
Requests without it redirect from the old cluster hostname to the primary URL.
This proxy edit is live configuration in Nginx Proxy Manager, outside GitOps.

Real application login on the primary URL returned the same owner identity and
all 144 jobs with HTTP 200. The old hostname returned HTTP 302 to the primary URL.
