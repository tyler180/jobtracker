# PostgreSQL storage and Authentik sign-in

`STORAGE_BACKEND=postgres` enables mandatory OpenID Connect sign-in and routes all
application persistence through the PostgreSQL repository. `file` remains the
default for existing single-user installations. Supplying database/OIDC settings
in file mode is an error, so partial configuration cannot silently disable auth.
The public demo remains a separate, read-only fictional archive and never opens
the private archive, database, or identity provider.

## Provision and configure

1. Create a dedicated Jobtracker database, a schema/migration owner, and a separate
   `jobtracker_app` login role. Keep their passwords in secret storage. Do not use
   the NAS PostgreSQL superuser for the running app. Choose a database connection
   with verified TLS for production, or separately validate equivalent private
   transport protection. The local rehearsal's trust authentication and disabled
   database TLS are only for its disposable loopback database.
2. Set `MIGRATION_DATABASE_URL` for the schema-owner connection and explicitly run
   `go run ./cmd/jobtracker-migrate -init-schema -apply`. This applies schema v2;
   the web server checks the version but never modifies the schema at startup.
3. Apply [runtime-grants.sql](../deploy/postgres/runtime-grants.sql) as the owner.
   The runtime role may operate on jobs, accounts, login attempts, and sessions,
   but cannot overwrite archived source bytes, access the migration ledger, or
   create schema objects. These permissions are exercised in the integration test.
4. In Authentik, create a Jobtracker application and confidential OAuth2/OIDC
   provider. Enable authorization-code login and S256 PKCE, with `openid`,
   `profile`, and `email` scopes. Set one strict redirect URI:
   `https://YOUR-JOBTRACKER-HOST/auth/callback`. Choose the provider's stable
   hashed-ID/UUID subject mode and do not change it after accounts are linked.
   Copy the exact issuer from the provider's discovery document; do not guess it
   from the admin URL. Configure application access/enrollment policies for the
   intended users. Jobtracker provisions an account only after a verified login;
   it does not enable public Authentik registration itself.
5. Start an isolated staging instance with these environment variables:

   ```text
   STORAGE_BACKEND=postgres
   DATABASE_URL=<secret runtime connection string>
   PUBLIC_URL=https://YOUR-JOBTRACKER-HOST
   OIDC_ISSUER_URL=<exact issuer from discovery>
   OIDC_CLIENT_ID=<confidential client ID>
   OIDC_CLIENT_SECRET=<secret client value>
   ```

   Secrets belong in environment injection from your secret manager, not in
   tracked Compose files or shell command arguments. PostgreSQL mode ignores
   `DATA_DIR`. Startup requires a working database and identity-provider discovery
   and fails closed on missing configuration or an outdated schema.
6. Sign in as the intended owner and visit `/auth/me` on that instance. This
   returns the caller's verified issuer, subject, and internal UUID without tokens
   or passwords. Use that exact issuer/subject for the explicit archive import
   described in [migration testing](postgresql-migration.md). No account is
   automatically granted legacy records based on email, signup order, or name.

Use separate staging and production providers if their callback URLs differ.
All users allowed by the configured provider can provision their own empty
Jobtracker account; constrain that access in Authentik as required. New account
creation does not grant Authentik administration or anyone else's application data.

## Session and HTTP behavior

- Login uses authorization code with S256 PKCE, a nonce, and ten-minute single-use
  state tied to a separate secure browser cookie. Issuer, audience, signature,
  token expiration, nonce, and returned subject are verified before creating a
  user or session. Login state and PKCE verifiers are stored server-side.
- Sessions last twelve hours with a random opaque token in a `Secure`, `HttpOnly`,
  `SameSite=Lax`, host-only cookie. Only a hash of that token is stored in the
  database. Sessions persist across application restarts/replicas. Login rotates
  any existing browser session; expiry is checked for every request. Expired
  pending challenges are removed on login; expired sessions on completed login.
- **Account → Sign out** posts to `/auth/logout`, revokes the session in the
  database, clears the cookie, and shows a signed-out page without automatically
  logging back in. It does not end the Authentik SSO session. There is no refresh
  token, back-channel logout, or immediate Authentik deactivation propagation;
  existing app sessions can remain valid until their twelve-hour expiry unless
  their database session records are revoked.
- API routes return JSON 401 without a valid session; browser pages redirect to
  sign-in. Descriptions, editing pages, previews, PDF imports, and mutations are
  protected. Every database query uses the authenticated owner ID, and another
  user's record is reported as missing. Owner IDs are not accepted from forms,
  query strings, or forwarded identity headers.
- Cookie-authenticated writes require `Origin` to exactly match `PUBLIC_URL` and
  reject `Sec-Fetch-Site: cross-site`. Scripts using the API must send their valid
  session cookie and that Origin; there are no unauthenticated write endpoints
  or API bearer tokens. An API 401 requires signing in again.
- The configured public host is enforced for all protected/auth routes. Reverse
  proxies must preserve that Host and terminate HTTPS correctly. The app does not
  trust `X-Forwarded-User`, `X-Auth-Request-User`, or forwarded scheme headers for
  authentication or callback construction. `/healthz` remains a public process
  health probe; it is not a continuous database/provider readiness check.
- The runtime connection pool is bounded to ten connections, with five-second
  connect and ten-second statement timeouts. Requests have a 25-second context
  deadline. Database/session errors fail closed rather than exposing file data.

## Validation and deployment status

Run `scripts/test-archive-migration.sh` as documented in the migration guide. It
also tests OIDC over local TLS with signed identity tokens and a restricted runtime
database role. Tests cover bad signatures/issuers/audiences/nonces/expiry, state
browser binding/replay/expiry, CSRF, logout/session expiry, migrated ownership,
and the actual application's list/description/save/edit/date/delete HTTP routes.
The temporary provider and databases are never the live Authentik/NAS services.

Code integration does not switch the deployed app. Finish staging validation,
confirm the final owner's identity, quiesce old archive writes, take a fresh
verified backup, import and compare it, then promote the configured app. Preserve
the old archive and plan how any PostgreSQL-only writes will be recovered if a
rollback becomes necessary.

Protocol references: [Go OIDC client](https://github.com/coreos/go-oidc),
[Go OAuth2 PKCE support](https://pkg.go.dev/golang.org/x/oauth2#S256ChallengeOption),
and [Authentik OAuth2/OIDC provider](https://docs.goauthentik.io/add-secure-apps/providers/oauth2/).
