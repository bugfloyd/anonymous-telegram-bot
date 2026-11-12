import { loadEnv, type Plugin } from 'vite';
import { defineConfig } from 'vitest/config';
import issuerKeys from './issuer-keys.json' with { type: 'json' };

// Build settings come from VITE_* environment variables (CI sets them per
// environment; see src/config.ts). They are checked here so a bad value fails
// the build instead of shipping. A build without them still works, but can't
// reach a backend.
interface BuildSettings {
  apiOrigin: string | null;
  /** The WebSocket for pushes (optional: without it the app polls). */
  wsOrigin: string | null;
  /** The media bucket (optional: without it attachments are off). */
  mediaOrigin: string | null;
}

function checkSettings(env: Record<string, string>): BuildSettings {
  const api = env.VITE_API_URL ?? '';
  const bot = env.VITE_BOT_USERNAME ?? '';
  const app = env.VITE_APP_NAME ?? '';
  const issuer = env.VITE_ISSUER_KEY ?? '';
  if (!api && !bot && !app && !issuer) return { apiOrigin: null, wsOrigin: null, mediaOrigin: null };
  if (!api || !bot || !app || !issuer) {
    throw new Error('Set all of VITE_API_URL, VITE_BOT_USERNAME, VITE_APP_NAME and VITE_ISSUER_KEY, or none');
  }
  if (issuer === '_comment' || !(issuer in issuerKeys)) {
    throw new Error(`VITE_ISSUER_KEY must name a key published in issuer-keys.json`);
  }
  const url = new URL(api);
  if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) {
    throw new Error('VITE_API_URL must be a plain https:// URL');
  }
  if (!/^[A-Za-z][A-Za-z0-9_]{3,31}$/.test(bot)) throw new Error('VITE_BOT_USERNAME is not a bot username');
  if (!/^[A-Za-z0-9_]{3,30}$/.test(app)) throw new Error('VITE_APP_NAME is not a Mini App short name');
  let wsOrigin: string | null = null;
  if (env.VITE_WS_URL) {
    const ws = new URL(env.VITE_WS_URL);
    if (ws.protocol !== 'wss:' || ws.username || ws.password || ws.search || ws.hash) {
      throw new Error('VITE_WS_URL must be a plain wss:// URL');
    }
    wsOrigin = ws.origin;
  }
  let mediaOrigin: string | null = null;
  if (env.VITE_MEDIA_ORIGIN) {
    const media = new URL(env.VITE_MEDIA_ORIGIN);
    if (media.protocol !== 'https:' || media.origin !== env.VITE_MEDIA_ORIGIN.replace(/\/+$/, '')) {
      throw new Error('VITE_MEDIA_ORIGIN must be a plain https:// origin');
    }
    mediaOrigin = media.origin;
  }
  return { apiOrigin: url.origin, wsOrigin, mediaOrigin };
}

// The Content Security Policy is a <meta> tag because GitHub Pages can't send
// headers. It is only added to production builds: Vite's dev server injects
// inline scripts that the policy would block. The page may only connect to
// its own origin, the API, the push socket and the media bucket; media
// (decrypted attachments) only plays from blob: URLs.
function policy(settings: BuildSettings): string {
  return [
    "default-src 'none'",
    "script-src 'self'",
    "style-src 'self'",
    "img-src 'self' data: blob:",
    "font-src 'self'",
    ['connect-src', "'self'", settings.apiOrigin, settings.wsOrigin, settings.mediaOrigin].filter(Boolean).join(' '),
    "media-src blob:",
    "manifest-src 'self'",
    "base-uri 'none'",
    "form-action 'none'",
    "object-src 'none'",
    "require-trusted-types-for 'script'",
  ].join('; ');
}

function contentSecurityPolicy(csp: string): Plugin {
  return {
    name: 'content-security-policy',
    apply: 'build',
    transformIndexHtml: {
      order: 'pre',
      handler: (html) =>
        html.replace(
          '<meta charset="utf-8">',
          `<meta charset="utf-8">\n<meta http-equiv="Content-Security-Policy" content="${csp}">`,
        ),
    },
  };
}

export default defineConfig(({ mode }) => ({
  // Relative paths: the same build works at a domain root, under a subpath, and
  // inside native app shells.
  base: './',
  plugins: [contentSecurityPolicy(policy(checkSettings(loadEnv(mode, '.', 'VITE_'))))],
  oxc: {
    jsx: { runtime: 'automatic', importSource: 'preact' },
  },
  build: {
    target: 'es2022',
    // No inline polyfill script (it would break the CSP), and no lazy chunks:
    // a deploy replaces the whole site, so a cached page must never point at
    // chunks that no longer exist.
    modulePreload: { polyfill: false },
    cssCodeSplit: false,
    assetsInlineLimit: 0,
    sourcemap: true,
    rolldownOptions: {
      output: {
        codeSplitting: false,
        entryFileNames: 'assets/app-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
  test: {
    include: ['src/**/*.test.ts'],
  },
}));
