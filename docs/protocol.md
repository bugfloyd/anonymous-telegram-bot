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
| `kind` | `message`, `seen`, `key_update` or `end` |
| `conv` | conversation ID (16 bytes) |
| `id` | message ID (16 bytes) |
| `at` | sent time, Unix milliseconds, sender's clock |
| `from` | sender's current conversation public key (65 bytes); a new one is a rotation |
| `to` | key ID of the recipient key it was written for; must be the key that opened it |
| `reply` | mailbox to write back to (16 bytes), first messages or when it changes |
| `tag` | sender tag (32 bytes), first message through a link |
| `last` | ID of the last message received from the other side |
| `text` | the text (`message` only; at most 12,000 UTF-8 bytes) |
| `replyTo` | ID of the message this answers |
| `seen` | IDs read (`seen` only, 1–100) |

Unknown fields are ignored but covered by the tag. `message` needs non-blank
text; `seen` needs `seen` and no text; `key_update` and `end` carry neither.

**Who writes with which key.** A first message through a link comes from a
fresh conversation key of the sender and is written for the link's current
encryption key. The owner's first reply comes from a fresh conversation key of
the owner and carries its reply mailbox; from then on each side writes for the
other's latest conversation key. Both sides rotate their own conversation key
about weekly, each conversation on its own schedule.

## Test vectors

| What | Where |
|---|---|
| Labelled messages, P1363 signatures | `backend/internal/devicesig`, `web/src/crypto/crypto.test.ts` |
| Fingerprint | `backend/internal/devicesig`, `web/src/crypto/crypto.test.ts` |
| Device list | `backend/internal/devicelist`, `web/src/protocol/device-list.test.ts` |
| Register, session and request messages | `backend/internal/api/vectors_test.go`, `web/src/account/account.test.ts` |
| Link keys | `backend/internal/links`, `web/src/protocol/link.test.ts` |
| HPKE (RFC 9180 Appendix A, AES-128-GCM and AES-256-GCM) | `web/src/crypto/hpke-vectors.json` |
