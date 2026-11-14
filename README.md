# Incognito Chat

Anonymous messaging through personal links, end-to-end encrypted, inside
Telegram.

You share your link; anyone with it can write to you without you knowing who
they are, and you can reply. Everything happens in a Telegram Mini App that
encrypts on the device before anything leaves it. The server only holds
encrypted messages until they're delivered, and never records who wrote to
whom.

## What it does

- **Personal links.** One link per account, replaceable at any time. While the
  service is invite-only, owning a link needs an invitation; anyone with
  Telegram can write to one.
- **Conversations like Telegram's:** text, replies, photos, videos, GIFs,
  files, music, voice messages, round video messages, emoji and stickers, with
  "sent" and "seen" ticks.
- **Anonymous both ways:** a sender is a label like `🦊 K3F9`, the same each
  time they write through your link, and unlinkable to anything else they do.
- **Silent blocks:** the blocked person isn't told; their messages just stop
  arriving.
- **Notices** from the bot when a message arrives: a generic "New message",
  never who or what.
- **English and Persian**, right-to-left included, in Telegram's own theme.

## How it works, briefly

- **The app** (TypeScript, Preact) runs as a Telegram Mini App and does all
  the encryption with the browser's WebCrypto. It's served from GitHub Pages,
  built reproducibly and attested, so anyone can check that what's served is
  this repository's code.
- **The backend** is four Go functions on AWS Lambda (API, bot, sockets,
  notices), with DynamoDB and an S3 bucket for encrypted attachments. Nothing
  runs when idle.
- **Messages** are encrypted to the other side's current key (HPKE on P-256,
  AES-256-GCM) and routed by random **mailbox** IDs, never by account. The
  server knows who a mailbox belongs to, not who writes to it.
- **Sending needs no login:** each send spends an anonymous, blind-signed
  token, so the server can rate-limit without learning who sends.
- **Keys rotate** about weekly; history and keys live only on the user's one
  device.

More: [how it works](docs/architecture.md), and the
[protocol](docs/protocol.md) in full.

## Security at a glance

**The operator can't** read messages or attachments, learn from stored data
who wrote to whom or who an anonymous sender is, forge messages, or swap keys
unnoticed (links carry their key's fingerprint).

**The operator could, but would be caught,** serving app code that differs
from this repository: builds are reproducible, hashed and attested, and
[anyone can check](docs/verifying.md) the live site.

**Users still have to trust the operator** on two points:

1. **A swapped page inside Telegram.** Whoever controls the bot decides which
   page Telegram opens. A swapped page can't reach the keys on your device, so
   past messages stay safe, but what you type into it isn't. Setting up again
   on such a page would end your account loudly, on your old device.
2. **Live traffic watching.** The operator or AWS could link people by the
   timing and IP addresses of requests as they happen. Nothing stored shows
   this.

And, as with any app, users trust their own device and the Telegram app.

What still leaks (timing, IP addresses, which links Telegram sees opened,
rough attachment sizes) and the other limits are listed in full in the
[security model](docs/security.md). What is kept, where and for how long is in
[what the software stores](docs/data.md).

## Documentation

| Document | What |
|---|---|
| [Security model](docs/security.md) | Goals, threats, what users still have to trust, what still leaks, limits |
| [What the software stores](docs/data.md) | What is kept, where, for how long, and who sees what |
| [How it works](docs/architecture.md) | The parts, accounts, links, conversations, delivery, attachments, stickers |
| [Protocol](docs/protocol.md) | Exact formats: keys, links, envelopes, messages, rotation, delivery, media, tokens |
| [Verifying the app](docs/verifying.md) | Reproducible builds, attestations, and checking the live site |
| [Development](docs/development.md) | Code layout, running every check locally, dependency rules |
| [Deployment](docs/deployment.md) | Environments, bootstrap, secrets, the bot and the app's hosting |
| [Hosting](hosting/README.md) | The hosting repository that builds, attests and serves the app |

## Repository

| Path | What |
|---|---|
| [`web/`](web) | The app: TypeScript, Vite, Preact. |
| [`backend/`](backend) | Go: the `api`, `bot`, `ws` and `notify` Lambda functions. |
| [`infra/`](infra) | Terraform for one environment; [`infra/init/`](infra/init) bootstraps an AWS account. |
| [`hosting/`](hosting) | The app's build-and-deploy workflow, run by the hosting repository. |
| [`scripts/`](scripts) | Bootstrap, reproducible Lambda builds, deploy and live-site verification. |
| [`docs/`](docs) | The documentation above. |

Every check CI runs can be run locally; see [Development](docs/development.md).
The same commit always builds byte-identical files, and CI fails if it
doesn't.

Version 1, an in-chat Telegram bot that this version replaces, is kept at tag
[`v1`](../../tree/v1).
