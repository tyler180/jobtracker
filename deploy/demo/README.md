# Public read-only demo

Build this checkout into an image that supports `DEMO_MODE=true`, then replace `jobtracker-demo:local` in `app.yaml` with its published digest before deploying to a cluster. Existing released images do not support this mode.

`kubectl kustomize deploy/demo` renders a separate `jobtracker-demo` namespace, Deployment and Service. Storage is a 64 MiB `emptyDir`; there is no private archive PVC. `TMPDIR=/data` keeps demo temporary files on that volume. Each process creates a fresh directory and seeds six fictional applications, ignoring `DATA_DIR`.

The optional route files propose `jobtracker-demo.k8s.749rmw.com` through the existing public gateway. They are deliberately excluded from the Kustomization until the new image and internal read-only checks pass. Confirm DNS and external HTTPS routing before adding those files to `resources` and publishing the demo.

Demo mode rejects every method other than GET/HEAD, redirects add/edit pages to Applications, removes write controls, and labels application and description pages as mock data. Search, filters, sorting, notes, milestones and saved descriptions remain available. URL imports and PDF uploads are disabled. No outbound import fetch is reachable.

For a local preview:

```sh
DEMO_MODE=true LISTEN_ADDR=127.0.0.1:8081 go run ./cmd/jobtracker
```

The normal deployment remains independent. This directory is a deployment template; it is not registered with Argo CD or publicly routed yet.
