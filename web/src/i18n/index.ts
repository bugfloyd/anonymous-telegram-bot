// Strings and formatting in English and Persian. Hand-written and typed, so
// both languages always have every string; numbers, dates and relative times
// come from Intl (Persian digits and calendar included).

import { en, type Key } from './en';
import { fa } from './fa';

export type Language = 'en' | 'fa';

const strings: Record<Language, Record<Key, string>> = { en, fa };

export const DIRECTION: Record<Language, 'ltr' | 'rtl'> = { en: 'ltr', fa: 'rtl' };

/**
 * Picks the language: an explicit choice wins, then the language Telegram
 * reports for the user, then the browser's. Anything that isn't Persian is
 * shown in English.
 */
export function pickLanguage(...candidates: (string | null | undefined)[]): Language {
  for (const candidate of candidates) {
    if (!candidate) continue;
    const tag = candidate.toLowerCase();
    if (tag === 'fa' || tag.startsWith('fa-') || tag.startsWith('fa_')) return 'fa';
    if (tag === 'en' || tag.startsWith('en-') || tag.startsWith('en_')) return 'en';
  }
  return 'en';
}

/** Translates a key, filling {placeholders}. */
export function t(language: Language, key: Key, values: Record<string, string | number> = {}): string {
  return strings[language][key].replace(/\{(\w+)\}/g, (match, name: string) =>
    name in values ? formatValue(language, values[name]!) : match,
  );
}

function formatValue(language: Language, value: string | number): string {
  return typeof value === 'number' ? new Intl.NumberFormat(locale(language)).format(value) : value;
}

/** The BCP 47 locale used with Intl. */
export function locale(language: Language): string {
  return language === 'fa' ? 'fa-IR' : 'en';
}

/** "3 minutes ago" / «۳ دقیقه پیش». */
export function relativeTime(language: Language, from: Date, now = new Date()): string {
  const seconds = Math.round((from.getTime() - now.getTime()) / 1000);
  const rtf = new Intl.RelativeTimeFormat(locale(language), { numeric: 'auto' });
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 31_536_000],
    ['month', 2_592_000],
    ['day', 86_400],
    ['hour', 3_600],
    ['minute', 60],
  ];
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) return rtf.format(Math.trunc(seconds / size), unit);
  }
  return rtf.format(seconds, 'second');
}

/** Applies the language and direction to the document. */
export function applyLanguage(language: Language, doc: Document = document): void {
  doc.documentElement.lang = language;
  doc.documentElement.dir = DIRECTION[language];
}
