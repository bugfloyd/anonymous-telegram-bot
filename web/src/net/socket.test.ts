import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPushSocket, HEARTBEAT_MS } from './socket';

class FakeSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 3;
  static all: FakeSocket[] = [];
  readyState = FakeSocket.CONNECTING;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  constructor(
    readonly url: string,
    readonly protocols: string[],
  ) {
    FakeSocket.all.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = FakeSocket.CLOSED;
    this.onclose?.();
  }
  // Test controls.
  open() {
    this.readyState = FakeSocket.OPEN;
    this.onopen?.();
  }
  drop() {
    this.readyState = FakeSocket.CLOSED;
    this.onclose?.();
  }
  receive(data: string) {
    this.onmessage?.({ data });
  }
}

const flush = () => vi.advanceTimersByTimeAsync(0);

describe('push socket', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeSocket.all = [];
  });
  afterEach(() => vi.useRealTimers());

  function make() {
    let hints = 0;
    let tokens = 0;
    const socket = createPushSocket({
      url: 'wss://ws.example/live',
      token: async () => `v1.token.${++tokens}`,
      onHint: () => hints++,
      WebSocket: FakeSocket as unknown as typeof WebSocket,
    });
    return { socket, hints: () => hints };
  }

  it('connects with the token as a subprotocol, syncs on open and on hints', async () => {
    const { socket, hints } = make();
    socket.start();
    await flush();
    const ws = FakeSocket.all[0]!;
    expect(ws.url).toBe('wss://ws.example/live');
    expect(ws.protocols).toEqual(['v1', 'v1.token.1']);
    expect(socket.connected).toBe(false);
    ws.open();
    expect(socket.connected).toBe(true);
    expect(hints()).toBe(1);
    ws.receive('{"t":"sync"}');
    ws.receive('not json');
    ws.receive('{"t":"other"}');
    expect(hints()).toBe(2);
    // Heartbeats keep it from closing as idle.
    await vi.advanceTimersByTimeAsync(HEARTBEAT_MS * 2);
    expect(ws.sent).toEqual(['{"action":"ping"}', '{"action":"ping"}']);
  });

  it('reconnects with backoff, and closes while hidden', async () => {
    const { socket } = make();
    socket.start();
    await flush();
    FakeSocket.all[0]!.drop();
    await vi.advanceTimersByTimeAsync(400);
    expect(FakeSocket.all).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(FakeSocket.all).toHaveLength(2);
    // A fresh token each time.
    expect(FakeSocket.all[1]!.protocols[1]).toBe('v1.token.2');
    FakeSocket.all[1]!.drop();
    // The second retry waits longer.
    await vi.advanceTimersByTimeAsync(900);
    expect(FakeSocket.all).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(3_000);
    expect(FakeSocket.all).toHaveLength(3);
    FakeSocket.all[2]!.open();

    // Hidden: closed, and not reopened; shown: open again at once.
    socket.setVisible(false);
    expect(FakeSocket.all[2]!.readyState).toBe(FakeSocket.CLOSED);
    expect(socket.connected).toBe(false);
    await vi.advanceTimersByTimeAsync(120_000);
    expect(FakeSocket.all).toHaveLength(3);
    socket.setVisible(true);
    await flush();
    expect(FakeSocket.all).toHaveLength(4);

    socket.stop();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(FakeSocket.all).toHaveLength(4);
  });

  it("doesn't connect while hidden", async () => {
    const { socket } = make();
    socket.setVisible(false);
    socket.start();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(FakeSocket.all).toHaveLength(0);
  });
});
