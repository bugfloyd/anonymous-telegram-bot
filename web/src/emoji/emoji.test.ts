import { describe, expect, it } from 'vitest';
import { EMOJI_GROUPS } from './data';
import { deleteBefore, insertAt } from './edit';

describe('emoji data', () => {
  const all = EMOJI_GROUPS.flatMap((g) => g.emoji.split(' '));
  const graphemes = (s: string) => [...new Intl.Segmenter(undefined, { granularity: 'grapheme' }).segment(s)].length;

  it('has a keyboard’s categories, each with emoji', () => {
    expect(EMOJI_GROUPS.map((g) => g.id)).toEqual(['people', 'nature', 'food', 'activity', 'travel', 'objects', 'symbols', 'flags']);
    for (const g of EMOJI_GROUPS) expect(g.emoji.split(' ').length).toBeGreaterThan(50);
  });

  it('lists each emoji once, as one character, in the default skin tone', () => {
    expect(new Set(all).size).toBe(all.length);
    for (const e of all) expect(graphemes(e)).toBe(1);
    expect(all.some((e) => /[\u{1F3FB}-\u{1F3FF}]/u.test(e))).toBe(false);
    expect(all).toContain('😀');
    expect(all).toContain('👨‍👩‍👧');
    expect(all).toContain('🏳️‍🌈');
  });
});

describe('typing from the emoji panel', () => {
  it('inserts at the cursor or in place of the selection', () => {
    expect(insertAt('hi', { start: 2, end: 2 }, '👋')).toEqual({ text: 'hi👋', caret: { start: 4, end: 4 } });
    expect(insertAt('hi there', { start: 3, end: 8 }, '🙂')).toEqual({ text: 'hi 🙂', caret: { start: 5, end: 5 } });
    expect(insertAt('', { start: 9, end: 9 }, '😀')).toEqual({ text: '😀', caret: { start: 2, end: 2 } });
    // Never past the box's limit.
    expect(insertAt('abc', { start: 3, end: 3 }, '😀', 4)).toEqual({ text: 'abc', caret: { start: 3, end: 3 } });
  });

  it('deletes a whole emoji, joined sequences and flags included, before the cursor', () => {
    expect(deleteBefore('ok 👨‍👩‍👧', { start: 11, end: 11 })).toEqual({ text: 'ok ', caret: { start: 3, end: 3 } });
    expect(deleteBefore('🇮🇷!', { start: 4, end: 4 })).toEqual({ text: '!', caret: { start: 0, end: 0 } });
    expect(deleteBefore('سلام', { start: 4, end: 4 })).toEqual({ text: 'سلا', caret: { start: 3, end: 3 } });
    expect(deleteBefore('abc', { start: 0, end: 0 })).toEqual({ text: 'abc', caret: { start: 0, end: 0 } });
    expect(deleteBefore('abcdef', { start: 1, end: 4 })).toEqual({ text: 'aef', caret: { start: 1, end: 1 } });
  });
});
