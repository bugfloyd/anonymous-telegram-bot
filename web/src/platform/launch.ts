// Launch parameters Telegram passes to a Mini App in the URL fragment
// (https://core.telegram.org/api/web-events). Parsed once at start-up; later
// navigation must not depend on the fragment, which a reload can lose.

export interface Launch {
  /** Raw initData query string, exactly as signed by Telegram ('' outside Telegram). */
  initData: string;
  /** Bot API version the Telegram client supports, e.g. "9.6". */
  version: string;
  /** Client platform, e.g. "android", "ios", "tdesktop", "weba", "web". */
  platform: string;
  /** The start parameter of this launch ('' if none). */
  startParam: string;
  /** Theme colours from Telegram, if any. */
  themeParams: Record<string, string>;
  /** The language Telegram reports for the user ('' if none). Nothing else is read from the user. */
  userLanguage: string;
}

/** Parses launch parameters from a URL fragment (with or without the leading '#') and query string. */
export function parseLaunch(fragment: string, search = ''): Launch {
  const hash = new URLSearchParams(fragment.replace(/^#/, ''));
  const query = new URLSearchParams(search.replace(/^\?/, ''));
  const initData = hash.get('tgWebAppData') ?? '';
  const signed = new URLSearchParams(initData);
  return {
    initData,
    version: hash.get('tgWebAppVersion') ?? '',
    platform: hash.get('tgWebAppPlatform') ?? '',
    // The signed value wins; the URL copies are only a fallback.
    startParam: signed.get('start_param') ?? query.get('tgWebAppStartParam') ?? hash.get('tgWebAppStartParam') ?? '',
    themeParams: parseTheme(hash.get('tgWebAppThemeParams')),
    userLanguage: parseUserLanguage(signed.get('user')),
  };
}

function parseUserLanguage(raw: string | null): string {
  if (!raw) return '';
  try {
    const user: unknown = JSON.parse(raw);
    const code = user && typeof user === 'object' ? (user as { language_code?: unknown }).language_code : undefined;
    return typeof code === 'string' && /^[A-Za-z]{2,3}([_-][A-Za-z0-9]{2,8})?$/.test(code) ? code : '';
  } catch {
    return '';
  }
}

function parseTheme(raw: string | null): Record<string, string> {
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
    const theme: Record<string, string> = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value === 'string' && /^#[0-9a-f]{3,8}$/i.test(value)) theme[key] = value;
    }
    return theme;
  } catch {
    return {};
  }
}

/** Whether this page was launched by Telegram. */
export function isTelegramLaunch(launch: Launch): boolean {
  return launch.initData !== '';
}
