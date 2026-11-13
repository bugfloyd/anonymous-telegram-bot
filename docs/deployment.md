# Deployment

How an environment is set up and deployed. Each environment (dev, production)
has its own AWS account, its own GitHub environment in this repository, its
own bot, and its own hosting organization and repository for the app.

Nothing environment-specific is committed: account IDs, bucket names, bot
names, URLs and tokens live in GitHub environment secrets and variables and in
the hosting repository's variables. Below, `<env>`, `<bot>`, `<app>` and
`<org>` are placeholders.

## How a deploy runs

- A push to `main` runs CI, which deploys to **dev**. Pull requests never get
  cloud access.
- CI assumes the environment's deploy role through GitHub's OIDC provider (no
  stored AWS keys), builds the Lambda bundles reproducibly, and applies
  `infra/` with Terraform. The deploy role is limited by the bootstrap stack
  and can't change its own permissions.
- It then sets up the bot (the function registers its own webhook, menu
  button and commands, so the bot token never passes through CI), and runs
  smoke tests: the API, the webhook and the socket refuse what they should.
- Last, it asks the hosting repository to build and deploy the app from the
  same commit, and waits until the site serves it ([verifying.md](verifying.md),
  [hosting/](../hosting/README.md)).

## Setting up an environment

An account admin bootstraps an AWS account once (and reruns it to apply
changes to the bootstrap stack):

```shell
AWS_PROFILE=<profile> scripts/bootstrap.sh <aws-region> <env>
```

It creates the Terraform state bucket, the GitHub OIDC provider, the deploy
role and the Lambda permissions boundary (`infra/init`), and the GitHub
environment `<env>`, which accepts deploys from `main` only, with the secrets
and variables it computed (`AWS_ACCOUNT_ID`, `AWS_ROLE_ARN`,
`TF_STATE_BUCKET`, `AWS_REGION`).

Then set the rest:

```shell
gh secret set BOT_TOKEN --env <env>
gh variable set CLIENT_ORIGINS --env <env> --body '["https://<org>.github.io"]'
```

| Name | Kind | What |
|---|---|---|
| `BOT_TOKEN` | secret | The bot's token from BotFather. Raise `BOT_TOKEN_VERSION` to push a changed token. |
| `ISSUER_KEY` | secret | The send-token issuer's private key (below). Raise `ISSUER_KEY_VERSION` when it changes. |
| `ADMIN_TELEGRAM_IDS` | secret | Comma-separated Telegram user IDs of admins. A secret, so the IDs stay out of the public deploy logs; the API keeps only their keyed hashes. |
| `CLIENT_ORIGINS` | variable | Origins allowed to call the API from a browser: the app's `https://<org>.github.io`. |
| `INVITE_ONLY` | variable | `true` by default: only accounts that joined through an invitation, and admins, can own links. |
| `DELETION_PROTECTION` | variable | Protects the tables from deletion. Keep it on in production. |
| `SECRETS_VERSION` | variable | Raise it to replace the generated secrets (session key, Telegram-ID pepper, webhook secret). Replacing the pepper starts every account over. |
| `PAGES_REPO`, `PAGES_URL` | variables | The hosting repository and the site's address, for starting its deploy and waiting for it. |
| `DEV_PAGES_DISPATCH_TOKEN` | secret | A token that can only start the hosting repository's workflow (dev). |

### The send-token issuer key

Sends are paid with anonymous tokens signed by the environment's issuer key,
an RSA-2048 private key kept only as a secret. Its public key is published in
[`web/issuer-keys.json`](../web/issuer-keys.json), and an app build accepts
only the key its build names, so the server can't give users different keys:

```shell
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out issuer.pem
openssl pkey -in issuer.pem -pubout -outform DER | basenc -w0 --base64url  # the "spki" to publish
gh secret set ISSUER_KEY --env <env> < issuer.pem && shred -u issuer.pem
```

Add the public key to `web/issuer-keys.json` under a new name with its
`keyId` (the SHA-256 of the DER), and set the hosting repository's
`ISSUER_KEY` variable to that name.

### The bot

In BotFather, for the environment's bot:

- a direct-link Mini App `<app>` pointing at `https://<org>.github.io/`,
  with "request write access" on;
- the main Mini App and the menu button pointing at the same address;
- the privacy policy ([privacy.md](privacy.md)), which Telegram's bot
  developer terms require.

### The app's hosting

The app is served from `<org>/<org>.github.io` in a GitHub organization of its
own. How to create and configure it, and the variables it needs (`API_URL`,
`WS_URL`, `MEDIA_ORIGIN` from Terraform's outputs, `BOT_USERNAME`,
`APP_NAME`, `ISSUER_KEY`), is in [hosting/README.md](../hosting/README.md).

## Secrets at rest

Secrets (the bot token, the session key, the Telegram-ID pepper, the webhook
secret, the issuer key, the key that seals chat IDs) are SSM parameters
written through Terraform's write-only arguments, so they never land in
Terraform state or plans. Each function's role can read only the parameters
it needs: only `bot` and `notify` can read the bot token.

## Cost guards

The tables are on-demand with per-table caps on read and write units, the
WebSocket API is throttled, and nothing runs when idle. Logs are kept 7 days.
