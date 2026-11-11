// Invitations (design D5, v1's rules): codes like "abcd-efgh", shared as
// t.me/<bot>/<app>?startapp=inv<code without the dash>. Joining with one makes
// this account a member, one level below the code's owner.

import { checkObject } from '../net/api';

export const PAYLOAD_PREFIX = 'inv';
const ALPHABET = 'abcdefghjkmnpqrstuvwxyz23456789';

export interface InvitationCode {
  code: string;
  uses: number;
  left: number;
}

export interface InvitationsInfo {
  inviteOnly: boolean;
  member: boolean;
  admin: boolean;
  level: number;
  unlimited: boolean;
  remaining: number;
  /** The most uses one new code can have now. */
  maxUses: number;
  codes: InvitationCode[];
}

export interface LevelAllowance {
  level: number;
  invitations: number;
}

const int = (v: unknown): v is number => Number.isSafeInteger(v) && (v as number) >= 0;

export function checkInvitations(value: unknown): InvitationsInfo {
  const v = checkObject(value);
  const { inviteOnly, member, admin, level, unlimited, remaining, maxUses, codes } = v;
  if (typeof inviteOnly !== 'boolean' || typeof member !== 'boolean' || typeof admin !== 'boolean' || typeof unlimited !== 'boolean') {
    throw new Error('bad invitations');
  }
  if (!int(level) || !int(remaining) || !int(maxUses) || !Array.isArray(codes)) throw new Error('bad invitations');
  return {
    inviteOnly,
    member,
    admin,
    level,
    unlimited,
    remaining,
    maxUses,
    codes: codes.map((c: unknown) => {
      const { code, uses, left } = checkObject(c);
      if (typeof code !== 'string' || normalizeCode(code) !== code || !int(uses) || !int(left)) throw new Error('bad code');
      return { code, uses, left };
    }),
  };
}

export function checkLevels(value: unknown): LevelAllowance[] {
  const { levels } = checkObject(value);
  if (!Array.isArray(levels)) throw new Error('bad levels');
  return levels.map((l: unknown) => {
    const { level, invitations } = checkObject(l);
    if (!int(level) || !int(invitations)) throw new Error('bad level');
    return { level, invitations };
  });
}

export function checkCreated(value: unknown): string {
  const { code } = checkObject(value);
  if (typeof code !== 'string' || normalizeCode(code) !== code) throw new Error('bad code');
  return code;
}

export function checkRedeemed(value: unknown): number {
  const { level } = checkObject(value);
  if (!int(level)) throw new Error('bad level');
  return level;
}

/**
 * Accepts what people paste (any case, spaces, a missing dash, the start
 * parameter or the whole invitation link) and returns "abcd-efgh", or null.
 * Matches the backend's invitations.Normalize.
 */
export function normalizeCode(input: string): string | null {
  let s = input.trim();
  try {
    const url = new URL(s);
    s = url.searchParams.get('startapp') ?? '';
  } catch {
    // Not a URL.
  }
  s = s.replace(/\s+/g, '').toLowerCase();
  if (s.startsWith(PAYLOAD_PREFIX)) s = s.slice(PAYLOAD_PREFIX.length);
  s = s.replace(/-/g, '');
  if (s.length !== 8 || [...s].some((c) => !ALPHABET.includes(c))) return null;
  return `${s.slice(0, 4)}-${s.slice(4)}`;
}

/** The code in an invitation's start parameter, or null. */
export function parseInvitationPayload(startParam: string): string | null {
  return /^inv[a-z2-9]{8}$/.test(startParam) ? normalizeCode(startParam) : null;
}

export function invitationUrl(botUsername: string, appName: string, code: string): string {
  return `https://t.me/${botUsername}/${appName}?startapp=${PAYLOAD_PREFIX}${code.replace('-', '')}`;
}
