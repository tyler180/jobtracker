# Jobtracker releases

The reusable release workflow and its tests now live in [tyler180/github-workflows](https://github.com/tyler180/github-workflows). Jobtracker owns its app tests, Dockerfile, initial deployment manifests, and `.github/workflows/release.yaml` caller.

The caller uses:

```yaml
uses: tyler180/github-workflows/.github/workflows/reusable-release.yaml@v1
```

## Setup order

1. Commit and push the shared workflow changes in `github-workflows`. Run its **Validate shared GitOps release** workflow. Publish the shared **`v1`** tag after those checks pass. That tag must exist before Jobtracker's release workflow can run.
2. Commit and push Jobtracker's updated caller and ensure its CI passes.
3. Create a GitHub App installed only on `tyler180/talos-gitops`, with **Contents: Read and write** and **Pull requests: Read and write**. Set the App ID as Jobtracker's Actions variable `GITOPS_APP_ID`, and its private key as the Actions secret `GITOPS_APP_PRIVATE_KEY`. Do not commit the private key.
4. If `github-workflows` is private, configure its Actions settings to allow Jobtracker to use its reusable workflows.
5. Create an intentional Jobtracker release tag from the tested default branch:

```sh
git tag -a v0.1.0 -m 'Release Jobtracker v0.1.0'
git push origin v0.1.0
```

The shared workflow publishes `ghcr.io/tyler180/jobtracker:v0.1.0` for `linux/amd64`, captures the digest, renders the GitOps proposal, and opens or updates the `automation/jobtracker` PR in `talos-gitops`. Make the GHCR package public after its first publication, or configure image-pull credentials before syncing.

The first proposal adds `applications/jobtracker/`, `infrastructure/applications/jobtracker.yaml`, and its entry in `infrastructure/kustomization.yaml`. Later releases update the Deployment image while preserving cluster settings. Source `deploy/` changes are copied only for initial onboarding; later infrastructure changes belong in GitOps.

Review and merge the GitOps PR, manually sync `root` and then `jobtracker`, and keep Prune, Force, and Replace unchecked. Verify the PVC, pod readiness, real posting import, and persistence across pod replacement before adding an authenticated route. CI has no cluster credentials and does not sync Argo.

To retry a release, run **Release Jobtracker** from the default branch and enter the existing app tag. Shared workflow versions (`v1`) and application versions (`v0.1.0`) are independent.

See the [shared workflow setup guide](https://github.com/tyler180/github-workflows/blob/main/README.md) for configuration and reuse by other apps.
