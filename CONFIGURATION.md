# Configuration reference

Everything the `undump` agent reads comes from a single YAML file, passed explicitly on every invocation:

```bash
undump check --config /path/to/undump.yaml
```

There is no default path, no config auto-discovery, and no environment-variable overrides for individual fields — the file is the whole configuration. A ready-to-copy example lives in [`undump.example.yaml`](undump.example.yaml).

## File layout

```yaml
cloud:            # optional — where to send run reports
  endpoint: "https://cloud.undump.dev"
  api_key: "env:UNDUMP_API_KEY"

targets:          # one entry per backup you want restore-tested
  - name: "prod-billing"
    engine: "postgres"
    schedule: "0 * * * *"
    source:
      type: "s3"
      uri: "s3://backups/billing/latest.dump"
      endpoint_url: "https://s3.example.com"
      access_key: "env:S3_ACCESS_KEY"
      secret_key: "env:S3_SECRET_KEY"
      region: "eu-central-1"
    checks:
      - type: "rowcount"
        table: "invoices"
        max_drop_pct: 10.0
```

## Secrets: `env:` references

Three fields are secret-bearing and accept an `env:VAR_NAME` reference instead of a literal value:

- `cloud.api_key`
- `targets[].source.access_key`
- `targets[].source.secret_key`

At load time `env:FOO` is replaced with the value of the `FOO` environment variable. If the variable is **not set**, the agent exits with an error before doing anything — an empty-but-exported variable passes, an unset one does not. A literal value (anything not starting with `env:`) is used as-is, but don't do that: config files end up in git, shell history, and backups of their own.

No other fields resolve `env:` references.

## `cloud` — reporting (optional)

| Field | Type | Description |
|---|---|---|
| `endpoint` | string | Base URL of the UnDump Cloud API (or anything implementing the same contract). The report is `POST`ed to `{endpoint}/v1/runs`. |
| `api_key` | string | Sent as `Authorization: Bearer <key>`. Accepts `env:`. |

Behavior:

- If **either** field is empty, reporting is skipped entirely and the agent logs that the cloud is not configured. Fully offline operation is a supported mode, not a degraded one.
- Delivery has a **15-second timeout** and a failed delivery (network error, non-2xx response) is logged as a warning but **never fails the check run** — the restore test itself is the point; the report is a bonus.
- The payload is JSON containing only run metadata: target name, engine, source URI, agent version, timestamps, status, RTO in seconds, dump size in bytes, per-check results, and the error text if the run errored. No table data, no rows, no credentials — the payload shape is defined in [`internal/models/models.go`](internal/models/models.go).

Example payload:

```json
{
  "target_name": "prod-billing",
  "engine": "postgres",
  "source_uri": "s3://backups/billing/latest.dump",
  "agent_version": "0.1.0",
  "started_at": "2026-07-03T10:00:01Z",
  "finished_at": "2026-07-03T10:00:19Z",
  "status": "pass",
  "rto_seconds": 14.32,
  "dump_size_bytes": 104857600,
  "checks": [
    { "name": "restore", "status": "pass", "detail": "restore completed without errors" }
  ]
}
```

`status` is `pass` (restored, all checks passed), `fail` (restored, but a check failed), or `error` (couldn't even get that far — S3 unreachable, Docker unavailable, etc.; the `error` field carries the message).

## `targets[]` — the backups under test

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identifier used in console output and reports. |
| `engine` | string | yes | Reporting label only — the restore path is auto-detected from the dump's content, not from this field. See "The restore environment" below. |
| `schedule` | string (cron) | no | **Reserved.** Parsed but ignored by `undump check`, which always does a single pass over every target. It will drive the future `undump run` daemon mode; until then, schedule the agent externally (cron, systemd timer, CI). |
| `source` | object | yes | Where the dump comes from — see below. |
| `checks` | list | no | Data checks to run against the restored database — see below. |

Targets run **sequentially**, in file order. A failure in one target never aborts the pass: the failure is recorded in that target's report and the loop moves on.

### `targets[].source`

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | yes | Only `s3` is supported today. |
| `uri` | string | yes | Either a full object key (`s3://bucket/path/file.dump`) or a **prefix** ending in `/` (`s3://bucket/path/`). With a prefix, the agent lists the objects under it and picks the one with the most recent `LastModified` — i.e. "always test the newest backup". |
| `pattern` | string (glob) | no | Narrows prefix selection to objects whose **basename** matches the glob, e.g. `*.dump` to skip checksum or log files sitting in the same prefix. Only valid when `uri` is a prefix — combining `pattern` with a full object key is a config error at load time. |
| `endpoint_url` | string | no | For S3-compatible storage (MinIO, Ceph, Yandex Object Storage, …). Leave empty for AWS. Path-style addressing is always used, which is what non-AWS endpoints expect. |
| `access_key` | string | yes | Accepts `env:`. |
| `secret_key` | string | yes | Accepts `env:`. |
| `region` | string | no | Defaults to `us-east-1`. Many S3-compatible services accept any value, but AWS itself will care. |

Access is **read-only**: the agent lists and downloads objects, nothing else. The dump is downloaded into a temporary directory on the agent host and deleted when the target finishes, pass or fail.

### `targets[].checks[]`

Fields are a union across check types; `type` decides which apply.

| `type` | Fields | Meaning |
|---|---|---|
| `rowcount` | `table`, `max_drop_pct` | Fail if the table's row count dropped more than `max_drop_pct` percent against the previous run. |
| `freshness` | `table`, `column`, `max_age_hours` | Fail if the newest value in a timestamp column is older than `max_age_hours` — catches "the backup restores fine but is three weeks old". |
| `sql_assert` | `id`, `query`, `expect` | Run an arbitrary SQL query against the restored database and fail unless the result equals `expect`. `id` names the check in reports. |

> **Current status (v0.1.0):** these three check types are parsed and validated, but **not executed yet** — the agent logs that they'll arrive in a future phase. The one check that always runs is `restore` itself: did the dump actually restore into a live database (Postgres or MySQL) without errors? You don't declare it; it's implicit for every target. Keep the checks in your config — they'll light up when the corresponding agent version ships.

## The restore environment

Not configurable today, but worth knowing what happens on your Docker host for each target:

- The agent talks to Docker via the standard environment (`DOCKER_HOST` etc., or the mounted `/var/run/docker.sock` when running in the published image).
- Which database engine gets spun up is **auto-detected from the dump's content**, not from `targets[].engine` — that field is reporting-only today. Custom-format `pg_dump` (`PGDMP` magic bytes) and plain-SQL dumps (Postgres, or the default fallback for anything unrecognized) start a **`postgres:18`** container; dumps starting with the `-- MySQL dump` header that `mysqldump` emits start a **`mysql:8`** container instead.
- Each container gets a random one-shot password, database `undump_check`, storage on `tmpfs` (nothing touches disk), and its default port (5432 for Postgres, 3306 for MySQL) published on `127.0.0.1` only, on a random host port.
- Both images must already be present on the host — the agent does **not** pull them. Run `docker pull postgres:18` and `docker pull mysql:8` once when provisioning.
- Readiness is waited for up to **60 seconds**, then the run errors.
- Postgres dump format is detected automatically within the Postgres path too: custom-format dumps go through `pg_restore --no-owner --no-acl`, plain-SQL dumps through `psql --set ON_ERROR_STOP=1` (without which psql happily exits 0 on broken SQL). MySQL support is currently **`mysqldump` plain SQL only** (no `.sql.gz`, no xtrabackup/physical backups) and restores via `mysql -uroot <db> < dump`.
- Restore clients run **inside** the container via docker exec — the agent host needs no Postgres or MySQL client tools.
- The container is force-removed when the target finishes, **including on failure and on infrastructure errors**.

## Exit codes

`undump check` exits non-zero only for startup problems: unreadable config, invalid YAML, an unset `env:` variable, or a misplaced `pattern`. A target that **fails or errors does not change the exit code** — per-target results go to stdout and (optionally) to the cloud. If you wire the agent into alerting via exit codes today, parse the output; exit-code semantics for failing targets may tighten in a future release.

## Environment variables recap

The agent itself defines no environment variables — you choose the names via `env:` references. A typical Docker invocation:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$(pwd)/undump.yaml:/app/undump.yaml" \
  -e S3_ACCESS_KEY=... \
  -e S3_SECRET_KEY=... \
  -e UNDUMP_API_KEY=... \
  ghcr.io/undumpd/agent check --config /app/undump.yaml
```

`DOCKER_HOST`, `DOCKER_TLS_VERIFY`, and friends are honored via the standard Docker client environment if you point the agent at a remote Docker daemon instead of mounting the socket.
