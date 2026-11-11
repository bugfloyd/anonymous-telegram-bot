# Hosting

The client is served by GitHub Pages from a hosting repository,
`<org>/<org>.github.io`, in a GitHub organization of its own (design §16):
all Pages sites of one owner share an origin, and with it the keys stored in
the browser. The dev site has its own organization too.

The hosting repository holds no application code, only
[`deploy.yml`](deploy.yml) (as `.github/workflows/deploy.yml`). It builds one
commit (dev) or release tag (production) of this repository, writes
`manifest.json`, attests every file with Sigstore, and deploys with Pages' own
action. This directory keeps the reviewed copy; the hosting repositories run
theirs.

## Setting up a hosting repository

1. Create `<org>/<org>.github.io` (public), with `deploy.yml` as
   `.github/workflows/deploy.yml`.
2. Set Pages to deploy from GitHub Actions:
   `gh api -X PUT repos/<org>/<org>.github.io/pages -f build_type=workflow`.
3. Repository variables: `CODE_REPOSITORY` (`incognitochat/incognitochat`),
   `REFS` (`commits` for dev, `tags` for production), `RELEASE_BRANCH` (`v2`
   while v2 is built), `API_URL` (the `api_url` Terraform output),
   `BOT_USERNAME`, `APP_NAME`, `ISSUER_KEY` (the name of the environment's
   send-token issuer key in `web/issuer-keys.json`), and `WS_URL` (the
   `ws_url` Terraform output; optional: without it the app polls).
4. A fine-grained personal access token, owned by the hosting organization,
   for that one repository, with **Actions: read and write** only. It can start
   the workflow but not change the repository. Store it in this repository's
   environment as `DEV_PAGES_DISPATCH_TOKEN` (dev).
5. In the organization's settings: require two-factor authentication, disallow
   Pages for members and deploy keys, and keep the free plan.

## Checking a live site

```shell
scripts/verify-site.sh https://<org>.github.io <org>/<org>.github.io --rebuild
```

It fetches the manifest and every file, checks each against the manifest and
its attestation (made by the hosting repository's `deploy.yml`), and with
`--rebuild` builds the same commit with the same settings and compares the
bytes.
