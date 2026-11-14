# Security model

What Incognito Chat protects, from whom, and where it stops. How it works is
in [architecture.md](architecture.md); the exact formats are in
[protocol.md](protocol.md).

## Why it exists in this form

Version 1 relayed messages inside Telegram chats with the bot. Anyone holding
the bot's token can read those chats back through Telegram's own APIs,
including Bot API forwarding and MTProto's `messages.getMessages`, which walks
every message still stored in every chat with the bot. The bot's owner can
always see the token in BotFather. No server hardening fixes that, so
version 2 moves all content out of Telegram and encrypts it on the device.

## Goals

- **Content is end-to-end encrypted.** Only the two people in a conversation
  can read it: not Telegram, not the bot-token holder, not the operator, not
  AWS, not GitHub, not anyone with a copy of the database.
- **Stored data shows no social graph.** The server never records who sent a
  message or which conversations belong together.
- **Anonymous senders stay anonymous**, also to recipients who compare notes.
  Someone's anonymous conversation with you can't be linked to the
  conversation they have with you through their own link.
- **The app's code is verifiable**: built in public, reproducibly, with
  published hashes and provenance, and served the same to everyone
  ([verifying.md](verifying.md)).
- **Low cost, nothing running when idle.**

Not goals of this version: hiding IP addresses from AWS and GitHub, resisting
an operator who watches live traffic, resisting a malicious Telegram app,
several devices per account, and recovering an account after losing its
device.

## Who could attack, and what they get

| Who | Can | Gets |
|---|---|---|
| A copy of the database (leak, backup, data request) | Read every stored item | Accounts, public keys, links, mailboxes and their owners, undelivered ciphertext, encrypted attachments. No content, no senders, no conversation pairs. |
| The operator or AWS account holder, passively | Read the database, logs and settings | The same. |
| The operator, actively and live | Change server code, log requests, watch traffic | The recipient of each message, timing, IP addresses. Can link people by timing over several messages. Can't read content or forge messages, as long as the app itself isn't swapped. |
| The bot-token holder | Everything Telegram's bot APIs allow | Who uses the bot, when notices go out. No content. Can send look-alike notices that open a look-alike page, which has none of the user's keys. |
| The BotFather owner | Change the Mini App's address | Can point the app at a malicious page, for everyone or for chosen people. The largest remaining risk; see below. |
| Telegram's servers | See app launches and bot traffic | Who opens which link, when notices go out. Timing can link senders to recipients. No content. |
| The Telegram app on the device | Hosts the app's page | Everything on that device. Trusted, like any app you type into. |
| GitHub (hosts the app) | Serve the app's files, log visits | IP addresses, and the link being opened (it's in the page address). Could serve different code, but verifiers would see it. |
| Other users | Receive messages, compare notes | Can't link an anonymous sender's conversations with different people, or their anonymous and known conversations. |
| Spammers | Create Telegram accounts | Bounded by Telegram-only registration, send-token budgets and per-mailbox and per-link caps. |
| A thief with an unlocked device | Use the device | Everything on it. Remedy: set up again, which ends the old account at once. |

## What users still have to trust

The goal is an app where nobody has to trust anyone, including whoever runs
it. This is how close it gets.

**What the operator can't do at all**

- Read anyone's messages or attachments.
- Learn from stored data who wrote to whom, or who an anonymous sender is.
- Forge messages or slip them into a conversation.
- Swap someone's keys unnoticed: every link carries its key's fingerprint, and
  the app checks it.

**What the operator could do, but would be caught**

- Serve app code that differs from this repository. Builds are public,
  reproducible, attested and hashed, so anyone can compare what is served with
  what is in the repository ([verifying.md](verifying.md)).

**What still requires trusting the operator**

1. **A swapped page inside Telegram.** Whoever controls the bot in BotFather
   decides which web page Telegram opens, for everyone or, through the bot,
   for chosen people, and Telegram offers no way to lock it. A swapped page
   has no access to the keys already on a user's device, so past messages
   stay safe. But anyone who types into it, for example a sender opening a
   link, is typing into the operator's page. A user it talks into "setting up
   again" loses their account. What limits the damage:
   - keys are tied to the real app's address, so another page can't use them;
   - setting up again replaces the account loudly: the old device says at once
     that the account was replaced, and that this may mean tampering;
   - the person's link changes, which their contacts see.
2. **Watching live traffic.** The operator, or AWS, can watch requests as they
   happen and link people by timing and IP address: a message arriving, then
   a notice, then a reply or a "seen". Nothing stored shows this, but live
   observation does. Immediate "seen" receipts make it easier.

**What every user trusts in any app**

- Their own phone or computer and its operating system.
- The Telegram app, which hosts the page and could read it.
- The app code they are actually running.

## What still leaks

- **Timing.** The operator, AWS or Telegram, watching live, can correlate "a
  message arrived" with "a notice went out" or "someone replied", and link
  people after a few rounds. "Seen" receipts are sent the moment a message is
  read, which sharpens this. Not stored, but observable.
- **IP addresses.** AWS sees the IP address of every request, including
  attachment uploads and downloads. GitHub sees the IP address of every app
  load, together with the link being opened (Telegram puts it in the page
  address).
- **The user's own network.** The app's traffic doesn't travel through
  Telegram: it goes straight to the app's host and the backend. Whoever
  carries it (an internet provider, a Wi-Fi owner, a VPN) sees that the
  device uses this service, and when. They see no content, links or accounts.
- **Telegram** sees who opens which link, and when notices are sent.
- **Recipients are known to the server.** It must know a mailbox's owner to
  deliver to it. Only senders and pairings are hidden.
- **Which messages are quiet.** "Seen" receipts, reactions, edits, deletes
  and key updates are marked so they don't trigger a notice. The server sees the mark, not
  which of these it is or who sent it.
- **Attachment sizes, roughly.** Files are padded before upload (to the next
  power of two up to 1 MiB, then the next whole MiB), and paid with one send
  token per started 5 MiB. So the server sees a size class, not the type, name
  or content.
- **Attachments wait on the server, encrypted.** An upload stays in the bucket
  until the recipient deletes the message or the conversation, or 31 days
  pass, whether or not it was downloaded or saved, so the app can fetch it
  again meanwhile. It's ciphertext the server can't open, but it exists. The
  link used to save a file in the device's browser can stay in that
  browser's history; it carries no delete secret, and once the upload is
  gone it opens nothing.
- **Who invited whom** can be guessed from when an invitation code is used.
- **Activity and volume:** roughly how many mailboxes and messages an account
  has.
- **A brand-new user's first message** follows their registration closely in
  time.
- **Files keep their metadata.** Photos are redrawn and videos have their
  location and time data removed, but a file sent "as a file" is sent
  untouched, location included if it has any.
- **Abuse can't be traced to a person**, by design. Blocks, budgets, caps and
  replacing a link are the remedies.

## Other limits

- **One device per account.** History lives only on that device. Setting up
  on another device replaces the account; there is no sync and no recovery.
  If Telegram or the phone clears the app's storage, the account is gone.
  Whether iPhones keep the app's storage over long periods is still being
  tested.
- **Edits rely on honest apps too.** An edit asks the other side's app to
  replace a message's text and mark it edited; a modified app could keep the
  original, as anyone can keep a screenshot. Only the sender's own messages
  can be edited.
- **Deleting for both sides relies on honest apps.** Deleting a message, or
  a whole conversation, for both sides asks the other side's app to delete
  it, and deletes the deleter's uploads from the server. A modified app could
  keep it, and anyone can take a screenshot, as with Telegram's. A single
  message can only be one of the deleter's own; a whole conversation, as in
  Telegram, takes both sides' messages.
- **Blocks rely on honest apps.** A block also covers new conversations from
  the same person through the same link, using a tag the sender's app adds. A
  modified app could send a random tag and appear as someone new. Per-link
  caps, budgets and replacing the link bound this.
- **A blocked sender may still trigger a notice**: the server can't tell a
  blocked first message from any other, so the owner may be notified of a
  message their app then drops.
- **Forward secrecy is weekly, not per message.** Encryption keys rotate about
  weekly and old private keys are deleted after 35 days. A stolen device key
  exposes at most about five weeks of messages a malicious server might have
  kept.
- **Users can't verify the backend directly.** Its builds are reproducible
  and attested, Lambda runs only code CI signed, and each deploy's public log
  shows that the running code is the attested build
  ([verifying.md](verifying.md#the-backend)). But users can't look into AWS,
  and whoever controls the AWS account could switch signing off. The design
  doesn't depend on it: a malicious backend can drop or delay messages and
  watch timing, but can't read content or forge messages, because apps check
  keys themselves.

## How the remaining gaps could close

- Native apps with reproducible builds, whose code is identical for everyone
  and can be checked, with keys in the phone's security chip.
- An independent relay that hides IP addresses, random notice delays, and a
  setting to turn "seen" receipts off.
- Change alerts from AWS's audit logs, ideally held by an independent party.
- The backend in an attested enclave (such as AWS Nitro Enclaves), so the app
  can check that the server runs this repository's code before talking to
  it. The operator could then no longer learn which Telegram user owns which
  account or mailbox, or match a sender's IP address to an account. Timing
  and IP addresses would still leak, and users would trust AWS's attestation.
- Unforgeable sender tags (anonymous credentials), so blocks hold against
  modified apps.
- A per-message ratchet (Double Ratchet or MLS) and post-quantum key exchange.
- Moving an account to a new phone, and optional multi-device.
