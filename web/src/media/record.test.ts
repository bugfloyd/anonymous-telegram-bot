import { describe, expect, it } from 'vitest';
import { canRecord, centreSquare, hasCamera, levelsOf, pickType, waveform, WAVEFORM_BARS } from './record';

describe('pickType', () => {
  it('prefers AAC in MP4, so iPhone and Android play each other', () => {
    expect(pickType(() => true)).toBe('audio/mp4;codecs=mp4a.40.2');
  });

  it('falls back to Opus, then to the default', () => {
    expect(pickType((t) => t.startsWith('audio/webm'))).toBe('audio/webm;codecs=opus');
    expect(pickType((t) => t === 'audio/ogg;codecs=opus')).toBe('audio/ogg;codecs=opus');
    expect(pickType(() => false)).toBe('');
  });

  it('prefers H.264 and AAC in MP4 for video messages, then WebM', () => {
    const video = ['video/mp4;codecs=avc1.42E01E,mp4a.40.2', 'video/mp4', 'video/webm;codecs=vp9,opus', 'video/webm'];
    expect(pickType(() => true, video)).toBe('video/mp4;codecs=avc1.42E01E,mp4a.40.2');
    expect(pickType((t) => t.startsWith('video/webm'), video)).toBe('video/webm;codecs=vp9,opus');
  });
});

describe('levelsOf', () => {
  it('measures each stretch of a decoded recording', () => {
    const samples = new Float32Array([0, 0, 0.1, -0.1, 0.5, -0.5, 0]);
    const levels = levelsOf(samples, 2);
    expect(levels.length).toBe(4);
    expect(levels[0]).toBe(0);
    expect(levels[1]).toBeCloseTo(0.4);
    expect(levels[2]).toBe(1);
    expect(levels[3]).toBe(0);
  });
});

describe('centreSquare', () => {
  it('crops the middle of a landscape or portrait picture', () => {
    expect(centreSquare(640, 480)).toEqual({ x: 80, y: 0, side: 480 });
    expect(centreSquare(720, 1280)).toEqual({ x: 0, y: 280, side: 720 });
    expect(centreSquare(500, 500)).toEqual({ x: 0, y: 0, side: 500 });
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

it('says recording is unavailable where there is no MediaRecorder', async () => {
  expect(canRecord()).toBe(false);
  expect(await hasCamera()).toBe(false);
});
