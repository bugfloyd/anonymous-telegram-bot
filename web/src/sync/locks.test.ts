import { describe, expect, it } from 'vitest';
import { openChannel, withLock, type SyncEvent } from './locks';

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

describe('withLock', () => {
  it('never lets two holders of the same lock interleave', async () => {
    const order: string[] = [];
    const task = (id: string) =>
      withLock('test-interleave', async () => {
        order.push(`${id}:start`);
        await sleep(10);
        order.push(`${id}:end`);
        return id;
      });
    const results = await Promise.all([task('a'), task('b'), task('c')]);
    expect(results).toEqual(['a', 'b', 'c']);
    for (let i = 0; i < order.length; i += 2) {
      expect(order[i]!.split(':')[0]).toBe(order[i + 1]!.split(':')[0]);
    }
  });

  it('lets different locks run at the same time', async () => {
    let concurrent = 0;
    let peak = 0;
    const task = (name: string) =>
      withLock(name, async () => {
        concurrent++;
        peak = Math.max(peak, concurrent);
        await sleep(10);
        concurrent--;
      });
    await Promise.all([task('x'), task('y')]);
    expect(peak).toBe(2);
  });

  it('runs directly where Web Locks are missing', async () => {
    expect(await withLock('none', async () => 42, undefined)).toBe(42);
  });

  it('releases the lock when the task fails', async () => {
    await expect(withLock('fails', async () => { throw new Error('boom'); })).rejects.toThrow('boom');
    expect(await withLock('fails', async () => 'ok')).toBe('ok');
  });
});

describe('openChannel', () => {
  it('delivers known events between copies and ignores anything else', async () => {
    const received: SyncEvent[] = [];
    const a = openChannel(() => {});
    const b = openChannel((e) => received.push(e));
    expect(a && b).toBeTruthy();
    a!.send({ type: 'keys-changed' });
    new BroadcastChannel('incognitochat:sync').postMessage({ type: 'evil', payload: 'x' });
    await sleep(20);
    expect(received).toEqual([{ type: 'keys-changed' }]);
    a!.close();
    b!.close();
  });
});
