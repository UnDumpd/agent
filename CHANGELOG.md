# Changelog

## [0.4.0](https://github.com/UnDumpd/agent/compare/v0.3.0...v0.4.0) (2026-08-17)


### Features

* apply min_age default/validation to S3 sources ([c072ea8](https://github.com/UnDumpd/agent/commit/c072ea896244c30b97e9b707d839c900155ff010))
* restore-test MongoDB dumps from a local mongodump directory ([cf78499](https://github.com/UnDumpd/agent/commit/cf78499aba2dbee21ce125e120328f2de490abf5))
* restore-test MongoDB dumps from an S3 prefix ([3730008](https://github.com/UnDumpd/agent/commit/37300080f931aec5704e4d73136bc5e21dd4b5cd))

## [0.3.0](https://github.com/UnDumpd/agent/compare/v0.2.0...v0.3.0) (2026-07-27)


### Features

* add local backup sources ([9b97a61](https://github.com/UnDumpd/agent/commit/9b97a61708af899f6aad8ecffe9e1801ef5b145d))

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
