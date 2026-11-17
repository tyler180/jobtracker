# Authentik and NAS staging validation

Validated November 12, 2026. Production was not switched.

## Configured resources

- URL: `https://jobtracker-staging.k8s.749rmw.com`
- Kubernetes namespace: `jobtracker-staging`; Deployment/Service `jobtracker`.
  This is manually managed staging, separate from the existing GitOps app.
- Image: `ghcr.io/tyler180/jobtracker@sha256:83731217e7dd12075cee8acd8b5870c5e674ad8b2925f67cc04f16c7efb7c041`.
  The staging tag was published without triggering a production release.
- NAS database: `jobtracker_staging`, PostgreSQL 18.6, schema version 2.
  Roles: `jobtracker_staging_owner` and restricted `jobtracker_staging_app`.
- Authentik application: **Jobtracker staging**. Issuer:
  `https://auth.k8s.749rmw.com/application/o/jobtracker-staging/`.
  Confidential client, authorization-code grant only, hashed user-ID subjects,
  explicit consent, and one strict callback:
  `https://jobtracker-staging.k8s.749rmw.com/auth/callback`.
- Application access is bound to the owner and a now-disabled temporary external
  test account. Public self-registration was not enabled.
- Runtime settings are in the namespace's `jobtracker-config` Secret. Schema-owner
  credentials, client settings, verified identities, and aggregate validation
  evidence are retained locally under the ignored, protected
  `.local-backups/staging-config/` directory. Its private resource manifest must
  never be committed. The copied NAS superuser credential file was removed after
  provisioning.

## Observed results

The owner completed real Authentik OIDC sign-in. The verified issuer/subject were
used explicitly to import the 142-record backup into staging. No private JSON
archive is mounted into the staging app. The second external account was accessed
through Authentik's audited administrator impersonation, producing a separate
verified OIDC identity. It saw zero owner records, could create/edit/delete its own
synthetic record, and received 404 for read/edit/date/delete attempts against an
owner record. The synthetic record was deleted, its app session was logged out,
the impersonation ended, and the Authentik test account was deactivated.

Afterward, another reconciliation verified all 142 original records unchanged
with zero additional inserts. Restarting staging preserved both the owner's
session and its 142 records. A fresh login after restricting grant types also
succeeded. The NAS runtime role was tested to reject schema DDL and migration
history access. The Gateway accepted the staging route and resolved its backend.

Production remains on `v0.2.37` with its original JSON archive; its Deployment,
route, and storage were not modified. Staging uses a copy of the November 12
backup and will not receive subsequent production changes automatically.

## PostgreSQL TLS follow-up (November 16, 2026)

The NAS PostgreSQL 18.6 server has TLS enabled and presents a certificate signed
by the private `Jobtracker PostgreSQL CA`. The server certificate is valid for
`192.168.3.128`, the address staging uses. The earlier staging Secret explicitly
set `sslmode=disable`, so PostgreSQL accepted a plaintext connection even though
server TLS was configured.

Staging now uses `sslmode=verify-full` with
`sslrootcert=/etc/jobtracker/postgres-ca/ca.crt`. The CA is in the
`jobtracker-postgres-ca` Secret, mounted read-only in the staging pod. The CA and
server certificate chain and IP name were verified against the live NAS before
the change. After rollout, staging started successfully; a read-only database
check reported schema version 2, 142 jobs, and TLS enabled for every visible
`jobtracker_staging_app` session. The production Deployment and Secret were not
changed.

## Before a production switch

The server's current `pg_hba.conf` includes a `host all all all scram-sha-256`
rule, so other NAS PostgreSQL clients may continue connecting without TLS.
Jobtracker's production `jobtracker` database is empty, schema v2 is initialized,
separate restricted roles and runtime grants are configured, and both roles
connected successfully with `sslmode=verify-full`. The SOPS-encrypted application,
migration, and CA Secrets are present in the local GitOps working tree at base
`a320a9f`, but are uncommitted and not deployed. The production Authentik provider
and application exist, but production sign-in and the owner's issuer/subject remain
unverified. Keep production on JSON. Before switching traffic, review the GitOps
configuration, quiesce JSON writes, take a fresh verified backup, and rehearse final
import and rollback. Use the cutover sequence in
[authentication.md](authentication.md) and [postgresql-migration.md](postgresql-migration.md).

## Production cutover completed (November 16, 2026)

The production PostgreSQL overlay was deployed through GitOps PR #61. Real app
login matched the verified production identity and returned all 144 freshly
imported jobs. Argo CD is Synced/Healthy and unauthenticated API access returns 401.
Earlier production-JSON and pending-cutover notes above describe the pre-cutover
state; see `postgresql-migration.md` for final backup and acceptance evidence.
