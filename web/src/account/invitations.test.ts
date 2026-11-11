import { describe, expect, it } from 'vitest';
import { checkInvitations, invitationUrl, normalizeCode, parseInvitationPayload } from './invitations';

describe('invitation codes', () => {
  it('accept what people paste, like the backend', () => {
    const valid: Record<string, string> = {
      'abcd-efgh': 'abcd-efgh',
      'ABCD-EFGH': 'abcd-efgh',
      '  abcd efgh ': 'abcd-efgh',
      abcdefgh: 'abcd-efgh',
      invabcdefgh: 'abcd-efgh',
      'https://t.me/some_bot/app?startapp=invabcdefgh': 'abcd-efgh',
    };
    for (const [input, want] of Object.entries(valid)) expect(normalizeCode(input)).toBe(want);
    for (const input of ['', 'abc-def', 'abcd-efgh-jk', 'abcd-efg0', 'abcd-efgl', 'hello there', 'https://t.me/some_bot/app?startapp=1abc']) {
      expect(normalizeCode(input)).toBeNull();
    }
  });

  it('round-trip through invitation links', () => {
    const url = invitationUrl('some_bot', 'app', 'abcd-efgh');
    expect(url).toBe('https://t.me/some_bot/app?startapp=invabcdefgh');
    expect(parseInvitationPayload(new URL(url).searchParams.get('startapp')!)).toBe('abcd-efgh');
    expect(parseInvitationPayload('reg' + 'a'.repeat(43))).toBeNull();
    expect(parseInvitationPayload('INVABCDEFGH')).toBeNull();
  });

  it('check the server response strictly', () => {
    const ok = { inviteOnly: true, member: true, admin: false, level: 1, unlimited: false, remaining: 2, maxUses: 2, codes: [{ code: 'abcd-efgh', uses: 2, left: 1 }] };
    expect(checkInvitations(ok).codes).toHaveLength(1);
    expect(() => checkInvitations({ ...ok, codes: [{ code: '<b>', uses: 1, left: 1 }] })).toThrow();
    expect(() => checkInvitations({ ...ok, remaining: -1 })).toThrow();
  });
});
