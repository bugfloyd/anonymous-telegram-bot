# Incognito Chat

Anonymous messaging through personal links, with end-to-end encryption.

> **Status:** version 2 is being built on this branch (`v2`). Version 1, an
> in-chat Telegram bot, keeps running from `main` and is kept at tag
> [`v1`](../../tree/v1).

Version 2 moves all messaging into a Telegram Mini App, a web app that
encrypts messages on the device before they leave it. The server only ever
holds encrypted messages waiting for delivery, and doesn't record who wrote to
whom. A full description of the security model, including what users still
have to trust, will be published here before release.

## Layout

| Path | What |
|---|---|
| `backend/` | Go, one module. `cmd/` holds one AWS Lambda entry point per trust boundary: `api` (HTTPS API), `bot` (Telegram webhook), `ws` (WebSocket connections), `notify` (Telegram notices). |
| `web/` | The client: TypeScript, Vite and Preact. Runs as a Telegram Mini App and in a browser. |
| `infra/` | App Terraform for one environment: DynamoDB tables (TTL, no point-in-time recovery), SSM parameters written write-only, the four functions with function URLs, the WebSocket API. Applied by CI. |
| `infra/init/` | Bootstrap Terraform, one AWS account per environment: state bucket, GitHub OIDC provider, deploy role, Lambda permissions boundary. Applied by an account admin with `scripts/bootstrap.sh`; CI can't change its own permissions. |
| `scripts/` | `bootstrap.sh`, and `build-lambdas.sh` for reproducible Lambda bundles. |

## Checks

Everything CI runs can be run locally.

Backend (the Go toolchain is pinned in `backend/go.mod`):

```shell
cd backend
gofmt -l .
go vet ./...
go test -race ./...
../scripts/build-lambdas.sh   # prints the SHA-256 of each bundle
```

The DynamoDB store tests skip unless DynamoDB Local is running (CI always runs
them):

```shell
podman run --rm -d -p 8000:8000 docker.io/amazon/dynamodb-local:3.3.2
DYNAMODB_ENDPOINT=http://localhost:8000 go test -race ./...
```

Web (Node is pinned in `web/.nvmrc`):

```shell
cd web
npm ci
npm audit signatures
npm run typecheck
npm test
npm run build
```

## Environments

Each environment has its own AWS account and GitHub environment. While v2 is
being built, pushes to `v2` deploy to **dev**, and pull requests never get
cloud access. An account admin bootstraps an environment once:

```shell
AWS_PROFILE=<profile> DEPLOY_BRANCH=v2 scripts/bootstrap.sh <aws-region> dev
gh secret set BOT_TOKEN --env dev
gh variable set CLIENT_ORIGINS --env dev --body '["https://<client-origin>"]'
```

While invite-only (the default; set the variable `INVITE_ONLY` to `false` to
open it), only accounts that joined through an invitation, and admins, can
own links. Admins are listed by Telegram user ID in a secret, so the IDs stay
out of the public deploy logs; the API keeps only their keyed hashes:

```shell
gh secret set ADMIN_TELEGRAM_IDS --env dev --body '<id>,<id>'
```

Sends are paid for with anonymous tokens signed by an issuer key: an RSA-2048
private key in a secret, whose public key is published in
`web/issuer-keys.json` (the client only accepts that one):

```shell
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out issuer.pem
openssl pkey -in issuer.pem -pubout -outform DER | basenc -w0 --base64url  # the "spki" to publish
gh secret set ISSUER_KEY --env dev < issuer.pem && shred -u issuer.pem
```

The dev client is published to its own GitHub Pages site, in a separate
GitHub organization: sites of one owner share an origin, and with it the keys
stored in the browser, so dev must never share production's. The site's own
workflow builds, attests and deploys each `v2` commit; CI only starts it, with
a token that can do nothing else. See [hosting/](hosting/README.md), and check
the live site with:

```shell
scripts/verify-site.sh https://incognitochat-dev.github.io incognitochat-dev/incognitochat-dev.github.io --rebuild
```

The client takes its settings at build time (`VITE_API_URL`, `VITE_BOT_USERNAME`,
`VITE_APP_NAME`, the hosting repository's variables); the API's origin is added to the page's Content Security
Policy. A build without them works but can't reach a backend.

Secrets (the bot token, the session key, the Telegram ID pepper, the webhook
secret) are written to SSM through Terraform write-only arguments, so they
never land in Terraform state or plans. Each function's role can read only the
parameters it needs; only the `bot` and `notify` functions can read the bot
token.

## Reproducible builds

The same commit always produces byte-identical Lambda bundles and web files.
CI builds everything twice and fails if anything differs, so anyone can
rebuild a release and compare it with what is deployed.

## Supply chain

- GitHub Actions are pinned to commit SHAs; Terraform provider lock files are committed.
- npm installs exact versions only, runs no install scripts, accepts no git
  dependencies, and refuses packages published less than 7 days ago
  (`web/.npmrc`). CI verifies registry signatures.
- `govulncheck` runs on every build; Dependabot proposes updates with a 7-day cooldown.
- No environment-specific values (account IDs, bucket names, bot names, tokens)
  are committed.
