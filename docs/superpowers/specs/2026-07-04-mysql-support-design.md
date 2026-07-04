# MySQL backup support — design

Status: approved
Date: 2026-07-04

## Problem

`dockerengine` currently only knows Postgres: hardcoded `postgres:18` image,
`pg_isready`/`psql`/`pg_restore`, and format detection limited to
"`PGDMP` magic vs plain SQL". `config.Target.Engine` is parsed from
`undump.yaml` but never consulted — there is no path for MySQL dumps at all.

## Scope

- Support `mysqldump` plain-SQL dumps only (no `.sql.gz`, no
  xtrabackup/physical backups — out of scope for this pass).
- Engine is auto-detected from dump content, not from `config.Target.Engine`.
  `Engine` in config stays parsed-but-unused, same as today.
- Only the `restore` check is affected. Rowcount/freshness/sql_assert checks
  are still unimplemented for every engine (see existing TODO in
  `cmd/undump/main.go`) and are untouched by this change.

## Design

### Engine detection (`internal/dockerengine/format.go`)

Replace `detectFormat` (bool) with `detectEngine` returning a 3-way result:

1. First `len("PGDMP")` bytes equal `PGDMP` → Postgres custom format
   (`pg_restore`).
2. Otherwise, if the file starts with the `-- MySQL dump` header that
   `mysqldump` always emits → MySQL plain SQL (`mysql` CLI).
3. Otherwise → Postgres plain SQL (`psql -f`) — preserves today's fallback
   behavior for any dump that isn't recognizably tagged.

### Engine spec table (`internal/dockerengine/restore.go`)

Introduce an internal (unexported) `engineSpec` describing everything that
varies per engine:

```go
type engineSpec struct {
    image      string
    port       string   // "5432/tcp" | "3306/tcp"
    env        func(password string) []string
    readyCmd   []string
    restoreCmd func(containerDumpPath string) []string
    dsn        func(user, password, host, port string) string
}
```

- **Postgres** (existing behavior, unchanged): `postgres:18`, `5432/tcp`,
  `POSTGRES_PASSWORD`/`POSTGRES_USER`/`POSTGRES_DB`, `pg_isready`,
  `pg_restore --no-owner --no-acl ...` or `psql --set ON_ERROR_STOP=1 -f ...`
  depending on sub-case 1 vs 3 above, `postgresql://` DSN.
- **MySQL** (new): `mysql:8`, `3306/tcp`, `MYSQL_ROOT_PASSWORD` +
  `MYSQL_DATABASE=undump_check`, ready-check `mysqladmin ping -uroot -p<pass>`,
  restore via `mysql -uroot -p<pass> undump_check < <dump>` (executed by
  piping the copied-in file, matching how psql/pg_restore already run
  in-container), `mysql://` DSN.
- DB is pre-created as a fixed name (`undump_check`), restore runs as root:
  if the dump contains its own `CREATE DATABASE`/`USE`, root has the
  privileges to honor it; if it doesn't, the dump lands in the fixed DB.
  Either way restore succeeds without parsing the dump for a DB name.

`Restore()` calls `detectEngine(dumpPath)` before creating the container,
looks up the matching `engineSpec`, and uses it for image/env/port/ready
check/restore command/DSN — replacing the currently-hardcoded Postgres
constants inline.

### Error handling

Same contract as today: infra failures (Docker unreachable, container won't
start, readiness timeout) return `error`; a failed restore of the dump itself
is reflected in `Outcome.OK=false`, not `error`.

## Testing

- `format_test.go`: add `testdata/sample_mysql.sql` (real `mysqldump` plain
  output, small fixture table) and cover all 3 `detectEngine` branches,
  including an empty/short-file case (mirrors current plain-SQL fallback for
  too-short input).
- `restore_test.go`: add a MySQL restore integration case parallel to the
  existing Postgres one (spins `mysql:8`, restores the fixture, asserts
  `Outcome.OK`).

## Out of scope / explicitly deferred

- Compressed dumps (`.sql.gz`).
- Physical/binary backup formats (xtrabackup, mysqlbackup).
- Wiring `config.Target.Engine` into the detection path (still parsed, still
  unused).
- Rowcount/freshness/sql_assert checks for MySQL (blocked on the same
  cross-engine TODO that already blocks them for Postgres).
