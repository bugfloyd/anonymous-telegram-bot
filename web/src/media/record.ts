// Voice messages (phase 5.4). Recorded with the device's microphone through
// MediaRecorder: as AAC in MP4 where the browser can, so iPhone and Android
// play each other's, else Opus in WebM or Ogg. The waveform comes from the
// microphone's level while recording, so nothing has to decode the result.
// A recording carries no metadata about the device or place.

/** The longest voice message: 15 minutes, a few MB. */
export const MAX_VOICE_MS = 15 * 60 * 1000;
/** Bars in a voice message's waveform. */
export const WAVEFORM_BARS = 48;
/** Recordings shorter than this are dropped, as Telegram does. */
export const MIN_VOICE_MS = 500;
const LEVEL_EVERY_MS = 50;

/** Recording formats, best first. */
const TYPES = ['audio/mp4;codecs=mp4a.40.2', 'audio/mp4', 'audio/webm;codecs=opus', 'audio/ogg;codecs=opus', 'audio/webm'];

/** The best recording format the browser supports, or '' for its default. */
export function pickType(supported: (type: string) => boolean): string {
  return TYPES.find((t) => supported(t)) ?? '';
}

/** Whether this browser can record at all (a Mini App may still be refused the microphone). */
export function canRecord(): boolean {
  return typeof MediaRecorder !== 'undefined' && typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getUserMedia === 'function';
}

/** The microphone can't be used: access refused, none found, or recording isn't supported here. */
export class MicUnavailable extends Error {
  constructor(readonly reason: 'denied' | 'missing' | 'unsupported') {
    super(`microphone ${reason}`);
    this.name = 'MicUnavailable';
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

export interface Recorder {
  /** Milliseconds recorded so far. */
  elapsed(): number;
  /** Stops and returns the recording, or null if it was too short to keep. */
  stop(): Promise<Recording | null>;
  /** Stops and drops it. */
  cancel(): void;
}

/**
 * Starts recording. Call it straight from a tap: the audio context for the
 * level meter is made before waiting for the microphone, while the tap still
 * counts (iPhone). `onLevel` gets the level (0–1) every 50 ms.
 */
export async function startRecording(onLevel?: (level: number) => void): Promise<Recorder> {
  if (!canRecord()) throw new MicUnavailable('unsupported');
  let context: AudioContext | null = null;
  try {
    context = new AudioContext();
  } catch {
    // No level meter or waveform, then.
  }
  let stream: MediaStream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
  } catch (err) {
    void context?.close();
    const name = (err as { name?: string }).name;
    throw new MicUnavailable(name === 'NotAllowedError' || name === 'SecurityError' ? 'denied' : name === 'NotFoundError' || name === 'OverconstrainedError' ? 'missing' : 'unsupported');
  }
  const levels: number[] = [];
  let meter = 0;
  const release = () => {
    clearInterval(meter);
    for (const track of stream.getTracks()) track.stop();
    void context?.close();
  };
  const type = pickType((t) => MediaRecorder.isTypeSupported(t));
  let recorder: MediaRecorder;
  try {
    recorder = new MediaRecorder(stream, { ...(type ? { mimeType: type } : {}), audioBitsPerSecond: 48_000 });
  } catch {
    release();
    throw new MicUnavailable('unsupported');
  }
  const chunks: Blob[] = [];
  recorder.ondataavailable = (e) => {
    if (e.data.size > 0) chunks.push(e.data);
  };
  if (context) {
    try {
      void context.resume();
      const analyser = context.createAnalyser();
      analyser.fftSize = 1024;
      context.createMediaStreamSource(stream).connect(analyser);
      const samples = new Float32Array(analyser.fftSize);
      meter = window.setInterval(() => {
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
  recorder.start();
  const started = performance.now();
  return {
    elapsed: () => performance.now() - started,
    stop: () =>
      new Promise<Recording | null>((resolve, reject) => {
        const ms = performance.now() - started;
        recorder.onerror = () => {
          release();
          reject(new Error('recording failed'));
        };
        recorder.onstop = () => {
          release();
          if (ms < MIN_VOICE_MS || chunks.length === 0) return resolve(null);
          const mime = (recorder.mimeType || type || 'audio/webm').split(';')[0]!.trim();
          void new Blob(chunks, { type: mime }).arrayBuffer().then(
            (buffer) => resolve({ file: new Uint8Array(buffer), type: mime, length: Math.max(1, Math.round(ms / 1000)), waveform: waveform(levels) }),
            reject,
          );
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
