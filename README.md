# Job tracker

A self-hosted Go app that saves job descriptions before the posting disappears. Paste a supported posting URL into the web page, or paste the description with a company name and title. A source URL is optional when a description is provided. The archive retains the job title, company/board identifier, location, canonical source URL, original description HTML, readable text, and UTC save time.

PDF uploads extract text for review before saving (up to 5 MB and 64,000 bytes of extracted text). The archive keeps the reviewed text, not the original PDF. Scanned or encrypted PDFs need a pasted description. Pay ranges are optional USD amounts, with annual salary or hourly rate, and can be edited later.

## Run locally

Requires Go 1.26 or newer:

```sh
go run ./cmd/jobtracker
```

Open http://127.0.0.1:8080. Postings are saved as individual JSON files under `./data`. They survive server restarts. Back up this directory; copying only the app does not preserve your archive. The application form fills company (the provider board/tenant identifier, which you can correct) and job title automatically when you enter a posting URL; you can override it. A blank title at save time uses the title extracted from the posting. Previewing a URL does not save it. The application form records your company name, job title, posting URL, and status: applied, waiting for response, not moving forward, or interview. The main page shows searchable application cards with status, recorded dates, notes, and links to archived descriptions. Choose **Add entry** to open the posting import form, or **Edit entry** on a card to update its details. Applied and response dates are the basic fields. Use **Add date** to record an initial screening or a numbered interview round; multiple dates and rounds beyond round 3 are supported, and dates can be removed individually. Existing screening and round dates remain available when opening older entries. Interview notes and next steps stay editable in every status. Saved-only postings can be edited without marking them applied. **Delete entry** asks for confirmation before removing the application and saved description. Editing details while keeping the same status and stage preserves blank dates.

For Ashby, Greenhouse, and Workday, the archived company field is the provider's board/tenant identifier rather than a verified legal company name. Upstart, Ford, and Principal use their company names. LinkedIn and generic job pages use the company published in the posting; pasted descriptions use extracted or entered metadata. Saving a URL again retains the first snapshot, without overwriting its description or save date.

Configuration:

| Variable | Default | Purpose |
|---|---|---|
| `LISTEN_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `DATA_DIR` | `./data` | Persistent archive directory |

## Supported URLs

- Ashby: `https://jobs.ashbyhq.com/{board}/{posting-id}` (also `/application`). Uses the public board API and selects the matching posting.
- Greenhouse: `https://boards.greenhouse.io/{board}/jobs/{id}` or `https://job-boards.greenhouse.io/{board}/jobs/{id}`. Corresponding EU hosts are also accepted.
- LinkedIn: `https://www.linkedin.com/jobs/view/{id}/` or a title/company slug ending in the numeric job ID. `linkedin.com` links are also accepted and canonicalized to the same numeric URL. Uses LinkedIn’s public guest posting endpoint, extracting the full **About the job** description, posting company, title, and location. No account or cookies are used. If LinkedIn requires sign-in, blocks the request, or omits the description, paste the About the job text and enter any missing company/title fields.
- Upstart: `https://careers.upstart.com/jobs/{slug-and-UUID}`. Extracts the rendered title and description. If the site blocks automated requests, preview offers editable company/title suggestions from the URL and asks you to paste the description; it does not save an incomplete job.
- Workday: `https://{tenant}.wd{number}.myworkdayjobs.com/{locale}/{site}/job/{location}/{slug}`. Locale is optional; `/apply` links are accepted. Uses the careers site's JSON detail endpoint.

Ford Motor Company (`www.careers.ford.com/job/.../48560/...`) and Principal (`careers.principal.com/careers-home/jobs/...`) postings can also be imported automatically. The importer reads their published job data to fill company, title, location, and the full description. Tracking parameters are removed from saved URLs. If a posting is unavailable or blocks importing, use the pasted-description fallback.

Other public HTTPS job pages can be imported when they publish inline Schema.org `JobPosting` data with a company, title, and description. The importer supports standalone objects, arrays, and `@graph` documents; it matches the requested URL and refuses ambiguous postings. Essential query parameters are retained and common tracking parameters are removed. No scripts are executed, schema links are not fetched, and redirects remain disabled. Pages without complete job data, login-only pages, and blocked requests require pasting the description.

Provider-specific URLs are canonicalized and tracking parameters removed. Custom company domains can be imported when they contain complete inline JobPosting data. Short links, redirects, and Greenhouse embedded-board URLs are not supported by the provider-specific importer. For blocked Upstart requests and other careers sites, copy the full description (including its title) into the optional **Job description** field with an optional HTTPS source URL. The form fills company and title from recognizable headings or explicit labels; enter missing fields manually. Your own edits take precedence over suggestions. Saving pasted text makes no outbound request and works even when a site blocks automated imports. Pasted descriptions are stored as plain text (up to 64,000 bytes), with provider `manual`; no original HTML is captured. For entries with a URL, the first saved snapshot is retained on duplicate saves. Each save without a URL creates a separate entry, even if the pasted description is identical. Entries without a URL omit the Original posting link. Tracking query parameters and fragments are also removed from these source URLs. No ATS account or API key is needed. Closed, private, missing, or blocked postings cannot be recovered: save while the job is available. Workday's careers endpoint is not a documented stable public API and may change or differ between tenants. No browser automation or CAPTCHA bypass is attempted.

Provider references:

- [Ashby public postings API](https://developers.ashbyhq.com/docs/public-job-posting-api) and [Greenhouse Job Board API](https://docs.greenhouse.io/job-board.html): documented APIs used by the importers.
- [LinkedIn Jobs](https://www.linkedin.com/jobs/), [Upstart careers](https://careers.upstart.com/home), [Ford careers](https://www.careers.ford.com/), and [Principal careers](https://careers.principal.com/careers-home): posting sites supported by the importers. These references are career pages, not API contracts.
- [Workday recruiting](https://www.workday.com/en-us/products/talent-management/recruiting.html): provider background. The importer uses tenant-specific careers endpoints, not a documented public API.
- [Schema.org JobPosting](https://schema.org/JobPosting): the structured data format used for generic public job pages.

## Reading, filtering, and sorting entries

Open `/` (`/entries` also remains available) to browse saved jobs, application status, dates, interview notes, and next steps. Open **Read saved description** to read the full archived posting. Empty date fields are hidden, and **Show location** lets you display posting locations.

Search by company, title, or notes. Start and end dates filter entries that have an **applied, initial screening, or interview round date** within the inclusive range. Saved and response dates do not qualify an entry. Leave either boundary blank for an open-ended range; the start date must not be after the end date. Text search and date filters work together.

Sort options are:

- **Applied date — newest to oldest** or **oldest to newest**: sorts by the applied date, with missing applied dates last.
- **Progress date — oldest to newest** or **newest to oldest**: uses each entry's earliest or latest matching applied, screening, or interview date. With a range selected, only dates inside that range determine the order. Entries without eligible dates appear last.
- **Most recent activity**: uses the latest saved, applied, response, screening, or interview date.
- **Status**: applied, waiting for response, interview, then not moving forward, with saved-only postings last.

Edit dates on the main Job tracker page, where each application row has six date fields and a **Save dates** button. Clearing a date leaves it blank; saving dates does not change status or notes. **Edit** opens the complete application editor.

## API

```sh
curl -X POST http://127.0.0.1:8080/api/jobs \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://jobs.ashbyhq.com/your-board/posting-id"}'
curl http://127.0.0.1:8080/api/jobs
```

`POST /api/jobs` also accepts `description_text` with `company`, `title`, and an application `status` to archive pasted text. The URL is optional when `description_text` is nonblank. A nonblank description bypasses automatic import; missing company and title are inferred where identifiable and must be supplied manually otherwise. A blank description uses the existing automatic importer. Manual URLs cannot contain credentials or custom ports. Save and preview request bodies are limited to 128 KiB; application updates retain their 32 KiB limit.

For a description-only tracked application:

```sh
curl -X POST http://127.0.0.1:8080/api/jobs \
  -H 'Content-Type: application/json' \
  -d '{"company":"Example company","title":"Platform Engineer","status":"applied","description_text":"Build and operate the platform."}'
```

`POST /api/jobs` returns the snapshot with status 201 for a new posting, or 200 for an existing one. Invalid URL/JSON returns 400, unsupported content type 415, import capacity exceeded 429, provider failure 502, and archive failure 500. `GET /api/jobs` returns saved postings newest first. `GET /healthz` is a process health probe.

`POST /api/jobs/preview` accepts a posting URL or pasted description and returns extracted posting details without saving an archive. An optional `description_text` field previews metadata inferred from pasted text, without an outbound request. If an Upstart import fails, preview returns URL-derived metadata with `description_required: true`; saving still requires a real description. Preview uses the same import limits as saving. Preview request bodies are limited to 128 KiB.

To create a tracked application, include `company`, `title`, and `status` alongside `url` or `description_text` in `POST /api/jobs`. Optional fields are `pay_min`, `pay_max`, `pay_type` (`salary` or `hourly`), `interview_stage`, `interview_notes`, `next_steps`, and `milestones`; an interview stage is required for interview status. Application fields are stored separately from the immutable posting snapshot. Duplicate imports preserve both the first snapshot and existing tracking details.

`PATCH /api/jobs/{id}/dates` updates only the supplied date fields (the six fields listed below). Use an empty string to clear a date; omitted dates and all other application fields are preserved, with no automatic date defaults. This works for tracked applications and saved postings without marking them applied. Invalid dates or fields return 400 and missing jobs return 404.

`DELETE /api/jobs/{id}` removes the posting and application, returning 200 with `{"deleted":true}` or 404 for a missing job.

`PUT /api/jobs/{id}/application` replaces tracking details using the same application fields (without `url`). Company and title are required and limited to 300 bytes each; notes and next steps are limited to 10,000 bytes each. Invalid fields return 400 and missing jobs return 404. `GET /jobs/{id}` displays the escaped, locally archived description without fetching the original posting.

The web page displays escaped plain text, so imported HTML cannot execute scripts. Raw HTML is retained only in JSON. Fetches have a 20-second timeout, an 8 MiB response cap, and four concurrent import slots. Provider imports use their specific endpoints; generic imports fetch the requested public HTTPS page, with no proxies or redirects and no connections to private IP addresses. The app has **no built-in authentication**: use it locally or behind a trusted authenticated gateway. Do not expose the service publicly before access controls are configured. Archive writes use synced temporary files and atomic replacement; use a single process/replica per archive directory. Listing reads the whole archive, suitable for a personal tracker.

## Container and Talos GitOps

```sh
docker build -t jobtracker:local .
docker run --rm -p 127.0.0.1:8080:8080 \
  --read-only --cap-drop=ALL --security-opt=no-new-privileges \
  -v jobtracker-data:/data jobtracker:local
```

The `/data` volume must be writable by UID/GID 65532. Existing bind-mounted directories may need ownership prepared first.

`deploy/` contains a Kustomize-ready Namespace, single-replica Deployment, Service, and 1 GiB PVC using the cluster's `nas-nfs` storage class. The deployment uses `Recreate` to avoid concurrent archive writers and runs as non-root with a read-only root filesystem. NFS permissions and directory-sync semantics must be verified on the actual storage backend.

To integrate with `tyler180/talos-gitops`, follow the [reusable release workflow setup](docs/release-workflow.md). After configuring the GitHub App, a successful default-branch change automatically publishes the next patch version and opens a digest-pinned deployment PR. GitOps CI automatically merges validated image-only Jobtracker promotions, and Argo syncs Jobtracker without pruning. Major/minor version tags and infrastructure changes remain intentional. See the release guide for the one-time activation and rollback procedure.

The release workflow publishes to GHCR and proposes GitOps changes; Argo performs cluster reconciliation. CI does not receive cluster credentials. Authentication and public routing remain configured in GitOps independently of the application release pipeline.

## Verification

```sh
go test -race ./...
go vet ./...
go build ./cmd/jobtracker
kubectl kustomize deploy
```

Tests cover provider payloads, URL validation, absent/malformed descriptions, readable conversion, persistent first-snapshot behavior, and HTTP request boundaries. GitHub Actions also builds the container. Live smoke verification on October 2, 2026 saved a public posting from Ashby's own board, Cloudflare's Greenhouse board, and NVIDIA's Workday site. This is compatibility evidence for those endpoints, not a guarantee for every tenant.

Interview dates are stored in `milestones`, an array such as `[{"stage":"initial screening","date":"2026-10-05"},{"stage":"round 4","date":"2026-10-12"}]`. Up to 100 dates can be recorded, including repeated stages. Supply `[]` to remove all interview dates; omit the collection on an update to preserve it. Screening and round 1–3 fields in older files are displayed and converted when edited through the form. Legacy application date fields use `YYYY-MM-DD`: `applied_date`, `response_date`, `screening_date`, `round_1_date`, `round_2_date`, and `round_3_date`. The form defaults the applied date to today in your browser's timezone. When dates are blank, the server defaults the applied date, the response date for interview/rejection status, and the selected interview stage date to today in America/Denver. Dates for events that have not occurred remain blank. Provided dates and dates from prior interview stages are retained. Existing files are not backfilled until edited; omitted date fields on updates preserve existing values.

Local PDF importing requires Poppler (`pdftotext`) on PATH; the container includes it. On macOS, install it with `brew install poppler`.

`POST /api/jobs/pdf` accepts a raw `application/pdf` body without content encoding and returns extracted text and inferred metadata without saving. It shares the four import slots, caps uploads at 5 MiB, extracted text at 64,000 bytes, and processing at 15 seconds. Text is processed through stdin/stdout without temporary files.

Pay suggestions are extracted from explicit USD salary/annual or hourly amounts in imported descriptions, pasted text, and PDF text. Conflicting rates leave pay blank for review, and manual form edits take precedence. Existing entries have a **Suggest pay from saved description** button; suggestions are saved only with your changes.

Interview milestones may include optional `time` (`HH:MM`) and `timezone` (an IANA name such as `America/Denver`). The form defaults the timezone to America/Denver; cards display the entered local time and timezone. Dates remain separate for filtering and date-only records stay valid. Invalid times/timezones and times skipped by daylight saving transitions are rejected.
