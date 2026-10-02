# Job tracker

A self-hosted Go app that saves job descriptions before the posting disappears. Paste a posting URL into the web page; the archive retains the job title, company/board identifier, location, canonical source URL, original description HTML, readable text, and UTC save time.

## Run locally

Requires Go 1.26 or newer:

```sh
go run ./cmd/jobtracker
```

Open http://127.0.0.1:8080. Postings are saved as individual JSON files under `./data`. They survive server restarts. Back up this directory; copying only the app does not preserve your archive. Company currently means the provider's board/tenant identifier, not a verified legal company name. Saving a URL again retains the first snapshot, without overwriting its description or save date.

Configuration:

| Variable | Default | Purpose |
|---|---|---|
| `LISTEN_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `DATA_DIR` | `./data` | Persistent archive directory |

## Supported URLs

- Ashby: `https://jobs.ashbyhq.com/{board}/{posting-id}` (also `/application`). Uses the public board API and selects the matching posting.
- Greenhouse: `https://boards.greenhouse.io/{board}/jobs/{id}` or `https://job-boards.greenhouse.io/{board}/jobs/{id}`. Corresponding EU hosts are also accepted.
- Workday: `https://{tenant}.wd{number}.myworkdayjobs.com/{locale}/{site}/job/{location}/{slug}`. Locale is optional; `/apply` links are accepted. Uses the careers site's JSON detail endpoint.

Tracking query parameters and fragments are removed from saved URLs. Custom company domains, Greenhouse embedded boards, short links, and redirects are not supported. No ATS account or API key is needed. Closed, private, missing, or blocked postings cannot be recovered: save while the job is available. Workday's careers endpoint is not a documented stable public API and may change or differ between tenants. No browser automation or CAPTCHA bypass is attempted.

Provider references: [Ashby public postings API](https://developers.ashbyhq.com/docs/public-job-posting-api), [Greenhouse Job Board API](https://docs.greenhouse.io/job-board.html).

## API

```sh
curl -X POST http://127.0.0.1:8080/api/jobs \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://jobs.ashbyhq.com/your-board/posting-id"}'
curl http://127.0.0.1:8080/api/jobs
```

`POST /api/jobs` returns the snapshot with status 201 for a new posting, or 200 for an existing one. Invalid URL/JSON returns 400, unsupported content type 415, import capacity exceeded 429, provider failure 502, and archive failure 500. `GET /api/jobs` returns saved postings newest first. `GET /healthz` is a process health probe.

The web page displays escaped plain text, so imported HTML cannot execute scripts. Raw HTML is retained only in JSON. Fetches have a 20-second timeout, an 8 MiB response cap, and four concurrent import slots. Requests are restricted to provider endpoints, with no proxies or redirects and no connections to private IP addresses. The app has **no built-in authentication**: use it locally or behind a trusted authenticated gateway. Do not expose the service publicly before access controls are configured. Archive writes use synced temporary files and atomic replacement; use a single process/replica per archive directory. Listing reads the whole archive, suitable for a personal tracker.

## Container and Talos GitOps

```sh
docker build -t jobtracker:v0.1.0 .
docker run --rm -p 127.0.0.1:8080:8080 \
  --read-only --cap-drop=ALL --security-opt=no-new-privileges \
  -v jobtracker-data:/data jobtracker:v0.1.0
```

The `/data` volume must be writable by UID/GID 65532. Existing bind-mounted directories may need ownership prepared first.

`deploy/` contains a Kustomize-ready Namespace, single-replica Deployment, Service, and 1 GiB PVC using the cluster's `nas-nfs` storage class. The deployment uses `Recreate` to avoid concurrent archive writers and runs as non-root with a read-only root filesystem. NFS permissions and directory-sync semantics must be verified on the actual storage backend.

To integrate with `tyler180/talos-gitops`:

1. Publish the container with an intentional version tag such as `v0.1.0` and record its image digest.
2. Replace the unpublished example image in `deploy/app.yaml` with that published digest-pinned reference.
3. Copy these manifests into `applications/jobtracker` in the GitOps repository and add an Argo application following that repository's conventions.
4. Review and manually sync it. Verify the PVC, permissions, readiness, and archive persistence across pod replacement before adding an authenticated route.

The example image has not been published. No cluster changes or GitOps pushes are made by this project. The Service is internal only; a public route and SSO are intentionally a separate deployment step.

## Verification

```sh
go test -race ./...
go vet ./...
go build ./cmd/jobtracker
kubectl kustomize deploy
```

Tests cover provider payloads, URL validation, absent/malformed descriptions, readable conversion, persistent first-snapshot behavior, and HTTP request boundaries. GitHub Actions also builds the container. Live smoke verification on October 2, 2026 saved a public posting from Ashby's own board, Cloudflare's Greenhouse board, and NVIDIA's Workday site. This is compatibility evidence for those endpoints, not a guarantee for every tenant.
