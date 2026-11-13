# Privacy policy

This policy covers Incognito Chat: the Telegram bot and the app it opens.
It's written to be read in full; nothing here is hidden in legal language.
How the protections work is in [security.md](security.md) and
[architecture.md](architecture.md).

> **Before publishing:** the operator fills in `<operator>` and `<contact>`,
> and registers this policy with BotFather.

**Operator:** `<operator>` · **Contact:** `<contact>`

## The short version

- Your messages and attachments are encrypted on your device. Nobody else can
  read them: not us, not Telegram, not our hosting providers.
- We don't store who writes to whom, or who an anonymous sender is.
- Your chats exist only on your device. We can't restore them, and we can't
  hand them to anyone, because we don't have them.
- Our servers do see some things they can't avoid seeing, such as IP addresses
  and when messages arrive. They're listed below.

## What stays on your device

Your keys, your conversations, their history, attachments you've received or
sent, labels, and blocks. History and attachments are encrypted under a key
that can't leave the device. None of it is sent to us in readable form. It's
deleted when you delete a conversation, when you set up again, or when
Telegram or your phone clears the app's storage.

## What our servers keep

| What | Why | How long |
|---|---|---|
| A random account ID, and a keyed hash of your Telegram user ID (not the ID itself) | One account per Telegram account | While the account exists |
| Your account's and device's public keys | To check logins and requests | While the account exists |
| Your Telegram chat ID, encrypted so only the notice service can open it, and your language | To send "New message" notices, if you allow them | While notices are on |
| Your link: its ID, its public keys, its first-contact mailbox | So people can write to you | Until you replace it, or your account ends |
| Your mailboxes: random IDs, and that they're yours | To deliver messages to you | Until they're ended or expire |
| Messages waiting for your device, encrypted | Delivery | Until your device confirms them, at most 30 days |
| Attachments waiting to be fetched or saved, encrypted, with their padded size | Delivery | Until saved, deleted, or at most 31 days |
| Invitation codes you created, and your invitation level | Invite-only access to links | While the account exists |
| Short-lived counters, login nonces, spent send tokens, open-socket records | Rate limits, replay protection, live updates | Minutes, up to two weeks |
| Error logs, without IDs, IP addresses or content | Fixing problems | 7 days |

After you set up again (which ends your old account), a marker and the old
device's public key are kept for 90 days, so the old device can tell you it
was replaced.

**What we never store:** the content of any message or attachment in readable
form, who sent a message, which conversations belong together, your name,
username or phone number, and your IP address. Senders don't log in to send:
they pay with anonymous tokens that can't be linked to their account.

## What we receive from Telegram

When you set up, Telegram gives the app signed launch data about you (your
Telegram ID, name, username and language). Our server checks Telegram's
signature, keeps the keyed hash of your ID described above, and forgets the
rest. When you allow notices, it also keeps your chat ID, encrypted, and your
language. When you open someone's link, the app never sends that launch data
to our server, so it doesn't learn who opened which link.

## Who else sees what

- **Telegram** runs the bot and the app's window. It sees that you use the
  bot, which links you open, and when you get a notice. It doesn't see
  messages or attachments.
- **Amazon Web Services** hosts our servers and storage. It sees the IP
  address of every request your app makes, and holds the encrypted data
  listed above.
- **GitHub** serves the app's files. It sees the IP address of every app
  load, and the link being opened, which is part of the page address.
- **Your device's browser**, when you save an attachment outside Telegram,
  opens a save page; the address may stay in its history (it opens nothing
  once the attachment is saved).

None of them receive message content, and we share nothing with anyone else.
We don't sell or rent data, show ads, or use analytics or trackers. The app
loads no third-party code.

## What timing can reveal

Even without stored records, someone watching our servers or Telegram live
could link people by timing: a message arriving, then a notice, then a reply
or a "seen". This is explained in [security.md](security.md#what-still-leaks).

## Your choices

- **Delete a conversation** on your device. The other person keeps theirs.
- **Block** someone. They're not told; their messages are no longer delivered
  to you.
- **Replace your link.** The old one stops working at once.
- **Turn notices off** by blocking the bot in Telegram.
- **End your account** by setting up again. Its links and mailboxes are
  deleted at once. A button to delete an account outright is planned; until
  then, ask us at `<contact>`, and we'll delete the account's records.

Data requests: since we keep no content and no record of who talks to whom,
there's nothing of that kind we could hand over. What we do keep is listed
above.

## Changes

This policy is kept in the app's public repository, so every change to it is
visible with its date.
