# Telegram Incognito Bot

A Telegram bot for receiving anonymous messages. Each user gets a personal link. Anyone who opens it can send them a message (any media type) without revealing who they are, and the recipient can open, reply to, or block the sender. Message content is never stored: it is copied from the sender's chat when the recipient opens it.

Runs on AWS: API Gateway (HTTP API) → Lambda (Go, arm64) → DynamoDB, with secrets in Secrets Manager. All infrastructure is Terraform and is deployed by GitHub Actions.

## Architecture & security

| Area | How |
|---|---|
| CI/CD auth | GitHub Actions gets short-lived credentials via OIDC; no AWS keys are stored anywhere. |
| Who can deploy | Only jobs in the `production` GitHub environment can assume the deploy role, and that environment only accepts the `main` branch. Pull requests run tests and checks without any cloud access. |
| Deploy permissions | Least privilege: the deploy role can only manage this bot's own resources, by name. The Lambda role it creates must carry a permissions boundary, so it can't be used to escalate privileges. |
| Bot token | Stored as a GitHub environment secret and written to Secrets Manager through a Terraform write-only argument, so it never lands in Terraform state, plan files, or the repo. |
| Webhook | Telegram signs each update with a secret token derived from the bot token. The Lambda rejects anything else with `401`, and every deploy smoke-tests that. |
| Endpoint abuse | API Gateway throttling, access logs, 30-day log retention. |
| Data | DynamoDB with point-in-time recovery and deletion protection. |
| State | Encrypted, versioned, TLS-only S3 bucket with S3-native locking. |
| Supply chain | Actions pinned to commit SHAs, provider lock files committed, `govulncheck` on every build, Dependabot for actions, Go modules, and Terraform. |
| Public repo hygiene | No environment-specific values (account IDs, bucket names, profiles, tokens) are committed. They live in GitHub environment secrets/variables and are masked in workflow logs. |

Two Terraform stacks:

- `infra/init` (**bootstrap**): state bucket, GitHub OIDC provider, deploy role and policy, Lambda permissions boundary. An account admin applies it with `scripts/bootstrap.sh`. CI can't change its own permissions.
- `infra` (**app**): Lambda, API Gateway, DynamoDB, Secrets Manager, logs. Applied by CI on every push to `main`.

## Deployment

### 1. Bootstrap (once per AWS account, or when `infra/init` changes)

Requirements: admin credentials for the target AWS account, the [GitHub CLI](https://cli.github.com/) logged in as a repo admin, Terraform ≥ 1.11 and Python 3.

```shell
AWS_PROFILE=<AWS_PROFILE> scripts/bootstrap.sh <AWS_REGION>
```

This creates the AWS resources above (keeping the bootstrap state in the new bucket) and configures the GitHub `production` environment:

| Name | Kind | Value |
|---|---|---|
| `AWS_ROLE_ARN` | secret | deploy role ARN |
| `AWS_ACCOUNT_ID` | secret | target account (deploys refuse any other account) |
| `TF_STATE_BUCKET` | secret | Terraform state bucket |
| `AWS_REGION` | variable | target region |

### 2. Set the bot token (once)

Create a bot with [@BotFather](https://t.me/BotFather) (or reuse one) and store its token. Typing it at the prompt keeps it out of files and shell history:

```shell
gh secret set BOT_TOKEN --env production
```

### 3. Deploy

Push or merge to `main`, or run the **CI/CD** workflow manually on `main`. The pipeline:

1. tests, vets, and vulnerability-scans the bot, then builds the Lambda bundle
2. checks Terraform formatting and validates both stacks
3. assumes the deploy role via OIDC, runs `terraform plan` and `apply`
4. registers the Telegram webhook (with its secret token)
5. smoke-tests that unauthenticated requests are rejected

Optional `production` environment variables:

| Variable | Default | Purpose |
|---|---|---|
| `DEFAULT_LANGUAGE` | *(empty: detect from client)* | `en_US` or `fa_IR` |
| `BOT_SECRET_VERSION` | `1` | Increase after changing `BOT_TOKEN` so the new token is written to Secrets Manager |
| `INVITE_ONLY` | `true` | Set to `false` to open the bot to everyone (see [Invitations](#invitations)) |

Optional `production` environment secret:

| Secret | Purpose |
|---|---|
| `ADMIN_TELEGRAM_IDS` | Comma-separated Telegram user IDs of admins (see [Invitations](#invitations)) |

### Rotating the bot token

1. In @BotFather, `/revoke` the token and copy the new one.
2. `gh secret set BOT_TOKEN --env production`
3. `gh variable set BOT_SECRET_VERSION --env production --body <previous + 1>`
4. Run the workflow on `main`. It updates the secret, restarts the Lambda environments, and re-registers the webhook.

### Tearing down

The DynamoDB table has deletion protection and the state bucket has `prevent_destroy`. Turn those off deliberately before running `terraform destroy`.

## Invitations

The bot is **invite-only** by default. Anyone can send messages to registered users through their links, but getting your own link or username requires an invitation.

- **Joining:** open an invitation link (`t.me/<bot>?start=inv-<code>`), or send `/register` and enter the code.
- **Levels:** people invited by an admin are level 1, their invitees level 2, and so on. Only the level number is stored, never who invited whom.
- **Inviting:** `/invite` shows how many invitations you can give and your codes, and lets you create a code for a number of people. Your budget is your level's allowance plus any personal grants, minus what you've already put into codes. It's enforced atomically, so it can't be overspent.
- **Admins** (Telegram user IDs in the `ADMIN_TELEGRAM_IDS` secret) can invite without limit and use:

  | Command | What it does |
  |---|---|
  | `/levels` | Show each level's allowance |
  | `/setlevel <level> <invitations>` | Set a level's allowance; applies right away to existing and new users at that level (unset levels: 0) |
  | `/grant <@username or link> <invitations>` | Give one user extra invitations on top of their level's allowance |

  To make yourself an admin, find your Telegram user ID (e.g. with a user-info bot) and run `gh secret set ADMIN_TELEGRAM_IDS --env production`, then redeploy.

**Opening the bot to everyone:** set the `INVITE_ONLY` variable to `false` and run the workflow on `main`. No code change is needed, and invitation commands then reply that no invitation is required.

**Removing the system entirely:** delete `bot/common/invitations/`, `bot/common/invite.go` and the hooks that call them (`start`, `getLink`, the username flow, `processText`, and the command/callback registrations), the `aws_dynamodb_table.invitations` resource with its IAM entries, and the invitation texts.

## Local development

Use docker compose to run a local DynamoDB and a local development web server on port 8080:

```shell
export BOT_TOKEN=<BOT_TOKEN>              # a separate test bot is recommended
export SQIDS_ALPHABET=<SHUFFLED_ALPHABET> # e.g. a shuffled a-zA-Z0-9
docker compose up -d
```

The local DynamoDB starts empty. Create an `IncognitoChatBot` table with the same keys and indexes as `infra/main.tf` (for example with `aws dynamodb create-table --endpoint-url http://localhost:8000 ...`).

After changing the bot code, restart the bot container:

```shell
docker compose restart bot
```

Expose port 8080 through a public HTTPS tunnel, for example [ngrok](https://ngrok.com/):

```shell
ngrok http 8080
```

Register the tunnel as the webhook. The script sets the same secret token the bot checks:

```shell
BOT_TOKEN=<BOT_TOKEN> scripts/set-webhook.sh <NGROK_FORWARDING_URL>
```

You can see the incoming update requests sent by Telegram on ngrok's web interface: `http://127.0.0.1:4040`.

### Tests

Unit tests run anywhere with `go test ./...`. The DynamoDB repository tests need DynamoDB Local; without it they are skipped (CI always runs them):

```shell
docker run --rm -d -p 8000:8000 amazon/dynamodb-local:3.3.2
cd bot
DYNAMODB_ENDPOINT=http://localhost:8000 AWS_REGION=local \
AWS_ACCESS_KEY_ID=dummy AWS_SECRET_ACCESS_KEY=dummy go test -race ./...
```

### Building the Lambda bundle manually

CI does this for you. To reproduce it locally:

```shell
cd bot
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -trimpath -ldflags="-s -w" -o bootstrap .
zip -X lambda_function.zip bootstrap
```
