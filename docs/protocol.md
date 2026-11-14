# Protocol

The formats clients and the backend agree on. Each has test vectors that both
sides check (listed at the end). The design and its reasons are in the design
notes; this file is only the exact encoding.

Suite `0x01` throughout: P-256, SHA-256, HKDF-SHA256, AES-256-GCM. Every key,
signature and envelope is tied to it, so a later suite can never be confused
with this one.

## Notation

**Labelled message**, used for everything that is signed, hashed or MACed:

```
Message(label, part1, part2, …) =
    u16(len(label)) ‖ label ‖ u32(len(part1)) ‖ part1 ‖ u32(len(part2)) ‖ part2 ‖ …
```

Big-endian lengths. A signature for one label is never accepted for another,
and bytes can't move between parts. Strings are UTF-8; numbers as parts are
either decimal strings or 8-byte big-endian integers, as stated.

**IDs** are random bytes in unpadded base64url: accounts, devices,
conversations, messages and mailboxes 16 bytes (22 characters), links 8 bytes
(11 characters). Only the canonical encoding is accepted.

**Times** in signed structures are Unix seconds on a whole hour, so they don't
record exactly when anything happened.

## Keys

| Key | Algorithm | Notes |
|---|---|---|
| Account key | ECDSA P-256 | Copyable (sealed in the vault), never shown to other users |
| Device signing key | ECDSA P-256 | Non-extractable, one device |
| Link signing key | ECDSA P-256 | One per link, copyable |
| Link encryption key | ECDH P-256 | Signed by the link signing key, rotated |
| Conversation key | ECDH P-256 | One per conversation per side, rotated |

Public keys travel as 65-byte uncompressed points. Signatures are IEEE P1363
(`r ‖ s`, 64 bytes) over SHA-256 of the labelled message, as WebCrypto makes
them.

**Fingerprint** of a public key `P`: base64url of
`SHA-256(Message("incognitochat/v2/fingerprint", [0x01], P))`, 43 characters.

**Key ID** of a recipient key `P`: the first 8 bytes of
`SHA-256(Message("incognitochat/v2/key-id", [0x01], P))`.

## Registration and sessions

| Label | Signed by | Parts |
|---|---|---|
| `incognitochat/v2/register` | account key and device key | SHA-256(initData), account key, device ID, device key |
| `incognitochat/v2/device-list` | account key | see below |
| `incognitochat/v2/session` | device key | account ID, device ID, timestamp (decimal), nonce |
| `incognitochat/v2/request` | device key | account ID, device ID, method, path, timestamp (decimal), nonce, SHA-256(body) |

A registration launch has `start_param = "reg" ‖ fingerprint(account key)`.
Signed requests carry `X-Device-Signature: <timestamp>.<nonce>.<signature>`
next to the session token. Nonces are 16–64 base64url characters and are used
once; timestamps must be within 60 seconds of the server's clock.

**One device per account.** Setting up on a second device replaces the
account (after a typed confirmation); there is no device linking or sync.
Replacing deletes the old account's links and mailboxes on the server, and a
new setup on a device first deletes everything the previous account left
there (keys, conversations, mailboxes, links). A
device without an account may ask `POST /accounts/lookup {initData}` whether
its Telegram user has one, to say "your account is on another device" instead
of offering setup. Like notices, it accepts only a plain or registration
launch, never one that names a link or an invitation, and stores nothing.

**Deleting an account:** `DELETE /account`, signed. The server deletes the
account's waiting envelopes, links (their bundles then answer "not found"),
mailboxes (sends to them get the usual answer and are dropped), invitation
codes, device list, devices and the account itself, and the item that keeps
one account per Telegram user, so the same user can set up again. Unlike a
replacement, it keeps no marker; only rate-limit counters and socket records
remain until they expire (within a day). Before asking, the app deletes the uploads
still waiting for it (`POST /media/{id}/delete`); afterwards it deletes
everything on the device. The other side of each conversation isn't told.

**Device list** (`incognitochat/v2/device-list`), parts in order: suite
(1 byte), sequence number (8), SHA-256 of the previous list (32; zeros for the
first), issued (8), expires (8, at most 30 days later), then per device: ID
(16 raw bytes), signing key (65), added (8).

## Links

```
https://t.me/<bot>/<app>?startapp=<payload>
payload = "1" ‖ link ID (11) ‖ fingerprint(link signing key) (43)       55 characters
```

The bundle (`GET /links/{id}`) holds the link signing key and 1–4 encryption
keys, each `{key, from, until, signature}` with the signature over
`Message("incognitochat/v2/link-key", [0x01], key, u64(from), u64(until))`.
A client trusts a bundle only if the signing key's fingerprint is the one in the
link and every key's signature checks; it encrypts to the newest key valid now.

An account has one link at a time (`POST /links`). Replacing it
(`POST /links` with `replaces: <old ID>`) swaps the old link and its intro
mailbox for the new one in one step: the old link stops working at once.
The new link has a new ID and new encryption keys but the **same signing
key**, so its fingerprint stays the same. Sender tags are made from that
fingerprint, so the same people keep the same tags across replacements, which
keeps blocks and labels. Conversations carry on, since after the first message
they don't go through the link; a sender the owner hasn't answered yet keeps
writing to the old intro mailbox until the owner replies.

Invitation links use `payload = "inv" ‖ code`, the code being 8 characters from
`abcdefghjkmnpqrstuvwxyz23456789`, shown as `abcd-efgh`.

## Envelope

What the server stores and delivers, opaque to it:

```
envelope = suite (1) ‖ wrap count n (1, 1–16) ‖ wrap₁ … wrapₙ ‖ body
wrap     = key ID (8) ‖ enc (65) ‖ wrapped content key (48)
body     = nonce (12) ‖ AES-256-GCM(content key, nonce, aad, padded inner)
aad      = Message("incognitochat/v2/body", [0x01])
```

The content key is 32 random bytes per envelope. Each wrap is single-shot HPKE
Base mode (RFC 9180) — DHKEM(P-256, HKDF-SHA256), HKDF-SHA256, AES-256-GCM —
sealing the content key to one recipient key, with empty AAD and

```
info = Message("incognitochat/v2/wrap", [0x01], key ID, SHA-256(body))
```

so a wrap can't be moved onto another body. A recipient tries the wraps whose
key ID it knows.

**Padding.** The inner message is padded to 512, 2048, 8192 or 16384 bytes:
`u32(length) ‖ inner ‖ zeros`. Anything else is rejected, including non-zero
padding. An envelope's size therefore only reveals its wrap count and size
class.

## Inner message

```
inner = tag (32) ‖ JSON (UTF-8)
tag   = HMAC-SHA256(K, JSON)
K     = HKDF-SHA256(ikm = ECDH(sender conversation key, recipient key),
                    salt = empty,
                    info = Message("incognitochat/v2/message-auth", [0x01],
                                   sender public key, recipient public key))
```

The recipient computes the same `K` with its private key and the sender's
public key from `from`, so it can check the tag, and could have made it:
messages are deniable. The server holds neither private key.

JSON fields:

| Field | Meaning |
|---|---|
| `v` | 1 |
| `kind` | `message`, `seen`, `reaction`, `delete`, `key_update` or `end` |
| `conv` | conversation ID (16 bytes) |
| `id` | message ID (16 bytes) |
| `at` | sent time, Unix milliseconds, sender's clock |
| `from` | sender's current conversation public key (65 bytes); a new one is a rotation |
| `to` | key ID of the recipient key it was written for; must be the key that opened it |
| `reply` | mailbox to write back to (16 bytes), first messages or when it changes |
| `tag` | sender tag (32 bytes), first message through a link: `HMAC-SHA256(account secret, Message("incognitochat/v2/sender-tag", UTF-8 of the link fingerprint))`, the same for one person on every link with that fingerprint |
| `last` | ID of the last message received from the other side |
| `text` | the text (`message` only; at most 12,000 UTF-8 bytes) |
| `replyTo` | ID of the message this answers |
| `seen` | IDs read (`seen` only, 1–100) |
| `media` | an attachment (`message` only, with or without text): `{t, id, k, d, s, n?, m?, w?, h?, l?, v?, p?}`, see Media |
| `sticker` | a sticker (`message` only, with no text or attachment): `{id, e?}`, see Stickers |
| `reaction` | a reaction (`reaction` only): `{to, e}`, where `to` is the ID of a message in the conversation, from either side, and `e` one emoji (one grapheme with an emoji code point, at most 32 UTF-16 units) or empty to take the reaction back |
| `delete` | IDs of the sender's own messages to delete on the recipient's device too (`delete` only, 1–100) |
| `all` | `true`: the whole conversation instead, both sides' messages (`delete` only, instead of `delete`) |

Unknown fields are ignored but covered by the tag. `message` needs non-blank
text, an attachment or a sticker; `seen` needs `seen` and no text;
`reaction` needs `reaction` and `delete` needs `delete` or `all` (one of
them), and nothing else;
`key_update` and `end` carry none of these.

**Deleting for both sides.** A sender can delete their own messages on the
recipient's device too, whether or not they were opened: the app sends a
`delete`, deletes the uploads of any attachments (it holds their delete
secrets), then deletes its own copies. The recipient deletes only messages
from that side in that conversation; a `delete` naming one of the
recipient's own messages changes nothing. A `delete` that arrives before the
message it names (delivery has no set order) is remembered for 31 days, the
longest a message can wait, and the message is dropped when it comes. A
conversation started by anything but a `message` stays out of the list
until one arrives. With `all`, the recipient deletes the whole
conversation, both sides' messages, as in Telegram; it leaves the list until
either side writes again, and messages from that side sent before the
`delete` (by its `at`) that arrive later are dropped. Like blocks, this
relies on the recipient's app doing it.

**Reactions.** Each side has at most one reaction per message, as on Telegram
without Premium; a new one replaces it. The recipient keeps the one with the
latest `at` from that side, so a replayed or late older one changes nothing.
A reaction to a message the recipient doesn't have (deleted, or in another
conversation) is dropped. Versions without reactions drop them as an unknown
kind.

**Who writes with which key.** A first message through a link comes from a
fresh conversation key of the sender and is written for the link's current
encryption key. The owner's first reply comes from a fresh conversation key of
the owner and carries its reply mailbox; from then on each side writes for the
other's latest conversation key. Both sides rotate their own conversation key
about weekly, each conversation on its own schedule.

## Key rotation

Encryption keys are replaced about weekly and old private keys deleted, so a
key stolen later can't open envelopes someone kept from long before.

- **Our conversation key:** every 6 to 9 days, on each conversation's own
  random schedule, the next message or receipt in it comes from a new key
  (its `from`); nothing is sent just to rotate. Not before the other side has
  written to us, since until then they know us by our first key alone.
- **A link's encryption key:** every 6 to 9 days, when the app opens, the
  owner publishes a new key (`PUT /links/{id}/keys`); the bundle then lists
  only the new key. A sender the owner hasn't answered yet fetches the bundle
  again before writing if the key it has is over an hour old.
- **Deleting old keys:** an old conversation key is deleted 35 days after the
  other side first writes to the new one; an old link key 35 days after it is
  replaced (or after the link is). Undelivered envelopes expire after 30 days,
  so nothing that can still arrive is written for a deleted key. If the other
  side never writes to the new key, the old one stays.
- Blocking keeps a conversation's keys, so it can be undone.

Rotation is invisible to users: labels come from sender tags, not keys, and
the history kept on the device is sealed under the device's storage key.

## Delivery

The server routes by **mailbox** (16 random bytes), never by account. It knows
each mailbox's owner, to deliver, but not who writes to it.

- Each link has an intro mailbox, listed in its bundle (`mailbox`); first
  messages go there. Clients keep a small pool of further mailboxes
  (`POST /mailboxes`) and take one when a conversation needs it, so creating
  one never happens right before a send.
- `POST /send {mailbox, envelope, token, quiet?}` needs no login, only a send
  token. The server checks the envelope's layout and the caps, then stores the
  envelope once for each of the owner's devices. Unknown and discarded
  mailboxes get the same `200 {}`, after a similar pause, and nothing is
  stored. `quiet` sends (seen receipts, reactions, deletes and key updates)
  trigger no notice.
- `GET /inbox` returns a device's undelivered envelopes in no meaningful order;
  `POST /ack {ids}` deletes the ones it has stored. Undelivered envelopes expire
  after 30 days.
- **Push:** an open app keeps a WebSocket (`new WebSocket(url, ["v1", token])`,
  the session token as the second subprotocol, never in a URL). After a
  delivery the server sends the owner's open sockets `{"t":"sync"}` and nothing
  else; the app then fetches its inbox. The Telegram notice goes out only if
  no socket took the push. Receipts are pushed but never noticed. The app
  sends a heartbeat about every 5 minutes and keeps the socket only while
  it's visible. The server keeps a socket's account and connection ID until
  it closes (at most 2 h 10 min).
- `POST /mailboxes/{id}/discard` ends a mailbox (a block, or an ended
  conversation); a link's intro mailbox ends with the link.
- `POST /mailboxes/check {ids}` returns which of the given mailboxes aren't
  the caller's (any more). At upkeep the app drops those from its pool and
  gives conversations that used one as their reply address a new one, which
  their next message carries. Nothing is stored.
- **Blocking** discards our mailbox for the conversation and, for someone who
  wrote to our link, remembers their sender tag (by link fingerprint), so new
  conversations from them are dropped too. Silent: their sends still look
  accepted. **Unblocking** forgets the tag and, if we had a mailbox, sends
  them a quiet `key_update` from a new mailbox (and a new key if ours is
  gone), so their app writes there from then on. What they sent meanwhile is
  lost. Only `message` sends trigger a Telegram notice.

## Media

Attachments travel outside the envelope, encrypted, through an S3 bucket.

- The app encrypts the file with a fresh key, pads it, and asks
  `POST /media {size, deleteHash, tokens}` for an upload URL. No login: it
  pays with send tokens, one per started 5 MiB (at most 26 MiB). `deleteHash`
  is the SHA-256 of a random delete secret. The answer is `{id, url}`, and the
  app PUTs exactly `size` bytes of ciphertext, typed
  `application/octet-stream`, to `url` (valid 15 minutes; the length and type
  are part of its signature).
- The message carries the attachment in `media`, inside the encrypted
  envelope: kind `t` (`photo`, `video`, `gif`, `voice`, `audio` (music),
  `round` (a video message) or `file`), upload ID `id` (16 bytes),
  AES-256-GCM key `k` (32 bytes), delete secret `d` (16 bytes), the file's
  size `s` (at most 25 MiB), and optionally its name `n`, MIME type `m`,
  pixel size `w`/`h`, length in seconds `l`, a voice message's waveform `v`
  (base64url of 1–100 bytes, one bar each, 0–255) and a tiny JPEG preview `p`
  (base64url, at most 3000 characters). The server never sees a key, name or type. A kind the
  recipient's version doesn't know (lowercase letters and `_`, at most 16)
  is accepted and shown as a file.
- What each kind is: a photo is redrawn on the sender's device (at most
  1280 px, JPEG), which drops its metadata. A video is sent as recorded (no
  compression), with its location, device and time metadata removed: the
  MP4/QuickTime `udta` and `meta` boxes are renamed `free` and the header
  times zeroed, so nothing else in the file moves. A GIF is sent as it is.
  A voice message is recorded in the app (AAC in MP4 where the browser can,
  else Opus in WebM or Ogg; at most 15 minutes), with a waveform taken from
  the microphone level while recording (or from the recording, where the
  level couldn't be read); it carries no device or place metadata. A video
  message is recorded in the app too: the camera's picture cropped to a
  384 × 384 square (mirrored for the front camera, as the sender saw it),
  with sound, as H.264 in MP4 where the browser can, else WebM; at most a
  minute, with a preview frame and no device or place metadata. Music is the
  audio file as it is, with its length. A file is the original bytes,
  untouched (location included, if it has any). Photos, voice and video
  messages, and GIFs up to 10 MiB are fetched when their conversation opens;
  videos, music and files on a tap.
- The upload is `nonce (12) ‖ AES-GCM(k, nonce, file ‖ zeros, aad =
  "incognitochat/v2/media")`, padded to the next power of two from 16 KiB up
  to 1 MiB, then to the next whole MiB.
- The recipient gets a download URL with `GET /media/{id}` (no login; the ID
  is the permission; at most 10 a day), fetches and decrypts the file and
  keeps it on the device. The upload stays, downloaded or saved, until the
  message or conversation is deleted on the recipient's device (then it's
  deleted with `POST /media/{id}/delete {secret}`) or it expires.
- **Saving outside Telegram.** A Mini App can't hand a file to the device
  (Telegram's download needs a public HTTPS URL, which would mean a decrypted
  copy on a server). Where the app has a share sheet, it uses that.
  Otherwise it opens `<site>/#save=<base64url(JSON {id, k, s, n?, m?})>`
  in the device's own browser: the part after `#` is never sent to any
  server, and the page removes it from the address at once. That page
  fetches and decrypts the upload and offers it as a normal download. It
  doesn't delete the upload, and the link carries no delete secret. The
  link may stay in the browser's history; once the upload is gone it opens
  nothing.
- Uploads expire after 31 days. The bucket keeps no
  versions and no access logs.

## Stickers

Stickers come from the app's own packs, built into the app (web/public/stickers:
Microsoft's Fluent Emoji, MIT licensed). Sending one uploads nothing: the
message names it, `sticker: {id, e?}`, where `id` is `pack/name` (lowercase
letters, digits and `_`; pack at most 32 characters, name at most 40) and `e`
the emoji it stands for (at most 16 UTF-16 units). A sticker the recipient's
version doesn't have, from a newer pack, shows as that emoji. There are no
Telegram sticker packs: a Mini App can't reach them, and fetching them
through the bot would tell the server who uses which.

The images are files of the app's build (`stickers/<pack>/<name>.webp`).
The app fetches all of them at once when a conversation opens, not when a
sticker is shown or sent, so the host serving the app learns nothing about
stickers from it.

## Notices

The bot can tell someone that a message is waiting: a generic "New message"
with a button that opens the app, never who wrote or what. Only the `notify`
function holds the bot token.

- The app turns notices on with `POST /notices {initData}`, sending its
  Telegram launch data. The server accepts only a plain launch or a
  registration launch, never one whose `start_param` names a link or an
  invitation, and only for the account's own Telegram user. It keeps the chat
  ID sealed (AES-256-GCM under a key only `notify` has) and the notice
  language, nothing else. The bot needs permission to write first; the app
  asks Telegram for it. `GET /account` says whether notices are on, and the
  app sets them up again when they're off.
- After each delivery that isn't `quiet`, `api` asks `notify` to send the
  owner a notice, without waiting. `notify` sends at most one every 3 minutes
  per account, and deletes the previous one, so the chat holds one notice at
  most. If Telegram refuses (the bot is blocked), notices turn off.

What this tells the server: that an account has a Telegram chat, which it
already knows from registration, and when the account gets messages, which
delivery already tells it. Quiet sends tell it which deliveries are seen
receipts, though not whose. Telegram learns when someone gets messages, in
3-minute steps.

## Send tokens

Every `POST /send` spends one anonymous token, so the server can limit sends
without learning who sends. Tokens are RSA blind signatures (RFC 9474,
RSABSSA-SHA384-PSS-Deterministic) in the shape of Privacy Pass token type 2
(RFC 9578):

```
token     = u16(0x0002) ‖ nonce (32) ‖ challenge (32) ‖ key ID (32) ‖ authenticator (256)
challenge = SHA-256(Message("incognitochat/v2/send-token", [0x01], u64(epoch)))
key ID    = SHA-256(issuer public key as SubjectPublicKeyInfo, DER)
epoch     = floor(unix seconds / 604800)
```

The authenticator is an RSA-PSS signature (SHA-384, MGF1-SHA-384, 48-byte
salt) over the first 98 bytes, by a 2048-bit issuer key. A logged-in device
blinds token inputs, gets them signed (`POST /tokens`, at most 50 a request and
200 a day), and unblinds them; the issuer never sees the tokens it signs.

A token is accepted in its own week and the next, and only once: the server
remembers spent nonces until then. A send refused for a cap doesn't spend its
token. Clients only accept the issuer key their build names
(`VITE_ISSUER_KEY`), one of those published in `web/issuer-keys.json`, so the
server can't hand different users different keys to tell them apart.

## Test vectors

| What | Where |
|---|---|
| Labelled messages, P1363 signatures | `backend/internal/devicesig`, `web/src/crypto/crypto.test.ts` |
| Fingerprint | `backend/internal/devicesig`, `web/src/crypto/crypto.test.ts` |
| Device list | `backend/internal/devicelist`, `web/src/protocol/device-list.test.ts` |
| Register, session and request messages | `backend/internal/api/vectors_test.go`, `web/src/account/account.test.ts` |
| Link keys | `backend/internal/links`, `web/src/protocol/link.test.ts` |
| HPKE (RFC 9180 Appendix A, AES-128-GCM and AES-256-GCM) | `web/src/crypto/hpke-vectors.json` |
| Envelope layout (sealed by the client) | `backend/internal/envelope/testdata` |
| Blind RSA (RFC 9474 Appendix A, PSS-Deterministic) | `web/src/crypto/blindrsa-vectors.json`, `backend/internal/tokens/testdata` |
| Send-token challenge | `backend/internal/tokens`, `web/src/protocol/token.test.ts` |
