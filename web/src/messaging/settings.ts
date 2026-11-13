// What the app keeps on this device and fetches by itself (the Data and
// storage screen). Kept in the meta store; defaults until changed.

import { getMeta, putMeta, type Database } from '../storage/db';

const NAME = 'media-settings';
const DAY = 24 * 60 * 60 * 1000;

/** How long attachments stay on this device. */
export type KeepMedia = 'forever' | 'month' | 'week';

export interface MediaSettings {
  keep: KeepMedia;
  /** Which received attachments are fetched as soon as their conversation is open. */
  auto: { photos: boolean; voice: boolean; gifs: boolean };
}

export const DEFAULT_MEDIA_SETTINGS: MediaSettings = { keep: 'forever', auto: { photos: true, voice: true, gifs: true } };

const KEEP_DAYS: Record<KeepMedia, number | null> = { forever: null, month: 30, week: 7 };

export async function getMediaSettings(db: Database): Promise<MediaSettings> {
  const stored = await getMeta<Partial<MediaSettings>>(db, NAME);
  const keep = stored?.keep && stored.keep in KEEP_DAYS ? stored.keep : DEFAULT_MEDIA_SETTINGS.keep;
  return { keep, auto: { ...DEFAULT_MEDIA_SETTINGS.auto, ...stored?.auto } };
}

export async function putMediaSettings(db: Database, settings: MediaSettings): Promise<void> {
  await putMeta(db, NAME, settings);
}

/** The time before which attachments are cleared from this device, or null to keep them. */
export function keepCutoff(keep: KeepMedia, now: number): number | null {
  const days = KEEP_DAYS[keep];
  return days === null ? null : now - days * DAY;
}
