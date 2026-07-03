#!/usr/bin/env bash
# Заливает тестовые дампы testdata/ в локальное S3-совместимое хранилище
# для интеграционных тестов internal/sources/s3 и internal/dockerengine.
# Требует поднятый docker-compose из соседней cloud-репы (сеть undump_default,
# сервис minio) и testdata/sample_*.
set -euo pipefail
cd "$(dirname "$0")/.."

MSYS_NO_PATHCONV=1 docker run --rm --network undump_default \
  -v "$(pwd)/testdata:/fixtures:ro" \
  --entrypoint sh minio/mc:latest -c '
    mc alias set local http://minio:9000 minioadmin minioadmin &&
    mc mb -p local/undump-test &&
    mc cp /fixtures/sample_custom.dump local/undump-test/dumps/exact.dump &&
    mc cp /fixtures/sample_custom.dump local/undump-test/dumps/prefix/2026-06-30T00-00-00.dump &&
    mc cp /fixtures/sample_custom.dump local/undump-test/dumps/prefix/2026-07-01T00-00-00.dump &&
    mc cp /fixtures/sample_plain.sql local/undump-test/dumps/exact.sql &&
    mc cp /fixtures/sample_custom.dump local/undump-test/dumps/patterned/2026-07-01T00-00-00.dump &&
    mc cp /fixtures/sample_plain.sql local/undump-test/dumps/patterned/2026-07-02T00-00-00.sql &&
    mc ls --recursive local/undump-test
  '
