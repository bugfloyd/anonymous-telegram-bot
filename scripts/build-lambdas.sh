#!/usr/bin/env bash
# Builds one Lambda zip per backend entry point (backend/cmd/*) into
# <out-dir>/<name>.zip, for the provided.al2023 arm64 runtime.
#
# The output is reproducible: the same source and Go toolchain (pinned in
# backend/go.mod) produce byte-identical zips on any machine, so anyone can
# rebuild and compare them with what is deployed. CI builds twice and checks
# this. The binaries carry no git metadata (-buildvcs=false): which commit a
# bundle came from is recorded by the build attestation instead, so a
# checkout's state (a merge commit, untracked files) can't change the bytes.
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
  # Local settings (GOFLAGS, experiments) must not change the output.
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 GOFLAGS= GOEXPERIMENT= go build \
    -tags lambda.norpc -trimpath -buildvcs=false -ldflags="-s -w" \
    -o "$work/$name/bootstrap" "./$dir"
  # Fixed permissions and timestamp, and no extra attributes, keep the zip
  # byte-identical whatever the umask or clock.
  chmod 0755 "$work/$name/bootstrap"
  touch -d '1980-01-01T00:00:00Z' "$work/$name/bootstrap"
  rm -f "$out/$name.zip"
  (cd "$work/$name" && zip -X -q "$out/$name.zip" bootstrap)
  echo "$(sha256sum "$out/$name.zip" | cut -d' ' -f1)  $name.zip"
done
