import { describe, expect, it } from 'vitest';
import { canRecord, pickType, waveform, WAVEFORM_BARS } from './record';

describe('pickType', () => {
  it('prefers AAC in MP4, so iPhone and Android play each other', () => {
    expect(pickType(() => true)).toBe('audio/mp4;codecs=mp4a.40.2');
  });

  it('falls back to Opus, then to the default', () => {
    expect(pickType((t) => t.startsWith('audio/webm'))).toBe('audio/webm;codecs=opus');
    expect(pickType((t) => t === 'audio/ogg;codecs=opus')).toBe('audio/ogg;codecs=opus');
    expect(pickType(() => false)).toBe('');
  });
});

describe('waveform', () => {
  it('scales the loudest bar to 255', () => {
    const bars = waveform([0.1, 0.2, 0.4, 0.2], 4);
    expect([...bars]).toEqual([64, 128, 255, 128]);
  });

  it('keeps the peak of each stretch when shrinking, and repeats when stretching', () => {
    expect([...waveform([0, 1, 0, 0, 0.5, 0], 2)]).toEqual([255, 128]);
    expect([...waveform([0.5, 1], 4)]).toEqual([128, 128, 255, 255]);
  });

  it('is flat for silence or nothing', () => {
    expect(waveform([], WAVEFORM_BARS).every((b) => b === 0)).toBe(true);
    expect(waveform([0, 0, 0]).length).toBe(WAVEFORM_BARS);
  });
});

it('says recording is unavailable where there is no MediaRecorder', () => {
  expect(canRecord()).toBe(false);
});
