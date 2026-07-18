# Changelog

## [0.3.0](https://github.com/UnDumpd/agent/compare/v0.2.0...v0.3.0) (2026-07-18)


### Features

* add cloud reporting and scheduled runs ([c5ecf7b](https://github.com/UnDumpd/agent/commit/c5ecf7b5b4d1c3bd295f36f47d6a84f687772a4c))
* add integrity checks ([e963588](https://github.com/UnDumpd/agent/commit/e96358826d758c632d730794eb7005418617346a))
* fetch backups from s3 ([8f42ec7](https://github.com/UnDumpd/agent/commit/8f42ec7c309aa2eb06847fa17fc04085bf92c27f))
* load backup targets from yaml ([2914f31](https://github.com/UnDumpd/agent/commit/2914f310da7e2610005b15860cda10b9acb4ba23))
* restore postgres and mysql dumps ([de8a11a](https://github.com/UnDumpd/agent/commit/de8a11a7385fa37a925c7db012b474475e8b47f5))

## [0.2.0](https://github.com/UnDumpd/agent/compare/v0.1.0...v0.2.0) (2026-07-09)


### Features

* add check runner registry ([0626f71](https://github.com/UnDumpd/agent/commit/0626f71e377ac806f3c0a1af853f540b6a79638a))
* add undump run daemon with per-target cron scheduling ([d13266b](https://github.com/UnDumpd/agent/commit/d13266b646be716116c962257a8704c63422ae08))
* auto-pull restore image when missing on the host ([eb88ba0](https://github.com/UnDumpd/agent/commit/eb88ba0571483ababe210dc742a2c2a9a17dfdf6))
* detect and restore MySQL dumps via a mysql:8 container ([e34b443](https://github.com/UnDumpd/agent/commit/e34b44312690b9b79b8e9f96fc3b2fb0d6eecf62))
* implement rowcount and freshness checks ([7be3d47](https://github.com/UnDumpd/agent/commit/7be3d4742a65e0b216f06cef4ac535b886d5a277))
* run sql assert checks ([f4fdb98](https://github.com/UnDumpd/agent/commit/f4fdb98b5cd90578f9d2d804c0cc4d6ca91cd888))


### Bug Fixes

* drive check SQL dialect by the detected engine, not the config label ([f0db1ff](https://github.com/UnDumpd/agent/commit/f0db1ffbaee36a65706fd27a8e844327e4419f33))
* update IMAGE_NAME in Docker build workflow to 'undump/agent' ([ff60f21](https://github.com/UnDumpd/agent/commit/ff60f21f40bfc17361f9fda89f3b504eafb79b82))
* update IMAGE_NAME to use repository context in Docker build workflow ([4f41aab](https://github.com/UnDumpd/agent/commit/4f41aabb15cd8e0f3d2f4164f6291963e68f4be0))
