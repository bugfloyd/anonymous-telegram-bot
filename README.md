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
| `infra/init/` | Bootstrap Terraform: state bucket, GitHub OIDC provider, deploy role, Lambda permissions boundary. Applied by an account admin with `scripts/bootstrap.sh`. |
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

Web (Node is pinned in `web/.nvmrc`):

```shell
cd web
npm ci
npm audit signatures
npm run typecheck
npm test
npm run build
```

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
