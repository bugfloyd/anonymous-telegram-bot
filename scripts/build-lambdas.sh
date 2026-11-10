#!/usr/bin/env bash
# Builds one Lambda zip per backend entry point (backend/cmd/*) into
# <out-dir>/<name>.zip, for the provided.al2023 arm64 runtime.
#
# The output is reproducible: the same commit and Go toolchain (pinned in
# backend/go.mod) produce byte-identical zips, so anyone can rebuild and
# compare them with what is deployed. CI builds twice and checks this.
#
# Usage: scripts/build-lambdas.sh [out-dir]   (default: backend/dist)
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
out=$(mkdir -p "${1:-$root/backend/dist}" && cd "${1:-$root/backend/dist}" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cd "$root/backend"
for dir in cmd/*/; do
  name=$(basename "$dir")
  mkdir -p "$work/$name"
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
    -tags lambda.norpc -trimpath -ldflags="-s -w" \
    -o "$work/$name/bootstrap" "./$dir"
  # A fixed timestamp and no extra attributes keep the zip byte-identical.
  touch -d '1980-01-01T00:00:00Z' "$work/$name/bootstrap"
  rm -f "$out/$name.zip"
  (cd "$work/$name" && zip -X -q "$out/$name.zip" bootstrap)
  echo "$(sha256sum "$out/$name.zip" | cut -d' ' -f1)  $name.zip"
done
