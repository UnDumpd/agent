# Configuration reference

Everything the `undump` agent reads comes from a single YAML file, passed explicitly on every invocation:

```bash
undump check --config /path/to/undump.yaml
```

There is no default path, no config auto-discovery, and no environment-variable overrides for individual fields — the file is the whole configuration. A ready-to-copy example lives in [`undump.example.yaml`](undump.example.yaml).

## File layout

```yaml
cloud:            # optional — where to send run reports
  api_key: "env:UNDUMP_API_KEY"   # endpoint defaults to UnDump Cloud SaaS if omitted

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
| `endpoint` | string | Optional. Base URL of the UnDump Cloud API (or anything implementing the same contract). The report is `POST`ed to `{endpoint}/v1/runs`. Defaults to `https://api.undumpd.com` when `api_key` is set — only set this if you're self-hosting the cloud. |
| `api_key` | string | Sent as `Authorization: Bearer <key>`. Accepts `env:`. |

Behavior:

- Reporting is driven by `api_key` alone: leave it unset (or empty) for fully offline operation — a supported mode, not a degraded one. Set it and reports go to `endpoint`, or to `https://api.undumpd.com` if `endpoint` is left blank.
- Delivery has a **15-second timeout** and a failed delivery (network error, non-2xx response) is logged as a warning but **never fails the check run** — the restore test itself is the point; the report is a bonus.
- The payload is JSON containing run metadata: target name, engine, source URI, agent version, timestamps, status, RTO in seconds, dump size in bytes, per-check results (including their values and details), and the error text if the run errored. Backup files, dump contents, and source credentials are never sent. The payload shape is defined in [`internal/models/models.go`](internal/models/models.go).

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

`status` is `pass` (restored, all checks passed), `fail` (restored, but a check failed), or `error` (couldn't even get that far — a source unavailable, Docker unavailable, etc.; the `error` field carries the message).

For a local source, a successful acquisition reports `source_uri` as `local://<basename>` (for example, `local://billing.dump`). If acquisition fails before a file is selected, it reports `local://unresolved`. Local-source runtime error text is also sanitized so reports never contain the configured host path.

The cloud replies with `{"run_id": <int>, "last_rowcount": <int|null>}`, where `last_rowcount` is the value of the target's most recent *passing* `rowcount` check — the delta base for the next run. `undump check`'s one-shot invocations ignore the response body (there is no "next run" to carry it to); `undump run` reads it and feeds it into that target's next scheduled `rowcount` check.

## `targets[]` — the backups under test

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identifier used in console output and reports. |
| `engine` | string | yes | Reporting label (`postgres`, `mysql`, or `mongo`) sent to UnDump Cloud. The restore path is still auto-detected from the dump's content, not from this field. See "The restore environment" below. |
| `schedule` | string (cron) | required for `run`, ignored by `check` | Standard 5-field cron (`"0 * * * *"`) or a `robfig/cron` descriptor (`"@every 1h"`, `"@hourly"`, …). `undump check` always does a single pass over every target and never looks at this field; `undump run` requires it on every target and fails to start otherwise. |
| `source` | object | yes | Where the dump comes from — see below. |
| `checks` | list | no | Data checks to run against the restored database — see below. |

Targets run **sequentially**, in file order. A failure in one target never aborts the pass: the failure is recorded in that target's report and the loop moves on.

### `targets[].source`

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | yes | `s3` or `local`. |
| `uri` | string | for `s3` | Either a full object key (`s3://bucket/path/file.dump`) or a **prefix** ending in `/` (`s3://bucket/path/`). With a prefix, the agent lists the objects under it and picks the one with the most recent `LastModified` — i.e. "always test the newest backup". |
| `path` | string | for `local` | A regular file or directory available to the agent. Relative paths are resolved from the directory containing `undump.yaml`, not the current working directory. For a MongoDB target, point this directly at a `mongodump` collection directory (the folder holding `*.bson`/`*.metadata.json` files, e.g. `<mongodump output>/<dbname>/`) — see "MongoDB dumps" below. |
| `pattern` | string (glob) | no | Narrows S3 prefix selection or local directory selection to entries whose **basename** matches the glob, e.g. `*.dump`. It is invalid with a full S3 object key or an exact local file. |
| `min_age` | string (duration) | no | Local sources only. Minimum age since modification; defaults to `5m`. Uses Go duration syntax such as `30s`, `5m`, or `1h30m`. Applies to both exact files and directory candidates; set `0s` to disable the age guard. |
| `endpoint_url` | string | no | For S3-compatible storage (MinIO, Ceph, Yandex Object Storage, …). Leave empty for AWS. Path-style addressing is always used, which is what non-AWS endpoints expect. |
| `access_key` | string | for `s3` | Accepts `env:`. |
| `secret_key` | string | for `s3` | Accepts `env:`. |
| `region` | string | no | S3 only. Defaults to `us-east-1`. Many S3-compatible services accept any value, but AWS itself will care. |

An exact local file:

```yaml
source:
  type: "local"
  path: "./backups/billing.dump"
  min_age: "5m"
```

A local directory:

```yaml
source:
  type: "local"
  path: "/backups"
  pattern: "*.dump"
  min_age: "10m"
```

Directory scanning is nonrecursive. The agent considers only regular files that are direct children of the directory, applies `pattern` to each basename, excludes files younger than `min_age`, and selects the eligible file with the newest modification time. If modification times tie, the lexically first basename wins.

Source access is **read-only**. For S3, the agent lists and downloads objects, then deletes the temporary download when the target finishes. For local sources, there is no host-side staging copy: the agent reads the original file and transfers it into the ephemeral database container without changing or deleting the source.

#### MongoDB dumps

MongoDB dumps are directory-shaped, not a single file, so they're handled differently from the Postgres/MySQL case above. Point `path` directly at a `mongodump` collection directory — the folder whose direct children are `*.bson` and `*.metadata.json` files, e.g. the output of:

```bash
mongodump --db=mydb --out=/backups/mongo
# restore-test this target against /backups/mongo/mydb
```

The agent recognizes this shape automatically (it looks for a `*.metadata.json` file among the directory's direct children — the same "detect from content, not from config" rule as the Postgres/MySQL signatures below) and treats the whole directory as one dump artifact instead of picking a file from inside it. `pattern` is invalid in this mode. `min_age` applies to the directory's own modification time.

This local-directory path is the only supported way to feed a MongoDB dump into the agent today; an S3-hosted MongoDB dump (a prefix of many objects, rather than one object) isn't supported yet.

### `targets[].checks[]`

Fields are a union across check types; `type` decides which apply.

| `type` | Fields | Meaning |
|---|---|---|
| `rowcount` | `table`, `max_drop_pct` | `SELECT COUNT(*)` on `table` (Postgres/MySQL) or `countDocuments()` on the `table`-named collection (MongoDB); fail if the count dropped more than `max_drop_pct` percent (default **10**; `0` also means the default — use a small positive value to forbid any drop) against the last known good value. With no previous value the check records a baseline and **passes**. |
| `freshness` | `table`, `column`, `max_age_hours` | Fail if the newest value of `column` is older than `max_age_hours` — catches "the backup restores fine but is three weeks old". The age is computed by the restored database itself (`EXTRACT(EPOCH ...)` on Postgres, `TIMESTAMPDIFF` on MySQL, an aggregation `$max` on MongoDB), so no timestamp-format guessing. An empty table/collection or all-null column is a **fail**, not an error. |
| `sql_assert` | `id`, `query`, `expect` | Run a query against the restored database and fail unless the scalar result equals `expect`. `id` names the check in reports (`sql_assert:<id>`). For Postgres/MySQL, `query` is SQL text. For MongoDB, `query` is a `mongosh` expression whose value becomes the checked scalar — write a bare expression such as `db.orders.countDocuments({status:"paid"})`, not a full script. |

> **`sql_assert` privacy:** the returned scalar, configured expected value, and result detail are included in `RunReport` when cloud reporting is enabled. Queries must return only non-sensitive assertion scalars. Do not select emails, tokens, PII, secrets, or other values you do not want reported.
>
> All three check types run for Postgres, MySQL, and MongoDB inside the restored database container. The agent host needs no database client tools. The `restore` check is implicit for every target and does not appear in the config.
>
> For MongoDB, `table` in `rowcount`/`freshness` names a **collection**, not a SQL table.
>
> **`rowcount`'s previous value** comes from the cloud's ingest response (`last_rowcount` — the most recent *passing* rowcount for the target), carried in memory from one scheduled run to the next. `undump check` performs one run per invocation with nowhere to carry that value to, so under `check` every `rowcount` records a baseline and passes; the continuous delta only accumulates under `undump run` (see below), and only resets when the daemon restarts.

## `undump run` — the daemon

`undump run --config undump.yaml` loads the config once, then schedules each target on its own `schedule` using standard cron (5 fields; `robfig/cron` descriptors like `@every 1h` / `@hourly` also work) — no built-in config reload, restart the process to pick up changes. It never returns on its own:

- **Shutdown** is `SIGINT`/`SIGTERM`. A restore already in flight is never interrupted by the signal — the daemon waits for it to finish (and clean up its container, same guarantee as `check`) before exiting. Only *scheduling new* runs stops immediately.
- **Overlap:** if a target's restore takes longer than its own schedule interval, the next due tick for that target is **skipped**, not queued — a slow target degrades to running late rather than piling up concurrent restores against the same Docker host. Other targets are unaffected; scheduling is per-target.
- Every target must have a non-empty `schedule` — `run` fails at startup (before scheduling anything) if any target is missing one, or if a `schedule` doesn't parse as cron.

## The restore environment

Not configurable today, but worth knowing what happens on your Docker host for each target:

- The agent talks to Docker via the standard environment (`DOCKER_HOST` etc., or the mounted `/var/run/docker.sock` when running in the published image).
- Which database engine gets spun up is **auto-detected from the dump's content**, not from `targets[].engine` — that field is reporting-only today. Custom-format `pg_dump` (`PGDMP` magic bytes) and plain-SQL dumps (Postgres, or the default fallback for anything unrecognized) start a **`postgres:18`** container; dumps starting with the `-- MySQL dump` header that `mysqldump` emits start a **`mysql:8`** container; a directory whose direct children include a `mongodump` `*.metadata.json` file starts a **`mongo:8`** container. Checks follow the detected engine too: a mislabeled `engine` never sends the wrong query dialect at the restored database.
- Each container gets a database/user named `undump_check` (MongoDB: a database named `undump_check`, populated by remapping the dump's original database name on restore), storage on `tmpfs` (nothing touches disk), and its default port (5432 for Postgres, 3306 for MySQL, 27017 for MongoDB) published on `127.0.0.1` only, on a random host port. Postgres and MySQL get a random one-shot password; the ephemeral MongoDB container runs without authentication — safe here not because of a shared credential model (there is none), but because, like every check container, it is throwaway, loopback-only, and force-removed on completion.
- If the needed image is missing on the host, the agent **pulls it automatically** before starting the container (a pull failure is an infrastructure error → run status `error`). The pull happens before the RTO timer starts, so a cold image cache doesn't inflate the measured restore time. Pre-pull `postgres:18` / `mysql:8` / `mongo:8` when provisioning only if you want to avoid the one-time download during the first run.
- Readiness is waited for up to **60 seconds**, then the run errors.
- Postgres dump format is detected automatically within the Postgres path too: custom-format dumps go through `pg_restore --no-owner --no-acl`, plain-SQL dumps through `psql --set ON_ERROR_STOP=1` (without which psql happily exits 0 on broken SQL). MySQL support is currently **`mysqldump` plain SQL only** (no `.sql.gz`, no xtrabackup/physical backups) and restores via `mysql -uroot <db> < dump`. MongoDB restores via `mongorestore` against the dump directory described in "MongoDB dumps" above; the official `mongo:8` image already bundles `mongorestore`/`mongosh` (`mongodb-database-tools`), so no extra install step runs at restore time.
- Restore and all data checks run **inside** the container via docker exec — the agent host needs no Postgres, MySQL, or MongoDB client tools.
- The container is force-removed when the target finishes, **including on failure and on infrastructure errors**.

## Exit codes

`undump check` exits non-zero only for startup problems: unreadable config, invalid YAML, an unset `env:` variable, or a misplaced `pattern`. A target that **fails or errors does not change the exit code** — per-target results go to stdout and (optionally) to the cloud. Alerting based on target status should use the report rather than the process exit code.

`undump run` applies the same startup checks, plus: every target must have a non-empty, parseable `schedule`, and the config must declare at least one target. It exits `0` on a clean `SIGINT`/`SIGTERM` shutdown. Same as `check`, an individual target failing or erroring never stops the daemon or changes anything about its exit code — it's reflected in that run's report only.

## Environment variables recap

The agent itself defines no environment variables — you choose the names via `env:` references. A typical Docker invocation:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$(pwd)/undump.yaml:/app/undump.yaml" \
  -v /host/backups:/backups:ro \
  -e S3_ACCESS_KEY=... \
  -e S3_SECRET_KEY=... \
  -e UNDUMP_API_KEY=... \
  ghcr.io/undumpd/agent check --config /app/undump.yaml
```

When the agent itself runs in Docker, a local source's configured `path` is inside the agent container. Mount the host backup directory read-only, as above, and configure `path: "/backups"` (or a file beneath it).

`DOCKER_HOST`, `DOCKER_TLS_VERIFY`, and friends are honored via the standard Docker client environment if you point the agent at a remote Docker daemon instead of mounting the socket.
