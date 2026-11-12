/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL?: string;
  readonly VITE_BOT_USERNAME?: string;
  readonly VITE_APP_NAME?: string;
  readonly VITE_ISSUER_KEY?: string;
  readonly VITE_WS_URL?: string;
  readonly VITE_MEDIA_ORIGIN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
