# Release Process Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire up automated, Conventional-Commits-driven releases for the `agent` repo: release-please maintains `CHANGELOG.md` and the `version` constant, tags `vX.Y.Z`, cuts a GitHub Release, and triggers the existing Docker build/push/sign without relying on a tag-push event.

**Architecture:** `release-please-config.json` (manifest mode, `release-type: simple`, single package `.`) drives a new `release-please.yml` workflow. `docker-build.yml` becomes a reusable workflow (`workflow_call`) that a second job in `release-please.yml` calls directly when a release is cut, checking out the release tag explicitly. Existing `push`/`pull_request` triggers on `docker-build.yml` are untouched.

**Tech Stack:** GitHub Actions, `googleapis/release-please-action@v4`, existing Go toolchain (untouched), Node (`npx js-yaml`) used only as a local YAML-syntax checker during this implementation — no new runtime dependency for the project itself.

Spec: `docs/superpowers/specs/2026-07-08-release-process-design.md`

---

### Task 1: Mark the version constant for release-please

**Files:**
- Modify: `agent/cmd/undump/main.go:22`

- [ ] **Step 1: Add the release-please marker comment**

Change line 22 from:

```go
const version = "0.1.0"
```

to:

```go
const version = "0.1.0" // x-release-please-version
```

- [ ] **Step 2: Verify the Go build still compiles**

Run: `bash hack/godev.sh run ./cmd/undump --version`
Expected: prints `undump version 0.1.0` (or cobra's equivalent version output) with no build errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/undump/main.go
git commit -m "chore: mark version constant for release-please"
```

---

### Task 2: Add release-please config and manifest

**Files:**
- Create: `agent/release-please-config.json`
- Create: `agent/.release-please-manifest.json`

- [ ] **Step 1: Create the config file**

```json
{
  "$schema": "https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json",
  "release-type": "simple",
  "packages": {
    ".": {
      "extra-files": [
        {
          "type": "generic",
          "path": "cmd/undump/main.go"
        }
      ]
    }
  }
}
```

- [ ] **Step 2: Create the manifest file, seeded to the current version**

```json
{
  ".": "0.1.0"
}
```

- [ ] **Step 3: Verify both files are valid JSON**

Run (PowerShell):
```powershell
Get-Content -Raw release-please-config.json | ConvertFrom-Json | Out-Null
Get-Content -Raw .release-please-manifest.json | ConvertFrom-Json | Out-Null
echo "both valid"
```
Expected: prints `both valid` with no errors.

- [ ] **Step 4: Commit**

```bash
git add release-please-config.json .release-please-manifest.json
git commit -m "chore: add release-please config and manifest"
```

---

### Task 3: Make docker-build.yml callable as a reusable workflow

**Files:**
- Modify: `agent/.github/workflows/docker-build.yml`

- [ ] **Step 1: Add a `workflow_call` trigger with a `ref` input**

Change the `on:` block from:

```yaml
on:
  push:
    branches: [main]
    tags: ["v*.*.*"]
  pull_request:
    branches: [main]
```

to:

```yaml
on:
  push:
    branches: [main]
    tags: ["v*.*.*"]
  pull_request:
    branches: [main]
  workflow_call:
    inputs:
      ref:
        description: "git ref to build (e.g. a release tag); defaults to the triggering ref"
        required: false
        type: string
```

- [ ] **Step 2: Check out the given ref explicitly**

Change the checkout step from:

```yaml
      - uses: actions/checkout@v4
```

to:

```yaml
      - uses: actions/checkout@v4
        with:
          ref: ${{ inputs.ref || github.ref }}
```

- [ ] **Step 3: Verify YAML syntax**

Run: `npx -y js-yaml .github/workflows/docker-build.yml`
Expected: prints the parsed document (no `YAMLException` error).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/docker-build.yml
git commit -m "ci: make docker-build.yml callable as a reusable workflow"
```

---

### Task 4: Add the release-please workflow

**Files:**
- Create: `agent/.github/workflows/release-please.yml`

- [ ] **Step 1: Create the workflow**

```yaml
name: Release Please

on:
  push:
    branches: [main]

permissions:
  contents: write
  pull-requests: write

jobs:
  release-please:
    runs-on: ubuntu-latest
    outputs:
      release_created: ${{ steps.release.outputs['.--release_created'] }}
      tag_name: ${{ steps.release.outputs['.--tag_name'] }}
    steps:
      - uses: googleapis/release-please-action@v4
        id: release
        with:
          config-file: release-please-config.json
          manifest-file: .release-please-manifest.json

  build:
    needs: release-please
    if: ${{ needs.release-please.outputs.release_created }}
    permissions:
      contents: read
      packages: write
      id-token: write
    uses: ./.github/workflows/docker-build.yml
    with:
      ref: ${{ needs.release-please.outputs.tag_name }}
```

Note on the bracket syntax: `googleapis/release-please-action@v4` in manifest
mode namespaces its outputs by package path. Since the package path is `.`,
the actual output keys are `.--release_created` and `.--tag_name` — the dot
in the key means they must be read with bracket notation
(`steps.release.outputs['.--release_created']`), not dot notation.

- [ ] **Step 2: Verify YAML syntax**

Run: `npx -y js-yaml .github/workflows/release-please.yml`
Expected: prints the parsed document (no `YAMLException` error).

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release-please.yml
git commit -m "ci: add release-please workflow"
```

---

### Task 5: Ship it and verify on GitHub

**Files:** none (verification only)

- [ ] **Step 1: Push the branch and open a PR**

```bash
git push -u origin HEAD
gh pr create --title "ci: add release-please based release process" --body "$(cat <<'EOF'
## Summary
- Adds release-please (Conventional Commits -> CHANGELOG.md + git tag + GitHub Release)
- Refactors docker-build.yml into a reusable workflow so the release job can trigger it directly (default GITHUB_TOKEN tag pushes don't fire other workflows)

## Test plan
- [ ] PR checks pass (docker-build.yml still runs fine on pull_request)
- [ ] After merge: Release Please workflow runs on push to main and opens a release PR
- [ ] Merging the release PR cuts a git tag + GitHub Release and the build job publishes the ghcr image
EOF
)"
```

- [ ] **Step 2: Confirm the PR's own checks are green**

Run: `gh pr checks --watch`
Expected: `docker-build.yml`'s `build` job (running under its normal `pull_request` trigger) passes.

- [ ] **Step 3: After merge, confirm release-please opened a release PR**

Run: `gh run list --workflow=release-please.yml --limit 5`
Expected: a run against `main` with conclusion `success`.

Run: `gh pr list --search "head:release-please--branches--main"`
Expected: an open PR titled something like `chore(main): release 0.1.0` (or higher, depending on accumulated `feat`/`fix` commits) with an updated `CHANGELOG.md` and the bumped version line in `cmd/undump/main.go`.

- [ ] **Step 4: Merge the release PR and confirm the release artifacts**

Run: `gh release list --limit 5`
Expected: a new release `vX.Y.Z` exists.

Run: `gh run list --workflow=release-please.yml --limit 1`
Expected: the latest run's `build` job succeeded, and the ghcr image for that tag exists (check the Packages tab or `gh api /orgs/UnDumpd/packages` if org-owned, or the repo's Packages sidebar).

---

## Definition of Done

A `feat:`/`fix:` commit merged to `main` produces, without manual steps: an
updated `CHANGELOG.md`, a bumped `version` constant in `cmd/undump/main.go`,
a git tag `vX.Y.Z`, a GitHub Release, and a signed ghcr image tagged with that
version — matching the design in
`docs/superpowers/specs/2026-07-08-release-process-design.md`.
