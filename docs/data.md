# What the software stores

What Incognito Chat keeps, where, and for how long, as this code does it. How
the protections work is in [security.md](security.md) and
[architecture.md](architecture.md).

This isn't a privacy policy. Whoever runs a deployment publishes its policy,
with who runs it, how to reach them, where the data is hosted, and anything
they add, such as payments; this page is the technical part it can point to.
A change to what the code stores updates this page in the same change.

## In short

- Messages and attachments are encrypted on the device. Nobody else can read
  them: not whoever runs the server, not Telegram, not the hosting providers.
- The server doesn't store who writes to whom, or who an anonymous sender is.
- Chats exist only on the user's device. The server can't restore them or hand
  them to anyone, because it doesn't have them.
- The server does see some things it can't avoid seeing, such as IP addresses
  and when messages arrive. They're listed below.

## On the device

Keys, conversations, their history, attachments received or sent, labels, and
blocks. History and attachments are encrypted under a key that can't leave the
device. None of it leaves the device in readable form. It's deleted when the
user deletes a conversation, deletes the account or sets up again, or when
Telegram or the phone clears the app's storage.

## On the server

| What | Why | How long |
|---|---|---|
| A random account ID, and a keyed hash of the user's Telegram ID (not the ID itself) | One account per Telegram account | While the account exists |
| The account's and its device's public keys | To check logins and requests | While the account exists |
| The user's Telegram chat ID, encrypted so only the notice service can open it, and their language | To send "New message" notices, if the user allows them | While notices are on |
| The account's link: its ID, its public keys, its first-contact mailbox | So people can write to the user | Until it's replaced, or the account ends |
| Mailboxes: random IDs, and which account they belong to | To deliver messages | Until they're ended or expire |
| Messages waiting for the device, encrypted | Delivery | Until the device confirms them, at most 30 days |
| Attachments waiting to be fetched or saved, encrypted, with their padded size | Delivery | Until saved, deleted, or at most 31 days |
| Invitation codes the account created, and its invitation level | Invite-only access to links | While the account exists |
| Short-lived counters, login nonces, spent send tokens, open-socket records | Rate limits, replay protection, live updates | Minutes, up to two weeks |
| Error logs, without IDs, IP addresses or content | Fixing problems | 7 days |

When a user deletes their account, all of the above goes at once. Only
short-lived rate-limit counters and open-socket records are left, and they
expire on their own within a day. Setting up again instead replaces the old
account: a marker and the old device's public key are kept for 90 days, so
the old device can say it was replaced; everything else of the old account
goes at once.

**Never stored:** the content of any message or attachment in readable form,
who sent a message, which conversations belong together, the user's name,
username or phone number, and IP addresses. Senders don't log in to send: they
pay with anonymous tokens that can't be linked to their account.

## What Telegram gives the app

When a user sets up, Telegram gives the app signed launch data about them
(their Telegram ID, name, username and language). The server checks Telegram's
signature, keeps the keyed hash of the ID described above, and forgets the
rest. When the user allows notices, it also keeps their chat ID, encrypted,
and their language. When someone opens a link, the app never sends that launch
data to the server, so it doesn't learn who opened which link.

## Who else sees what

The services the software runs on:

- **Telegram** runs the bot and the app's window. It sees who uses the bot,
  which links they open, and when notices are sent. It doesn't see messages or
  attachments.
- **Amazon Web Services** hosts the backend and storage. It sees the IP
  address of every request the app makes, and holds the encrypted data listed
  above.
- **GitHub Pages** serves the app's files. It sees the IP address of every app
  load, and the link being opened, which is part of the page address.
- **The device's browser**, when an attachment is saved outside Telegram,
  opens a save page; the address may stay in its history (it opens nothing
  once the attachment is saved).

None of them receive message content. The app loads no third-party code and
has no analytics or trackers.

## What timing can reveal

Even without stored records, someone watching the servers or Telegram live
could link people by timing: a message arriving, then a notice, then a reply
or a "seen". This is explained in [security.md](security.md#what-still-leaks).

## What users can do

- **Delete a conversation** on their device. The other person keeps theirs.
- **Block** someone. They're not told; their messages are no longer delivered.
- **Replace their link.** The old one stops working at once.
- **Turn notices off** by blocking the bot in Telegram.
- **Delete their account** in the app (on the Link tab). Everything the server
  holds for it is deleted at once (the link, mailboxes, waiting messages and
  attachments, invitation codes, keys, and the hash of the Telegram ID), and
  then everything on the device. They can set up a new account later.
- **Set up again on another device** if they can't use theirs any more. This
  replaces the old account; its links and mailboxes are deleted at once.

Since the server keeps no content and no record of who talks to whom, there's
nothing of that kind to hand over to anyone. What it does keep is listed
above.
