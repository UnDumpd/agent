# Release process for `agent` — design

## Scope

Versioned releases for `agent` only (public GitHub repo, BSL-licensed CLI/daemon
that users deploy themselves — version numbers matter to them). `cloud` and
`site` stay on their existing continuous-deployment flows (image tag = commit
sha, deployed via ArgoCD/GitOps); they are not part of this design.

## Versioning source of truth

Git tag `vX.Y.Z` + `CHANGELOG.md` at the repo root, driven by
[release-please](https://github.com/googleapis/release-please-action) reading
Conventional Commits (`feat:`, `fix:`, `docs:`, etc. — the convention the repo
already follows). `release-type: simple`; no Go-module-specific versioning
needed (not at v2+).

`cmd/undump/main.go` has a hardcoded `const version = "0.1.0"` that is both
the CLI's `--version` output and `RunReport.AgentVersion` sent to the cloud on
every run. It must track the release, so it carries an
`// x-release-please-version` marker comment that release-please's generic
file updater rewrites in the same release PR as the changelog entry.

## Files

- `release-please-config.json` (repo root) — `release-type: simple`, one
  package (`.`), `extra-files` entry pointing at `cmd/undump/main.go`.
- `.release-please-manifest.json` (repo root) — seeded to `0.1.0` (matches the
  current in-code version; no tag exists yet).
- `CHANGELOG.md` — created by release-please's first release PR.

## Workflows

`docker-build.yml` is refactored into a reusable workflow (`workflow_call`)
while keeping its existing `push`/`pull_request` triggers for normal
development builds on `main` and PRs — unchanged behavior there.

New `.github/workflows/release-please.yml`:
1. Job `release-please`: runs `googleapis/release-please-action` on push to
   `main`. Outputs `release_created`, `tag_name`.
2. Job `build` (`needs: release-please`, `if: needs.release-please.outputs.release_created`):
   calls `docker-build.yml` via `uses:` with the release tag, so the image
   build/push/cosign-sign steps run unconditionally on release — not
   dependent on the tag-push event.

This sidesteps the GitHub Actions rule that a tag pushed by the default
`GITHUB_TOKEN` does not trigger other workflows: the build job is invoked
directly instead of relying on `docker-build.yml`'s `tags: ["v*.*.*"]` push
trigger. No PAT needed.

`docker/metadata-action` in `docker-build.yml` already tags images with
semver (`vX.Y.Z`, `vX.Y`, `vX`) when built against a tag ref — no change
needed there.

## First release

No tags exist yet. release-please will consider the full commit history back
to the start of the repo (manifest has no prior tag to diff from) and
propose `0.1.0` or higher depending on `feat`/`fix`/`BREAKING CHANGE` commits
found — expect a large first CHANGELOG covering the whole MVP build. This is
expected and fine for a first-ever release.

## Out of scope

- `cloud` (backend) and `site` versioning/releases.
- Rewriting past commit messages to fit Conventional Commits more strictly.
- Publishing binaries outside the Docker image (no separate GoReleaser step).
