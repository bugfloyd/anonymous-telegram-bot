import { describe, expect, it } from 'vitest';
import { concat, toBase64Url, utf8 } from '../crypto/encoding';
import { exportPublicKey, generateCopyableAgreementKey } from '../crypto/keys';
import { keyId, sealEnvelope } from './envelope';
import { checkMessage, MAX_TEXT_BYTES, MessageError, newId, openMessage, sealMessage, type ConversationKey, type Draft } from './message';

async function conversationKey(): Promise<ConversationKey & { id: Uint8Array }> {
  const pair = await generateCopyableAgreementKey();
  const publicKey = await exportPublicKey(pair.publicKey);
  return { privateKey: pair.privateKey, publicKey, id: await keyId(publicKey) };
}

const finder = (...keys: (ConversationKey & { id: Uint8Array })[]) => async (id: Uint8Array) =>
  keys.find((k) => k.id.every((b, i) => b === id[i]));

const draft = (extra: Partial<Draft> = {}): Draft => ({ kind: 'message', conv: newId(), id: newId(), at: 1_800_000_000_000, text: 'سلام 👋', ...extra });

describe('messages', () => {
  it('round-trip, carrying who wrote them and for which key', async () => {
    const sender = await conversationKey();
    const linkKey = await conversationKey();
    const reply = newId();
    const tag = toBase64Url(crypto.getRandomValues(new Uint8Array(32)));
    const envelope = await sealMessage(draft({ reply, tag }), sender, linkKey.publicKey);
    const { message, keyId: opened } = await openMessage(envelope, finder(linkKey));
    expect(message).toMatchObject({ v: 1, kind: 'message', text: 'سلام 👋', reply, tag, from: toBase64Url(sender.publicKey) });
    expect(opened).toEqual(linkKey.id);
  });

  it('reject a claimed sender who did not write it', async () => {
    const victim = await conversationKey();
    const attacker = await conversationKey();
    const recipient = await conversationKey();
    // The attacker puts the victim's key in "from" but can only tag with their own.
    const forged = await sealMessage(draft(), { privateKey: attacker.privateKey, publicKey: victim.publicKey }, recipient.publicKey);
    await expect(openMessage(forged, finder(recipient))).rejects.toThrow('bad tag');
  });

  it('reject a message re-addressed to another key', async () => {
    const sender = await conversationKey();
    const meant = await conversationKey();
    const other = await conversationKey();
    // A genuine message for "meant", re-wrapped (by someone holding both) to "other".
    const json = utf8(
      JSON.stringify({ v: 1, ...draft(), from: toBase64Url(sender.publicKey), to: toBase64Url(meant.id) }),
    );
    const envelope = await sealEnvelope([other.publicKey], concat(new Uint8Array(32), json));
    await expect(openMessage(envelope, finder(other))).rejects.toThrow('written for another key');
  });

  it('carry seen receipts and key updates without text', async () => {
    const a = await conversationKey();
    const b = await conversationKey();
    const seen = [newId(), newId()];
    const receipt: Draft = { kind: 'seen', conv: newId(), id: newId(), at: 1, seen };
    const { message } = await openMessage(await sealMessage(receipt, a, b.publicKey), finder(b));
    expect(message.seen).toEqual(seen);
    const update = await sealMessage({ kind: 'key_update', conv: newId(), id: newId(), at: 1 }, a, b.publicKey);
    expect((await openMessage(update, finder(b))).message.kind).toBe('key_update');
  });

  it('limit text length', async () => {
    const a = await conversationKey();
    const b = await conversationKey();
    await expect(sealMessage(draft({ text: 'x'.repeat(MAX_TEXT_BYTES + 1) }), a, b.publicKey)).rejects.toThrow(MessageError);
    const longest = await sealMessage(draft({ text: 'x'.repeat(MAX_TEXT_BYTES) }), a, b.publicKey);
    expect((await openMessage(longest, finder(b))).message.text).toHaveLength(MAX_TEXT_BYTES);
  });
});

describe('checkMessage', () => {
  const valid = {
    v: 1,
    kind: 'message',
    conv: 'AAAAAAAAAAAAAAAAAAAAAA',
    id: 'AAAAAAAAAAAAAAAAAAAAAA',
    at: 1,
    from: toBase64Url(new Uint8Array(65)),
    to: 'AAAAAAAAAAA',
    text: 'hi',
  };

  it('accepts a well-formed message and ignores unknown fields', () => {
    expect(checkMessage({ ...valid, future: true }).text).toBe('hi');
  });

  it.each([
    ['version', { v: 2 }],
    ['kind', { kind: 'other' }],
    ['short conv', { conv: 'AAAA' }],
    ['non-canonical id', { id: 'AAAAAAAAAAAAAAAAAAAAAB' }],
    ['time', { at: -1 }],
    ['to', { to: 'AAAAAAAAAAAAAAAA' }],
    ['empty text', { text: '  ' }],
    ['text on seen', { kind: 'seen', seen: ['AAAAAAAAAAAAAAAAAAAAAA'] }],
    ['seen without ids', { kind: 'seen', text: undefined }],
    ['text on end', { kind: 'end' }],
    ['bad reply', { reply: 'x' }],
  ])('rejects bad %s', (_, change) => {
    expect(() => checkMessage({ ...valid, ...change })).toThrow(MessageError);
  });
});
