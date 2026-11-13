# Verifying the app

The encryption only protects you if the app you run is the app in this
repository. Inside Telegram, nothing stops whoever runs the bot from pointing
it at a different page ([security.md](security.md#what-users-still-have-to-trust)),
so the app is built so that anyone can check what is actually served.

## How the app is published

- **Reproducible builds.** The same commit always produces byte-identical
  files. CI builds the app twice and fails if anything differs; it does the
  same for the backend's Lambda bundles.
- **A hosting repository of its own.** The app is served by GitHub Pages at
  the root of a dedicated GitHub organization's `github.io` address. All Pages
  sites of one owner share one web origin, and with it the keys stored in the
  browser, so that organization hosts nothing else, and no custom domain is set
  (whoever controlled the domain's DNS could otherwise serve a page on the same
  origin). The dev site has a separate organization, so it never shares keys
  with production.
- **Built where it's served.** The hosting repository holds no application
  code, only a workflow ([`hosting/deploy.yml`](../hosting/deploy.yml)) that
  checks out one commit (dev) or release tag (production) of this repository,
  builds it, writes `manifest.json` (the commit, the build settings and the
  SHA-256 of every file), attests every file and the manifest with Sigstore,
  and deploys with GitHub's own Pages action. This repository can only start
  that workflow, with a token that can do nothing else.
- **Locked down at runtime.** A strict Content Security Policy allows only the
  app's own scripts, and connections only to the API, the socket and the
  attachment bucket. The app loads no third-party code, and no code or settings
  from the backend.

## Checking a live site

```shell
scripts/verify-site.sh https://<org>.github.io <org>/<org>.github.io --rebuild
```

The script:

1. fetches `manifest.json` and every file it lists;
2. checks each file's SHA-256 against the manifest;
3. checks each file's attestation with `gh attestation verify`, which proves
   it was built by the hosting repository's own workflow (public Sigstore
   transparency log);
4. with `--rebuild`, clones this repository at the manifest's commit, builds
   it with the same settings, and compares every byte.

It needs `curl`, `jq`, `sha256sum` and the GitHub CLI; `--rebuild` also needs
`git` and the Node version in [`web/.nvmrc`](../web/.nvmrc).

## What this proves, and what it doesn't

- **Proves:** the files the site serves right now are exactly what this
  repository's code builds into, for everyone who fetches them.
- **Doesn't prove** what one particular person was served inside Telegram. A
  bot owner could point the app at another address, for everyone or for
  chosen people. A page on any other address has none of the keys on your
  device: it would have to make you set up again, which ends your account
  loudly on your old device and changes your link.
- **Covers the backend only through the operator's records** (below). The
  design doesn't rely on the backend: a malicious one can drop or delay
  messages and watch timing, but can't read content or forge messages,
  because apps check every key themselves.

Native apps with reproducible builds, and web standards for pinning a site's
code (WEBCAT, WAICT), would close the first gap; they're on the
[list of future improvements](security.md#how-the-remaining-gaps-could-close).

## The backend

Users can't look into the AWS account, so the backend is checked through
what CI publishes:

- **Reproducible and attested.** The four Lambda bundles are byte-identical
  for the same commit (`scripts/build-lambdas.sh` prints their SHA-256), and
  each deploy attests them with Sigstore before deploying them.
- **Only signed code runs.** Each bundle is signed with the environment's AWS
  Signer profile, and every function has a code signing config that makes
  Lambda refuse anything not signed by it. CI is the only place that signs;
  the deploy role can't change the profile or the config, nor create or keep
  a function without it.
- **Each deploy checks itself.** After deploying, CI fetches the code each
  function runs from Lambda and checks that it is the attested bundle plus
  AWS's signature, and nothing else (`scripts/check-lambda-deploy.py`). The
  public log of the "Check the deployed code" step shows both hashes.

To check a deploy, rebuild its commit and compare:

```shell
git checkout <commit> && cd backend && ../scripts/build-lambdas.sh
gh attestation verify dist/api.zip --repo <org>/<org>
```

The hashes printed should match the deploy log, and the attestation proves
the bundle was built by this repository's workflow.

What this doesn't prove: whoever controls the AWS account can switch code
signing off and deploy something else, and only the account's own records
(CloudTrail) would show it. Change alerts held by an independent party are a
[future improvement](security.md#how-the-remaining-gaps-could-close).
