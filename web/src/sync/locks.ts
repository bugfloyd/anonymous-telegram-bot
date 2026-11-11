// Several copies of the app can run at once on one device: a reopened Mini
// App, Telegram Desktop windows, Telegram Web tabs. They share storage, so
// anything that must not interleave (creating keys, registering, rotating
// keys, syncing) runs under an exclusive lock, and copies tell each other
// about changes on a broadcast channel.

const PREFIX = 'incognitochat:';

// Each local profile (one per Telegram account, storage/profile.ts) has its
// own locks and channel, like its own database.
let namespace = '';

/** Scopes locks and the channel to one local profile. Call once at start. */
export function useProfile(profile: string): void {
  namespace = profile ? `${profile}:` : '';
}

export interface LockManagerLike {
  request<T>(name: string, options: { mode: 'exclusive' }, callback: () => Promise<T>): Promise<T>;
}

function defaultLocks(): LockManagerLike | undefined {
  return typeof navigator !== 'undefined' ? (navigator.locks as LockManagerLike | undefined) : undefined;
}

/**
 * Runs fn while holding the named lock across every copy of the app on this
 * device. Engines without Web Locks (rare: Safari before 15.4) run fn directly.
 */
export function withLock<T>(name: string, fn: () => Promise<T>, locks: LockManagerLike | undefined = defaultLocks()): Promise<T> {
  if (!locks) return fn();
  return locks.request(PREFIX + namespace + name, { mode: 'exclusive' }, fn);
}

export type SyncEvent = { type: 'account-changed' } | { type: 'keys-changed' } | { type: 'data-changed' };

/** A channel to the other copies of the app. Returns null where unsupported. */
export function openChannel(onEvent: (event: SyncEvent) => void): { send(event: SyncEvent): void; close(): void } | null {
  if (typeof BroadcastChannel === 'undefined') return null;
  const channel = new BroadcastChannel(PREFIX + namespace + 'sync');
  channel.onmessage = (e: MessageEvent<unknown>) => {
    const data = e.data as { type?: unknown } | null;
    if (data && (data.type === 'account-changed' || data.type === 'keys-changed' || data.type === 'data-changed')) {
      onEvent({ type: data.type });
    }
  };
  return {
    send: (event) => channel.postMessage(event),
    close: () => channel.close(),
  };
}
