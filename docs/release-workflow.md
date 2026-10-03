# Jobtracker continuous delivery

After a successful **Test** run for a push to `main`, **Release Jobtracker** rechecks the exact commit and calls `tyler180/github-workflows/.github/workflows/reusable-release.yaml@v2.0.0`.

The shared workflow serializes releases, skips older queued commits once `main` has moved, increments only the patch component of the latest semantic tag, and publishes the image with SBOM and provenance. It records the tag only after publishing successfully. Retries reuse an existing commit tag and registry digest instead of replacing a published image.

It opens or updates the `automation/jobtracker` PR in `talos-gitops`, using a scoped GitHub App token. GitOps CI renders the manifests and verifies that the bot's PR changes only the Jobtracker Deployment image, with an advancing semantic version and SHA256 digest. Changes to storage, routing, environment, other apps, or the Argo registration fail unattended promotion. That policy is loaded from the PR's trusted base commit. The merge job accepts only `tyler180-gitops-bot[bot]` PRs from the expected same-repository branch and locks the merge to the tested head commit; it does not bypass repository protections.

Once the GitOps PR merges, Argo automatically reconciles Jobtracker. Self-healing and bounded sync retries are enabled for this app. Pruning, empty-app deletion, Force, and Replace are not enabled. Other Argo applications, including the root app, keep their existing sync policies. CI does not need Kubernetes, Talos, or Argo credentials.

## Configuration

- Jobtracker Actions variable `GITOPS_APP_ID` and secret `GITOPS_APP_PRIVATE_KEY` identify the existing GitHub App installed on `tyler180/talos-gitops`, with contents and pull-request write permissions.
- The release caller grants `contents: write` for automatic tag creation and `packages: write` for GHCR publication.
- GitOps CI's merge job grants its own repository token contents and pull-request write permissions. App changes still enter Jobtracker through normal PRs and tests.
- Publish the tested shared-workflow tag `v2.0.0` before merging this caller. Merge the GitOps validation/policy change first, and reconcile only the Jobtracker Application through the root app once to enable automatic sync.
- The Jobtracker GHCR package must be public or have configured pull credentials.

## Versions, retries, and rollback

Merging an app change into `main` needs no manual application tag, deployment PR merge, or Argo sync. Major and minor bumps remain intentional: push an existing `vMAJOR.MINOR.PATCH` tag on default-branch history. A tag-triggered release rechecks that source commit before publishing. If a main commit already has a semantic tag, automatic release reuses it.

To retry, run **Release Jobtracker** from `main` with the existing tag. Leave the tag blank to test and release the latest `main` commit. Failed CI never triggers automatic publication; PR and fork workflow results cannot trigger privileged automatic releases.

For rollback, submit and merge a normal GitOps PR restoring a previous digest-pinned image. The unattended bot policy rejects downgrades; human GitOps PRs remain available for deliberate rollback and infrastructure changes. Argo will reconcile the merged rollback automatically. To pause deployments, disable Jobtracker's `spec.syncPolicy.automated.enabled` in GitOps, reconcile that Application through root, and disable the app release workflow if publication should also pause.

A successful release workflow means the image and proposal were published. GitOps CI reports merge success separately; Argo reports sync and readiness. Verify the deployed image, Argo health, HTTP availability, and archive persistence when initially enabling this pipeline.
