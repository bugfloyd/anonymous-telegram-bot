import type { Key } from './en';

export const fa = {
  appName: 'چت ناشناس',
  building: 'نسخهٔ ۲ در حال ساخت است.',
  openedInTelegram: 'در تلگرام باز شده ({platform})',
  openedInBrowser: 'در مرورگر باز شده',
  languageName: 'فارسی',
} as const satisfies Record<Key, string>;
