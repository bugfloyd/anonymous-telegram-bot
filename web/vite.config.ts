import type { Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

// The Content Security Policy is a <meta> tag because GitHub Pages can't send
// headers. It is only added to production builds: Vite's dev server injects
// inline scripts that the policy would block.
const CSP = [
  "default-src 'none'",
  "script-src 'self'",
  "style-src 'self'",
  "img-src 'self' data: blob:",
  "font-src 'self'",
  "connect-src 'self'",
  "manifest-src 'self'",
  "base-uri 'none'",
  "form-action 'none'",
  "object-src 'none'",
  "require-trusted-types-for 'script'",
].join('; ');

function contentSecurityPolicy(): Plugin {
  return {
    name: 'content-security-policy',
    apply: 'build',
    transformIndexHtml: {
      order: 'pre',
      handler: (html) =>
        html.replace(
          '<meta charset="utf-8">',
          `<meta charset="utf-8">\n<meta http-equiv="Content-Security-Policy" content="${CSP}">`,
        ),
    },
  };
}

export default defineConfig({
  // Relative paths: the same build works at a domain root, under a subpath, and
  // inside native app shells.
  base: './',
  plugins: [contentSecurityPolicy()],
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
});
