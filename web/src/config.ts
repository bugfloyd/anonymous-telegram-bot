// Settings baked in at build time (see vite.config.ts, which validates them
// and adds the API to the Content Security Policy). Nothing is configured at
// runtime: a page that fetched its settings could be pointed elsewhere.

import { fromBase64Url, fromHex } from './crypto/encoding';
import issuerKeys from '../issuer-keys.json';

export interface Config {
  /** Base URL of the HTTPS API, without a trailing slash. */
  apiUrl: string;
  /** The bot's username, for t.me links. */
  botUsername: string;
  /** The Mini App's short name, for t.me links. */
  appName: string;
  /**
   * The send-token issuer's public key, published in issuer-keys.json and
   * named by the build: tokens for any other key are refused.
   */
  issuer: { name: string; spki: Uint8Array<ArrayBuffer>; keyId: Uint8Array<ArrayBuffer> };
  /** The WebSocket that pushes "something arrived"; without it the app polls. */
  wsUrl?: string;
}

const env = import.meta.env;
const keys = issuerKeys as unknown as Record<string, { spki: string; keyId: string } | string>;

function issuer(name: string | undefined): Config['issuer'] | null {
  const entry = name && name !== '_comment' ? keys[name] : undefined;
  if (!entry || typeof entry === 'string') return null;
  return { name: name!, spki: fromBase64Url(entry.spki), keyId: fromHex(entry.keyId) };
}

const issuerKey = issuer(env.VITE_ISSUER_KEY);

/** The build's settings, or null for a build made without them (tests, previews). */
export const config: Config | null =
  env.VITE_API_URL && env.VITE_BOT_USERNAME && env.VITE_APP_NAME && issuerKey
    ? {
        apiUrl: env.VITE_API_URL.replace(/\/+$/, ''),
        botUsername: env.VITE_BOT_USERNAME,
        appName: env.VITE_APP_NAME,
        issuer: issuerKey,
        ...(env.VITE_WS_URL ? { wsUrl: env.VITE_WS_URL } : {}),
      }
    : null;
