<p align="center">
  <img src="docs/assets/banner.svg" alt="undump — continuous backup restore-testing for Postgres and MySQL" width="820">
</p>

<p align="center">
  <a href="https://undumpd.com"><img src="https://img.shields.io/badge/undumpd.com-website-2ea043" alt="undumpd.com"></a>
  <a href="https://dash.undumpd.com/?demo=1&lang=en&utm_source=github&utm_medium=readme"><img src="https://img.shields.io/badge/live%20demo-no%20signup-7ee787" alt="Live demo"></a>
  <a href="https://github.com/UnDumpd/agent/actions/workflows/docker-build.yml"><img src="https://github.com/UnDumpd/agent/actions/workflows/docker-build.yml/badge.svg" alt="Docker build"></a>
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25">
  <img src="https://img.shields.io/badge/image-ghcr.io%2Fundumpd%2Fagent-2ea043?logo=docker&logoColor=white" alt="ghcr.io/undumpd/agent">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-BUSL--1.1-blue" alt="License: BUSL-1.1"></a>
</p>

Continuous **backup restore-testing** agent for Postgres and MySQL. Backups are everywhere; few teams find out they're broken until the day they actually need one. `undump` closes that gap by periodically pulling a real dump, restoring it into a throwaway container, and checking that the data is actually alive — all inside your own network.

Part of UnDump — this agent is the open-source half. The other half, UnDump Cloud, only ever receives `pass`/`fail` results and metrics, never your data.

**[Live demo of the cloud dashboard](https://dash.undumpd.com/?demo=1&lang=en&utm_source=github&utm_medium=readme)** — three fake targets with real run history, no signup.

<p align="center">
  <img src="docs/assets/demo.svg" alt="undump check output: two targets pass, one fails because pg_restore hit end-of-file in a truncated dump" width="760">
</p>

## Why

A backup job that "succeeds" can still be worthless: the dump truncates mid-write, a cron job silently produces a 0-byte file, the schema drifts and `pg_restore` breaks, or the source table was empty to begin with. All of these look fine from the outside — "the backup ran" — right up until the day you need to restore and it doesn't work. `undump` finds that out on a schedule, not during an incident.

## How it works

<p align="center">
  <img src="docs/assets/architecture.svg" alt="Architecture: inside your infrastructure the agent pulls a dump from S3, restores it into an ephemeral database container, runs checks, and removes the container; only an optional pass/fail report crosses the boundary to UnDump Cloud" width="960">
</p>

Restore happens **in your infrastructure**. The agent never uploads the dump, row contents, or credentials anywhere — only the run result (status, RTO, check names) leaves the machine, and only if you set a cloud API key at all.

## Quick start

```bash
cp undump.example.yaml undump.yaml         # fill in your S3 source and (optionally) a cloud API key
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$(pwd)/undump.yaml:/app/undump.yaml" \
  -e S3_ACCESS_KEY=... -e S3_SECRET_KEY=... \
  ghcr.io/undumpd/agent run --config /app/undump.yaml
```

That starts the long-running daemon: each target is checked on its own `schedule` (standard 5-field cron) until the container is stopped. For a single one-off pass instead — e.g. wired into your own cron/systemd timer — use `check` in place of `run`.

Or build it locally instead of pulling the published image:

```bash
docker build -t undump .
```

The agent needs `docker.sock` mounted — that's how it spins up and tears down the ephemeral database container it restores into. The restore images (`postgres:18` / `mysql:8`) are pulled automatically on first use; pre-pull them yourself only if you want to avoid the one-time download during the first check run.

## Config

Full reference: **[CONFIGURATION.md](CONFIGURATION.md)** — every field, `env:` secret references, prefix/glob source selection, check types, the cloud report payload, and exit-code semantics. A ready-to-copy example lives in [`undump.example.yaml`](undump.example.yaml). The short version:

```yaml
targets:
  - name: "prod-billing"
    engine: "postgres"
    schedule: "0 * * * *"
    source:
      type: "s3"
      uri: "s3://backups/billing/latest.dump"
      access_key: "env:S3_ACCESS_KEY"      # secrets are env references, never plaintext
      secret_key: "env:S3_SECRET_KEY"
    checks:
      - type: "rowcount"
        table: "invoices"
        max_drop_pct: 10.0
      - type: "freshness"
        table: "invoices"
        column: "created_at"
        max_age_hours: 24
```

## Status

Both commands fetch the dump from S3, auto-detect the engine from the dump's content, restore it into an ephemeral `postgres:18` or `mysql:8` container (Postgres custom-format, Postgres plain-SQL, and `mysqldump` plain-SQL are all recognized), run the implicit `restore` check plus the configured `rowcount` / `freshness` / `sql_assert` checks, guarantee container cleanup even on failure, and (if `cloud.api_key` is set) report the result over HTTP:

- `undump check --config ...` — a single pass over every target, then exit. Useful for a one-off run or when you'd rather drive scheduling yourself (cron, systemd timer, CI).
- `undump run --config ...` — a daemon: every target's `schedule` (standard 5-field cron, e.g. `"0 * * * *"`, or `"@every 1h"`) is loaded once at startup and run on its own timer until SIGINT/SIGTERM. A schedule is required on every target for `run` (it's optional and ignored by `check`). Shutdown waits for any restore already in flight to finish and clean up its container before the process exits. If a target's restore outlasts its own schedule, the next tick for that target is skipped rather than piling up concurrent restores.

Check semantics:
- `rowcount` — counts rows in `table`; fails when the count drops more than `max_drop_pct` (default 10%) against the last known good value. Without a previous value (first run of a target since the daemon started, or no cloud configured) it records a baseline and passes.
- `freshness` — fails when `MAX(column)` in `table` is older than `max_age_hours`. The age is computed by the restored database itself, so no timestamp-format guessing.
- `sql_assert` — runs `query` and compares the scalar result with `expect`.

`rowcount`'s delta base (`last_rowcount`) comes from the cloud's response to the previous report and is carried in memory between scheduled runs of the same target — this only accumulates under `run`. A restart of the daemon, or `check`'s one-shot invocations, always start from a fresh baseline.

## Development

Go isn't required on the host — the toolchain runs in a container:

```bash
bash hack/godev.sh test ./...
bash hack/godev.sh run ./cmd/undump check --config undump.example.yaml
bash hack/godev.sh run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...
```

## License

[Business Source License 1.1](LICENSE) — free to read, modify, and run, including in production. The only thing it restricts is reselling `undump` (or a derivative) as a competing hosted restore-testing service. Each release converts to Apache 2.0 four years after publication.
