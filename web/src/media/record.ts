// Voice messages (phase 5.4) and round video messages (5.5), recorded with
// MediaRecorder. Voice: AAC in MP4 where the browser can, so iPhone and
// Android play each other's, else Opus in WebM or Ogg; the waveform comes
// from the microphone's level while recording, so nothing has to decode the
// result. Video: the camera's picture is cropped to a square on a canvas
// (mirrored for the front camera, as the person saw it) and that canvas is
// recorded with the microphone, as H.264 in MP4 where the browser can.
// A recording carries no metadata about the device or place.

import { stripVideoMetadata } from './mp4';
import { preview } from './prepare';

/** The longest voice message: 15 minutes, a few MB. */
export const MAX_VOICE_MS = 15 * 60 * 1000;
/** The longest video message: a minute, as in Telegram (about 5 MB). */
export const MAX_ROUND_MS = 60 * 1000;
/** Bars in a voice message's waveform. */
export const WAVEFORM_BARS = 48;
/** Recordings shorter than this are dropped, as Telegram does. */
export const MIN_VOICE_MS = 500;
/** A video message's width and height, in pixels (Telegram's size). */
export const ROUND_SIDE = 384;
const LEVEL_EVERY_MS = 50;
const SNAPSHOT_SIDE = 192;

/** Voice formats, best first. */
const VOICE_TYPES = ['audio/mp4;codecs=mp4a.40.2', 'audio/mp4', 'audio/webm;codecs=opus', 'audio/ogg;codecs=opus', 'audio/webm'];
/** Video message formats, best first. */
const ROUND_TYPES = [
  'video/mp4;codecs=avc1.42E01E,mp4a.40.2',
  'video/mp4;codecs=avc1,mp4a.40.2',
  'video/mp4;codecs=avc1,opus',
  'video/mp4',
  'video/webm;codecs=vp9,opus',
  'video/webm;codecs=vp8,opus',
  'video/webm',
];

/** The best format the browser supports, or '' for its default. */
export function pickType(supported: (type: string) => boolean, types: readonly string[] = VOICE_TYPES): string {
  return types.find((t) => supported(t)) ?? '';
}

/** Whether this browser can record at all (a Mini App may still be refused the microphone or camera). */
export function canRecord(): boolean {
  return typeof MediaRecorder !== 'undefined' && typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getUserMedia === 'function';
}

/** Whether the device has a camera, as far as the browser says before asking. */
export async function hasCamera(): Promise<boolean> {
  if (!canRecord() || typeof navigator.mediaDevices.enumerateDevices !== 'function') return false;
  try {
    return (await navigator.mediaDevices.enumerateDevices()).some((d) => d.kind === 'videoinput');
  } catch {
    return false;
  }
}

/** The microphone or camera can't be used: access refused, none found, or recording isn't supported here. */
export class RecordingUnavailable extends Error {
  constructor(readonly reason: 'denied' | 'missing' | 'unsupported') {
    super(`recording ${reason}`);
    this.name = 'RecordingUnavailable';
  }
}

export interface Recording {
  file: Uint8Array<ArrayBuffer>;
  /** MIME type, without parameters ("audio/mp4"). */
  type: string;
  /** Whole seconds, at least 1. */
  length: number;
  waveform: Uint8Array<ArrayBuffer>;
}

export interface RoundRecording {
  file: Uint8Array<ArrayBuffer>;
  /** MIME type, without parameters ("video/mp4"). */
  type: string;
  /** Whole seconds, at least 1. */
  length: number;
  /** Width and height, in pixels. */
  side: number;
  /** A tiny preview of an early frame (base64url JPEG), if it could be made. */
  preview?: string;
}

export interface Recorder<T = Recording> {
  /** Milliseconds recorded so far. */
  elapsed(): number;
  /** Stops and returns the recording, or null if it was too short to keep. */
  stop(): Promise<T | null>;
  /** Stops and drops it. */
  cancel(): void;
}

export interface RoundRecorder extends Recorder<RoundRecording> {
  /** The camera's picture, and the canvas being recorded (null where a canvas can't be recorded: the camera is then recorded as it is). Both go on the page while recording; the canvas covers the picture. */
  video: HTMLVideoElement;
  canvas: HTMLCanvasElement | null;
  /** Whether the picture is mirrored (front camera, no canvas: the page mirrors it). */
  mirrored(): boolean;
  /** Switches between the front and back cameras, if there are both (canvas only). */
  flip: (() => Promise<void>) | null;
  /** The picture right now, as a small JPEG data URL (null if there's none): shown while the recording is finished off. */
  snapshot(): string | null;
}

/** Asks for the microphone (and camera); refusals become RecordingUnavailable. */
async function devices(constraints: MediaStreamConstraints): Promise<MediaStream> {
  try {
    return await navigator.mediaDevices.getUserMedia(constraints);
  } catch (err) {
    const name = (err as { name?: string }).name;
    throw new RecordingUnavailable(name === 'NotAllowedError' || name === 'SecurityError' ? 'denied' : name === 'NotFoundError' || name === 'OverconstrainedError' ? 'missing' : 'unsupported');
  }
}

const MICROPHONE: MediaTrackConstraints = { echoCancellation: true, noiseSuppression: true, autoGainControl: true };

/** Records a stream; `release` frees what was used for it once it's stopped. */
function capture(stream: MediaStream, types: readonly string[], bitrates: MediaRecorderOptions, release: () => void) {
  const type = pickType((t) => MediaRecorder.isTypeSupported(t), types);
  let recorder: MediaRecorder;
  try {
    recorder = new MediaRecorder(stream, { ...(type ? { mimeType: type } : {}), ...bitrates });
  } catch {
    release();
    throw new RecordingUnavailable('unsupported');
  }
  const chunks: Blob[] = [];
  recorder.ondataavailable = (e) => {
    if (e.data.size > 0) chunks.push(e.data);
  };
  recorder.start();
  const started = performance.now();
  return {
    elapsed: () => performance.now() - started,
    /** The recording and its length in ms, or null if too short. */
    stop: () =>
      new Promise<{ file: Uint8Array<ArrayBuffer>; type: string; ms: number } | null>((resolve, reject) => {
        const ms = performance.now() - started;
        recorder.onerror = () => {
          release();
          reject(new Error('recording failed'));
        };
        recorder.onstop = () => {
          release();
          if (ms < MIN_VOICE_MS || chunks.length === 0) return resolve(null);
          const mime = (recorder.mimeType || type || (stream.getVideoTracks().length ? 'video/webm' : 'audio/webm')).split(';')[0]!.trim();
          void new Blob(chunks, { type: mime }).arrayBuffer().then((buffer) => resolve({ file: new Uint8Array(buffer), type: mime, ms }), reject);
        };
        if (recorder.state === 'inactive') recorder.onstop(new Event('stop'));
        else recorder.stop();
      }),
    cancel: () => {
      recorder.onstop = null;
      if (recorder.state !== 'inactive') recorder.stop();
      release();
    },
  };
}

const seconds = (ms: number) => Math.max(1, Math.round(ms / 1000));

/**
 * Starts recording a voice message. `onLevel` gets the level (0–1) every
 * 50 ms. Started from a held press, the audio context for the level meter
 * may not be allowed to run yet (iPhone): it's woken by the next touch, and
 * the waveform is then taken from the recording itself.
 */
export async function startRecording(onLevel?: (level: number) => void): Promise<Recorder> {
  if (!canRecord()) throw new RecordingUnavailable('unsupported');
  let context: AudioContext | null = null;
  try {
    context = new AudioContext();
  } catch {
    // No level meter or waveform, then.
  }
  let stream: MediaStream;
  try {
    stream = await devices({ audio: MICROPHONE });
  } catch (err) {
    void context?.close();
    throw err;
  }
  const levels: number[] = [];
  let meter = 0;
  const wake = () => void context?.resume().catch(() => {});
  const WAKERS = ['pointerup', 'touchend', 'click', 'keydown'] as const;
  const release = () => {
    clearInterval(meter);
    for (const event of WAKERS) document.removeEventListener(event, wake, true);
    for (const track of stream.getTracks()) track.stop();
    void context?.close();
  };
  if (context && context.state !== 'running') for (const event of WAKERS) document.addEventListener(event, wake, true);
  const recording = capture(stream, VOICE_TYPES, { audioBitsPerSecond: 48_000 }, release);
  if (context) {
    try {
      void context.resume();
      const analyser = context.createAnalyser();
      analyser.fftSize = 1024;
      context.createMediaStreamSource(stream).connect(analyser);
      const samples = new Float32Array(analyser.fftSize);
      meter = window.setInterval(() => {
        // Asleep, it hears nothing: those aren't levels.
        if (context.state !== 'running') return;
        analyser.getFloatTimeDomainData(samples);
        let sum = 0;
        for (const x of samples) sum += x * x;
        // Speech is mostly 0.02–0.25 RMS: scaled so it fills the meter.
        const level = Math.min(1, Math.sqrt(sum / samples.length) * 4);
        levels.push(level);
        onLevel?.(level);
      }, LEVEL_EVERY_MS);
    } catch {
      // No level meter or waveform, then.
    }
  }
  return {
    elapsed: recording.elapsed,
    stop: async () => {
      const result = await recording.stop();
      if (!result) return null;
      // Levels for most of it, or the meter wasn't running: from the recording, then.
      const heard = levels.length * LEVEL_EVERY_MS >= result.ms * 0.8;
      const bars = (heard ? null : await decodedWaveform(result.file)) ?? waveform(levels);
      return { file: result.file, type: result.type, length: seconds(result.ms), waveform: bars };
    },
    cancel: recording.cancel,
  };
}

/** Levels (0–1, any number) to `bars` bars of 0–255, scaled so the loudest is 255. */
export function waveform(levels: number[], bars = WAVEFORM_BARS): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(bars);
  if (levels.length === 0) return out;
  const peak = levels.reduce((a, b) => Math.max(a, b), 0) || 1;
  for (let i = 0; i < bars; i++) {
    const from = Math.floor((i * levels.length) / bars);
    const to = Math.max(from + 1, Math.floor(((i + 1) * levels.length) / bars));
    let max = 0;
    for (let j = from; j < to; j++) max = Math.max(max, levels[j]!);
    out[i] = Math.round((max / peak) * 255);
  }
  return out;
}

/** A recording's waveform, from its decoded sound (at a low rate: a long one stays small); null if it can't be decoded here. */
async function decodedWaveform(file: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer> | null> {
  if (typeof OfflineAudioContext === 'undefined') return null;
  try {
    const sound = await new OfflineAudioContext(1, 1, 8000).decodeAudioData(file.slice().buffer);
    return waveform(levelsOf(sound.getChannelData(0), Math.round((sound.sampleRate * LEVEL_EVERY_MS) / 1000)));
  } catch {
    return null;
  }
}

/** The RMS level of each `step` samples, scaled as the live meter is (0–1). */
export function levelsOf(samples: Float32Array, step: number): number[] {
  const levels: number[] = [];
  for (let i = 0; i < samples.length; i += Math.max(1, step)) {
    const end = Math.min(samples.length, i + Math.max(1, step));
    let sum = 0;
    for (let j = i; j < end; j++) sum += samples[j]! * samples[j]!;
    levels.push(Math.min(1, Math.sqrt(sum / (end - i)) * 4));
  }
  return levels;
}

/** The square in the middle of a w×h picture: its corner and side. */
export function centreSquare(w: number, h: number): { x: number; y: number; side: number } {
  const side = Math.min(w, h);
  return { x: (w - side) / 2, y: (h - side) / 2, side };
}

const camera = (facingMode: 'user' | 'environment'): MediaTrackConstraints => ({ facingMode, width: { ideal: 640 }, height: { ideal: 640 }, frameRate: { ideal: 30 } });

/** Waits until a video has a picture (or 5 s). */
function firstFrame(video: HTMLVideoElement): Promise<void> {
  if (video.readyState >= 2) return Promise.resolve();
  return new Promise((resolve) => {
    const done = () => {
      clearTimeout(timer);
      resolve();
    };
    const timer = setTimeout(done, 5000);
    video.addEventListener('loadeddata', done, { once: true });
  });
}

/**
 * Starts recording a video message from the front camera. Call it straight
 * from a press. The returned video and canvas must be put on the page (some
 * browsers don't draw a video that isn't).
 */
export async function startRound(): Promise<RoundRecorder> {
  if (!canRecord()) throw new RecordingUnavailable('unsupported');
  let facing: 'user' | 'environment' = 'user';
  const stream = await devices({ audio: MICROPHONE, video: camera(facing) });
  if (stream.getVideoTracks().length === 0) {
    for (const track of stream.getTracks()) track.stop();
    throw new RecordingUnavailable('missing');
  }
  const video = document.createElement('video');
  video.muted = true;
  video.playsInline = true;
  video.setAttribute('playsinline', '');
  video.srcObject = new MediaStream(stream.getVideoTracks());
  void video.play().catch(() => {});
  await firstFrame(video);

  const canvas = typeof HTMLCanvasElement.prototype.captureStream === 'function' ? document.createElement('canvas') : null;
  const context = canvas?.getContext('2d') ?? null;
  let frame = 0;
  let still: HTMLCanvasElement | null = null;
  let picture = stream.getVideoTracks();
  const audio = stream.getAudioTracks();
  const release = () => {
    cancelAnimationFrame(frame);
    for (const track of [...picture, ...audio]) track.stop();
    video.srcObject = null;
  };
  let recording: ReturnType<typeof capture> | undefined;
  if (canvas && context) {
    canvas.width = ROUND_SIDE;
    canvas.height = ROUND_SIDE;
    const draw = () => {
      const w = video.videoWidth;
      const h = video.videoHeight;
      if (w && h) {
        const { x, y, side } = centreSquare(w, h);
        const mirror = facing === 'user';
        context.setTransform(mirror ? -1 : 1, 0, 0, 1, mirror ? ROUND_SIDE : 0, 0);
        context.drawImage(video, x, y, side, side, 0, 0, ROUND_SIDE, ROUND_SIDE);
        // An early frame (not the very first, often dark) for the preview.
        if (!still && recording && recording.elapsed() > 300) {
          still = document.createElement('canvas');
          still.width = still.height = 96;
          still.getContext('2d')?.drawImage(canvas, 0, 0, 96, 96);
        }
      }
      frame = requestAnimationFrame(draw);
    };
    draw();
    const tracks = [...canvas.captureStream(30).getVideoTracks(), ...audio];
    recording = capture(new MediaStream(tracks), ROUND_TYPES, { videoBitsPerSecond: 600_000, audioBitsPerSecond: 48_000 }, () => {
      release();
      for (const track of tracks) track.stop();
    });
  } else {
    recording = capture(stream, ROUND_TYPES, { videoBitsPerSecond: 600_000, audioBitsPerSecond: 48_000 }, release);
  }

  const recorded = recording!;
  const cameras = (await navigator.mediaDevices.enumerateDevices().catch(() => [])).filter((d) => d.kind === 'videoinput').length;
  const flip =
    canvas && cameras > 1
      ? async () => {
          const next = facing === 'user' ? 'environment' : 'user';
          // Some phones can't open two cameras at once: the old one goes first.
          for (const track of picture) track.stop();
          try {
            picture = (await navigator.mediaDevices.getUserMedia({ video: camera(next) })).getVideoTracks();
            facing = next;
          } catch {
            picture = (await navigator.mediaDevices.getUserMedia({ video: camera(facing) })).getVideoTracks();
          }
          video.srcObject = new MediaStream(picture);
          void video.play().catch(() => {});
        }
      : null;

  return {
    video,
    canvas: canvas && context ? canvas : null,
    mirrored: () => facing === 'user',
    flip,
    snapshot: () => {
      try {
        const shot = document.createElement('canvas');
        shot.width = shot.height = SNAPSHOT_SIDE;
        const ctx = shot.getContext('2d');
        if (!ctx) return null;
        if (canvas && context) ctx.drawImage(canvas, 0, 0, SNAPSHOT_SIDE, SNAPSHOT_SIDE);
        else {
          const { x, y, side } = centreSquare(video.videoWidth, video.videoHeight);
          if (!side) return null;
          if (facing === 'user') ctx.setTransform(-1, 0, 0, 1, SNAPSHOT_SIDE, 0);
          ctx.drawImage(video, x, y, side, side, 0, 0, SNAPSHOT_SIDE, SNAPSHOT_SIDE);
        }
        return shot.toDataURL('image/jpeg', 0.7);
      } catch {
        return null;
      }
    },
    elapsed: recorded.elapsed,
    stop: async () => {
      const shot = still ?? (canvas && context ? canvas : video.readyState >= 2 ? video : null);
      let tiny: { p?: string } = {};
      try {
        if (shot) tiny = await preview(shot);
      } catch {
        // No preview, then.
      }
      const result = await recorded.stop();
      if (!result) return null;
      if (result.type === 'video/mp4') stripVideoMetadata(result.file);
      const side = canvas && context ? ROUND_SIDE : Math.min(video.videoWidth, video.videoHeight) || ROUND_SIDE;
      return { file: result.file, type: result.type, length: seconds(result.ms), side, ...(tiny.p ? { preview: tiny.p } : {}) };
    },
    cancel: recorded.cancel,
  };
}
