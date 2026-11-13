# Development

How the code is laid out, how to run every check CI runs, and the rules for
dependencies. Deploying is in [deployment.md](deployment.md).

## Layout

| Path | What |
|---|---|
| `backend/` | Go, one module. `cmd/` has one Lambda entry point per trust boundary: `api` (HTTPS API), `bot` (Telegram webhook), `ws` (WebSocket connections), `notify` (Telegram notices). `internal/` holds the packages: `api` (routes and handlers), `telegram` (launch data and the Bot API), `session` and `devicesig` (logins and signed requests), `envelope`, `inbox`, `links`, `invitations`, `tokens` (blind-signed send tokens), `media`, `notices`, `push`, and the DynamoDB stores `directory` and `ephemeral`. |
| `web/` | The app: TypeScript, Vite, Preact with signals. `src/crypto` (WebCrypto only: keys, HPKE, signatures, blind RSA), `src/protocol` (envelopes, messages, links), `src/messaging` (the engine: sending, syncing, rotation, the history store), `src/media` (preparing, recording, saving attachments), `src/stickers`, `src/storage` (IndexedDB), `src/platform` (the Telegram bridge, launch data, theme), `src/net` (API and socket), `src/state` (screens and actions), `src/i18n` (English and Persian), `app.tsx` (the screens). |
| `web/public/` | Files copied into the build as they are: the sticker images and their license. |
| `infra/` | App Terraform for one environment: tables, the media bucket, SSM parameters, the four functions with function URLs, the WebSocket API. Applied by CI. |
| `infra/init/` | Bootstrap Terraform, applied once per AWS account by an admin: state bucket, GitHub OIDC provider, deploy role, Lambda permissions boundary, code signing. CI can't change its own permissions. |
| `hosting/` | The reviewed copy of the hosting repository's workflow ([verifying.md](verifying.md)). |
| `scripts/` | `bootstrap.sh`, `build-lambdas.sh` (reproducible Lambda bundles), `check-lambda-deploy.py` (checks a deploy runs the attested bundles), `verify-site.sh`. |
| `docs/` | This documentation. |

## Checks

Everything CI runs can be run locally.

**Backend** (the Go toolchain is pinned in `backend/go.mod`):

```shell
cd backend
gofmt -l .
go vet ./...
go test -race ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
../scripts/build-lambdas.sh   # prints the SHA-256 of each bundle
```

The DynamoDB store tests skip unless DynamoDB Local is running (CI always runs
them):

```shell
podman run --rm -d -p 127.0.0.1:8000:8000 docker.io/amazon/dynamodb-local:3.3.2
DYNAMODB_ENDPOINT=http://localhost:8000 go test -race ./...
```

**Web** (Node is pinned in `web/.nvmrc`; the tests need it, for Web Locks):

```shell
cd web
npm ci
npm audit signatures
npm run typecheck
npm test
npm run build
```

**Terraform:**

```shell
terraform fmt -check -recursive -diff infra
for dir in infra infra/init; do
  terraform -chdir="$dir" init -backend=false -lockfile=readonly
  terraform -chdir="$dir" validate
done
```

## Reproducible builds

The same commit always produces byte-identical Lambda bundles and web files,
and CI builds everything twice and compares. Keep it that way:

- Go builds use `-trimpath -buildvcs=false`, a pinned toolchain, fixed file
  modes, and zip timestamps in UTC (`scripts/build-lambdas.sh`).
- The web build is one deterministic JavaScript file and one stylesheet, with
  no lazy chunks: a deploy replaces the whole site, so a cached page must never
  point at chunks that are gone. Static files the app fetches (such as sticker
  images) go in `web/public/` and are fetched by path. Importing them through
  Vite (`?url`) puts randomly ordered file references in the source map.
- The build settings come from `VITE_*` variables, checked by
  `web/vite.config.ts`. A build without them works but can't reach a backend.

## Dependencies and supply chain

- GitHub Actions are pinned to commit SHAs; Terraform provider lock files are
  committed.
- npm installs exact versions only, runs no install scripts, accepts no git
  dependencies, and refuses packages published less than 7 days ago
  (`web/.npmrc`). CI verifies registry signatures.
- `govulncheck` runs on every build; Dependabot proposes updates with a 7-day
  cooldown.
- The app loads no third-party code at runtime. Its only runtime dependencies
  are Preact, its signals, and `idb`.

## Conventions

- **No environment-specific values in the repository:** no account IDs,
  bucket names, bot names, URLs or tokens. They live in GitHub environment
  variables and secrets, Terraform outputs, and the hosting repository's
  variables. Docs use placeholders such as `<bot>`, `<app>` and `<org>`.
- Commits and pull request titles follow Conventional Commits. Changes to
  `main` go through pull requests, squash-merged; a push to `main` deploys to
  dev.
- The protocol is documented in [protocol.md](protocol.md) together with the
  code that implements it; change both in the same pull request.
- User-facing text is written in English and Persian (`web/src/i18n`), and
  layout uses logical CSS properties, so right-to-left needs no special cases.
