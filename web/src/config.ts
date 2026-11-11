// Settings baked in at build time (see vite.config.ts, which validates them
// and adds the API to the Content Security Policy). Nothing is configured at
// runtime: a page that fetched its settings could be pointed elsewhere.

export interface Config {
  /** Base URL of the HTTPS API, without a trailing slash. */
  apiUrl: string;
  /** The bot's username, for t.me links. */
  botUsername: string;
  /** The Mini App's short name, for t.me links. */
  appName: string;
}

const env = import.meta.env;

/** The build's settings, or null for a build made without them (tests, previews). */
export const config: Config | null =
  env.VITE_API_URL && env.VITE_BOT_USERNAME && env.VITE_APP_NAME
    ? { apiUrl: env.VITE_API_URL.replace(/\/+$/, ''), botUsername: env.VITE_BOT_USERNAME, appName: env.VITE_APP_NAME }
    : null;
