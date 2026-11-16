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

Production remains on `v0.2.36` with its original JSON archive; its Deployment,
route, and storage were not modified. Staging uses a copy of the November 12
backup and will not receive subsequent production changes automatically.

## Before a production switch

The NAS database listener currently has TLS disabled; staging database traffic
uses the private network. Verified database TLS (or an explicitly validated
equivalent transport arrangement) remains to be configured before production.
Staging is not a completed production rollout. Prepare the production provider,
database, secrets, and reviewed GitOps configuration separately. Confirm the
production issuer/subject, quiesce JSON writes, take a fresh verified backup, and
rehearse final import and rollback before switching traffic. Use the cutover
sequence in [authentication.md](authentication.md) and
[postgresql-migration.md](postgresql-migration.md).
