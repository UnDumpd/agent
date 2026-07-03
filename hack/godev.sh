#!/usr/bin/env bash
# Runs the Go toolchain inside a container — Go is not installed on the host.
# Usage (from agent/):
#   bash hack/godev.sh build ./...
#   bash hack/godev.sh test ./... -v
#   bash hack/godev.sh run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...
set -eu
cd "$(dirname "$0")/.."
REPO_ROOT="$(cd .. && pwd)"

MSYS_NO_PATHCONV=1 docker run --rm \
  -v "$REPO_ROOT:/workspace" \
  -w /workspace/agent \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v undump-go-mod-cache:/go/pkg/mod \
  -v undump-go-build-cache:/root/.cache/go-build \
  --network undump_default \
  golang:1.25 go "$@"
