// Maps Telegram's theme colours onto the app's own design tokens. Outside
// Telegram the tokens keep their CSS defaults (light/dark from the system).

const TOKENS: Record<string, string> = {
  bg_color: '--app-bg',
  text_color: '--app-text',
  hint_color: '--app-muted',
  secondary_bg_color: '--app-surface',
  button_color: '--app-accent',
  button_text_color: '--app-on-accent',
  link_color: '--app-link',
  destructive_text_color: '--app-danger',
};

const COLOR = /^#[0-9a-f]{3,8}$/i;

/** Applies Telegram theme parameters to the root element's custom properties. */
export function applyTheme(params: Record<string, string>, root: HTMLElement = document.documentElement): void {
  let applied = false;
  for (const [param, token] of Object.entries(TOKENS)) {
    const value = params[param];
    if (value && COLOR.test(value)) {
      root.style.setProperty(token, value);
      applied = true;
    }
  }
  if (applied) root.dataset.theme = 'telegram';
}

type Insets = { top: number; bottom: number; left: number; right: number };
const insets: { safe?: Insets; content?: Insets } = {};

/**
 * Keeps --app-safe-bottom at the space Telegram says is taken at the bottom
 * of the screen (the iPhone home bar; in full screen, Telegram's own
 * controls too), so the message bar can sit low without going under it.
 */
export function applyInsets(update: { safe?: unknown; content?: unknown }, root: HTMLElement = document.documentElement): void {
  for (const key of ['safe', 'content'] as const) {
    const value = update[key] as Partial<Insets> | undefined;
    if (value && typeof value.bottom === 'number' && Number.isFinite(value.bottom)) insets[key] = value as Insets;
  }
  const bottom = (insets.safe?.bottom ?? 0) + (insets.content?.bottom ?? 0);
  root.style.setProperty('--app-safe-bottom', `${Math.round(Math.min(200, Math.max(0, bottom)))}px`);
}
