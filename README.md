# Job tracker

A self-hosted Go app that saves job descriptions before the posting disappears. Paste a supported posting URL into the web page, or paste the description with a company name, title, and HTTPS source URL from another careers site; the archive retains the job title, company/board identifier, location, canonical source URL, original description HTML, readable text, and UTC save time.

## Run locally

Requires Go 1.26 or newer:

```sh
go run ./cmd/jobtracker
```

Open http://127.0.0.1:8080. Postings are saved as individual JSON files under `./data`. They survive server restarts. Back up this directory; copying only the app does not preserve your archive. The application form fills company (the provider board/tenant identifier, which you can correct) and job title automatically when you enter a posting URL; you can override it. A blank title at save time uses the title extracted from the posting. Previewing a URL does not save it. The application form records your company name, job title, posting URL, and status: applied, waiting for response, not moving forward, or interview. Interview tracking includes initial screening, round 1, round 2, or round 3, notes on how it went, and next steps. All applications appear in a status-filterable overview with links to their locally saved descriptions. Change status directly using the dropdown in each row; selecting interview uses the previous stage or initial screening, and displays a stage dropdown. The Delete button asks for confirmation and removes both the application and its saved description. Edit application details to update any saved posting; existing archives appear as “Saved posting only” until tracking details are added. Interview notes remain saved when you switch to another status.

The archived company field currently means the provider's board/tenant identifier, not a verified legal company name. Saving a URL again retains the first snapshot, without overwriting its description or save date.

Configuration:

| Variable | Default | Purpose |
|---|---|---|
| `LISTEN_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `DATA_DIR` | `./data` | Persistent archive directory |

## Supported URLs

- Ashby: `https://jobs.ashbyhq.com/{board}/{posting-id}` (also `/application`). Uses the public board API and selects the matching posting.
- Greenhouse: `https://boards.greenhouse.io/{board}/jobs/{id}` or `https://job-boards.greenhouse.io/{board}/jobs/{id}`. Corresponding EU hosts are also accepted.
- Workday: `https://{tenant}.wd{number}.myworkdayjobs.com/{locale}/{site}/job/{location}/{slug}`. Locale is optional; `/apply` links are accepted. Uses the careers site's JSON detail endpoint.

Tracking query parameters and fragments are removed from saved URLs. Custom company domains, Greenhouse embedded boards, short links, and redirects are not supported for automatic import. For Upstart and other careers sites, copy the description from the posting into the optional **Job description** field and enter its company, title, and HTTPS URL. Saving pasted text makes no outbound request and works even when a site blocks automated imports. Pasted descriptions are stored as plain text (up to 64,000 bytes), with provider `manual`; no original HTML is captured. The first saved snapshot is retained on duplicate saves. Tracking query parameters and fragments are also removed from these source URLs. No ATS account or API key is needed. Closed, private, missing, or blocked postings cannot be recovered: save while the job is available. Workday's careers endpoint is not a documented stable public API and may change or differ between tenants. No browser automation or CAPTCHA bypass is attempted.

Provider references: [Ashby public postings API](https://developers.ashbyhq.com/docs/public-job-posting-api), [Greenhouse Job Board API](https://docs.greenhouse.io/job-board.html).

## API

```sh
curl -X POST http://127.0.0.1:8080/api/jobs \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://jobs.ashbyhq.com/your-board/posting-id"}'
curl http://127.0.0.1:8080/api/jobs
```

`POST /api/jobs` also accepts `description_text` with `company`, `title`, and an application `status` to archive pasted text from any HTTPS posting URL. A nonblank description bypasses automatic import; company and title are required. A blank description uses the existing automatic importer. Manual URLs cannot contain credentials or custom ports. Save request bodies are limited to 128 KiB; preview and application updates retain their 32 KiB limit.

`POST /api/jobs` returns the snapshot with status 201 for a new posting, or 200 for an existing one. Invalid URL/JSON returns 400, unsupported content type 415, import capacity exceeded 429, provider failure 502, and archive failure 500. `GET /api/jobs` returns saved postings newest first. `GET /healthz` is a process health probe.

`POST /api/jobs/preview` accepts a posting URL and returns extracted posting details without saving an archive. It uses the same provider restrictions and import limits as saving.

To create a tracked application, include `company`, `title`, and `status` alongside `url` in `POST /api/jobs`. Optional fields are `interview_stage`, `interview_notes`, and `next_steps`; an interview stage is required for interview status. Application fields are stored separately from the immutable posting snapshot. Duplicate imports preserve both the first snapshot and existing tracking details.

`DELETE /api/jobs/{id}` removes the posting and application, returning 200 with `{"deleted":true}` or 404 for a missing job.

`PUT /api/jobs/{id}/application` replaces tracking details using the same application fields (without `url`). Company and title are required and limited to 300 bytes each; notes and next steps are limited to 10,000 bytes each. Invalid fields return 400 and missing jobs return 404. `GET /jobs/{id}` displays the escaped, locally archived description without fetching the original posting.

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

To integrate with `tyler180/talos-gitops`, follow the [reusable release workflow setup](docs/release-workflow.md). After configuring the GitHub App, an intentional version tag publishes the image and opens a PR with the digest-pinned app manifests and Argo registration. Review and merge the PR, manually sync Argo, and verify archive persistence before adding an authenticated route.

The example image has not been published. The release workflow can publish it and open a GitOps PR after its GitHub App credentials are configured; it never syncs the cluster. The Service is internal only; a public route and SSO are intentionally a separate deployment step.

## Verification

```sh
go test -race ./...
go vet ./...
go build ./cmd/jobtracker
kubectl kustomize deploy
```

Tests cover provider payloads, URL validation, absent/malformed descriptions, readable conversion, persistent first-snapshot behavior, and HTTP request boundaries. GitHub Actions also builds the container. Live smoke verification on October 2, 2026 saved a public posting from Ashby's own board, Cloudflare's Greenhouse board, and NVIDIA's Workday site. This is compatibility evidence for those endpoints, not a guarantee for every tenant.

Application dates use `YYYY-MM-DD`: `applied_date`, `response_date`, `screening_date`, `round_1_date`, `round_2_date`, and `round_3_date`. The form defaults the applied date to today in your browser's timezone. When dates are blank, the server defaults the applied date, the response date for interview/rejection status, and the selected interview stage date to today in America/Denver. Dates for events that have not occurred remain blank. Provided dates and dates from prior interview stages are retained. Existing files are not backfilled until edited; omitted date fields on updates preserve existing values.
