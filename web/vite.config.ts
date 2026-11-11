import { loadEnv, type Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

// Build settings come from VITE_* environment variables (CI sets them per
// environment; see src/config.ts). They are checked here so a bad value fails
// the build instead of shipping. A build without them still works, but can't
// reach a backend.
interface BuildSettings {
  apiOrigin: string | null;
}

function checkSettings(env: Record<string, string>): BuildSettings {
  const api = env.VITE_API_URL ?? '';
  const bot = env.VITE_BOT_USERNAME ?? '';
  const app = env.VITE_APP_NAME ?? '';
  if (!api && !bot && !app) return { apiOrigin: null };
  if (!api || !bot || !app) throw new Error('Set all of VITE_API_URL, VITE_BOT_USERNAME and VITE_APP_NAME, or none');
  const url = new URL(api);
  if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) {
    throw new Error('VITE_API_URL must be a plain https:// URL');
  }
  if (!/^[A-Za-z][A-Za-z0-9_]{3,31}$/.test(bot)) throw new Error('VITE_BOT_USERNAME is not a bot username');
  if (!/^[A-Za-z0-9_]{3,30}$/.test(app)) throw new Error('VITE_APP_NAME is not a Mini App short name');
  return { apiOrigin: url.origin };
}

// The Content Security Policy is a <meta> tag because GitHub Pages can't send
// headers. It is only added to production builds: Vite's dev server injects
// inline scripts that the policy would block. The page may only connect to
// its own origin and the API.
function policy(settings: BuildSettings): string {
  return [
    "default-src 'none'",
    "script-src 'self'",
    "style-src 'self'",
    "img-src 'self' data: blob:",
    "font-src 'self'",
    ['connect-src', "'self'", settings.apiOrigin].filter(Boolean).join(' '),
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
