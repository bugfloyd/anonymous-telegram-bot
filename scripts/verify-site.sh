#!/usr/bin/env bash
# Checks that a live site is exactly what its hosting workflow built and
# attested, and optionally that rebuilding the same commit gives the same
# bytes (design §16).
#
#   scripts/verify-site.sh <site URL> <hosting repository> [--rebuild]
#   scripts/verify-site.sh https://incognitochat.github.io incognitochat/incognitochat.github.io --rebuild
#
# Needs curl, jq, sha256sum, and the GitHub CLI (gh attestation verify works
# without logging in for public repositories). --rebuild also needs git and
# Node (the version in web/.nvmrc).
set -euo pipefail

site=${1:?site URL}
repo=${2:?hosting repository}
rebuild=${3:-}
site=${site%/}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "Fetching ${site}/manifest.json"
curl -fsS --max-time 30 "${site}/manifest.json?nocache=$RANDOM" -o "$work/manifest.json"
commit=$(jq -r .commit "$work/manifest.json")
echo "Built from $(jq -r .repository "$work/manifest.json") $(jq -r .ref "$work/manifest.json") (commit ${commit})"

failed=0
check() { # path expected-sha256
  local path=$1 want=$2 file="$work/site/$1"
  mkdir -p "$(dirname "$file")"
  curl -fsS --max-time 30 "${site}/${path}?nocache=$RANDOM" -o "$file"
  local got
  got=$(sha256sum "$file" | cut -d' ' -f1)
  if [ "$got" != "$want" ]; then
    echo "MISMATCH  ${path}"
    failed=1
  elif ! gh attestation verify "$file" --repo "$repo" --signer-workflow "${repo}/.github/workflows/deploy.yml" >/dev/null 2>&1; then
    echo "UNATTESTED ${path}"
    failed=1
  else
    echo "ok        ${path}"
  fi
}

while IFS=$'\t' read -r path sha; do
  check "$path" "$sha"
done < <(jq -r '.files | to_entries[] | "\(.key)\t\(.value)"' "$work/manifest.json")
# The manifest itself is attested too.
if gh attestation verify "$work/manifest.json" --repo "$repo" --signer-workflow "${repo}/.github/workflows/deploy.yml" >/dev/null 2>&1; then
  echo "ok        manifest.json"
else
  echo "UNATTESTED manifest.json"
  failed=1
fi

if [ "$rebuild" = --rebuild ]; then
  echo "Rebuilding ${commit}"
  git clone -q "https://github.com/$(jq -r .repository "$work/manifest.json").git" "$work/code"
  git -C "$work/code" checkout -q "$commit"
  (
    cd "$work/code/web"
    npm ci --silent
    VITE_API_URL=$(jq -r .build.VITE_API_URL "$work/manifest.json") \
    VITE_BOT_USERNAME=$(jq -r .build.VITE_BOT_USERNAME "$work/manifest.json") \
    VITE_APP_NAME=$(jq -r .build.VITE_APP_NAME "$work/manifest.json") \
    VITE_ISSUER_KEY=$(jq -r '.build.VITE_ISSUER_KEY // empty' "$work/manifest.json") \
    VITE_WS_URL=$(jq -r '.build.VITE_WS_URL // empty' "$work/manifest.json") \
      npm run build --silent >/dev/null
  )
  while IFS=$'\t' read -r path sha; do
    got=$(sha256sum "$work/code/web/dist/$path" 2>/dev/null | cut -d' ' -f1 || true)
    if [ "$got" != "$sha" ]; then
      echo "REBUILD DIFFERS ${path}"
      failed=1
    fi
  done < <(jq -r '.files | to_entries[] | "\(.key)\t\(.value)"' "$work/manifest.json")
  [ "$failed" = 0 ] && echo "Rebuild matches byte for byte"
fi

if [ "$failed" != 0 ]; then
  echo "VERIFICATION FAILED"
  exit 1
fi
echo "Verified: every file matches the manifest and is attested by ${repo}"
