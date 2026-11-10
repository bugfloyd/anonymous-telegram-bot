import { describe, expect, it } from 'vitest';
import { en } from './en';
import { fa } from './fa';
import { DIRECTION, pickLanguage, relativeTime, t } from './index';

describe('i18n', () => {
  it('has every string in both languages, none empty', () => {
    expect(Object.keys(fa).sort()).toEqual(Object.keys(en).sort());
    for (const value of [...Object.values(en), ...Object.values(fa)]) expect(value.trim()).not.toBe('');
  });

  it('picks the first known language, defaulting to English', () => {
    expect(pickLanguage(undefined, 'fa', 'en-US')).toBe('fa');
    expect(pickLanguage('', 'FA-ir')).toBe('fa');
    expect(pickLanguage('fa_IR')).toBe('fa');
    expect(pickLanguage('de', 'en-GB', 'fa')).toBe('en');
    expect(pickLanguage('de', 'ru')).toBe('en');
    expect(pickLanguage()).toBe('en');
  });

  it('fills placeholders, with Persian digits in Persian', () => {
    expect(t('en', 'openedInTelegram', { platform: 'android' })).toBe('Opened in Telegram (android)');
    expect(t('fa', 'openedInTelegram', { platform: 'android' })).toBe('در تلگرام باز شده (android)');
    expect(t('en', 'openedInTelegram')).toBe('Opened in Telegram ({platform})');
  });

  it('formats relative times', () => {
    const now = new Date('2026-11-10T12:00:00Z');
    expect(relativeTime('en', new Date('2026-11-10T11:57:00Z'), now)).toBe('3 minutes ago');
    expect(relativeTime('fa', new Date('2026-11-10T11:57:00Z'), now)).toBe('۳ دقیقه پیش');
    expect(relativeTime('en', new Date('2026-11-10T12:00:00Z'), now)).toBe('now');
  });

  it('sets right-to-left for Persian', () => {
    expect(DIRECTION.fa).toBe('rtl');
    expect(DIRECTION.en).toBe('ltr');
  });
});
