// Typing into the message box from the emoji panel, while the box isn't
// focused (focusing it would bring the keyboard up over the panel): the
// cursor is tracked here, not read from the box.

/** Where the cursor (or selection) is in the message box. */
export interface Caret {
  start: number;
  end: number;
}

/** Puts s in place of the selection; the cursor ends after it. */
export function insertAt(text: string, caret: Caret, s: string, maxLength = Infinity): { text: string; caret: Caret } {
  const start = Math.min(caret.start, text.length);
  const end = Math.min(Math.max(caret.end, start), text.length);
  const next = text.slice(0, start) + s + text.slice(end);
  if (next.length > maxLength) return { text, caret: { start, end } };
  const at = start + s.length;
  return { text: next, caret: { start: at, end: at } };
}

/** Deletes the selection, or the whole character before the cursor: an emoji with its joiners and modifiers counts as one. */
export function deleteBefore(text: string, caret: Caret): { text: string; caret: Caret } {
  let start = Math.min(caret.start, text.length);
  const end = Math.min(Math.max(caret.end, start), text.length);
  if (start === end) {
    if (start === 0) return { text, caret: { start: 0, end: 0 } };
    const last = [...new Intl.Segmenter(undefined, { granularity: 'grapheme' }).segment(text.slice(0, start))].at(-1);
    start = last ? last.index : start - 1;
  }
  return { text: text.slice(0, start) + text.slice(end), caret: { start, end: start } };
}
