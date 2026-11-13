import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { openDatabase, putMeta } from '../storage/db';
import { DEFAULT_MEDIA_SETTINGS, getMediaSettings, keepCutoff, putMediaSettings } from './settings';

describe('media settings', () => {
  it('starts with keeping everything and fetching photos, voice and GIFs', async () => {
    const db = await openDatabase(`settings-${crypto.randomUUID()}`);
    expect(await getMediaSettings(db)).toEqual(DEFAULT_MEDIA_SETTINGS);
  });

  it('keeps what was saved, and fills in what an older version left out', async () => {
    const db = await openDatabase(`settings-${crypto.randomUUID()}`);
    await putMediaSettings(db, { keep: 'week', auto: { photos: false, voice: true, gifs: false } });
    expect(await getMediaSettings(db)).toEqual({ keep: 'week', auto: { photos: false, voice: true, gifs: false } });
    await putMeta(db, 'media-settings', { keep: 'nonsense', auto: { photos: false } });
    expect(await getMediaSettings(db)).toEqual({ keep: 'forever', auto: { photos: false, voice: true, gifs: true } });
  });

  it('turns "keep media" into a cutoff time', () => {
    const now = Date.UTC(2026, 10, 14);
    expect(keepCutoff('forever', now)).toBeNull();
    expect(keepCutoff('week', now)).toBe(now - 7 * 24 * 3600 * 1000);
    expect(keepCutoff('month', now)).toBe(now - 30 * 24 * 3600 * 1000);
  });
});
