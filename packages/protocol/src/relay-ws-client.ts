// Browser-side counterpart of internal/relayclient (Go).
//
// Maintains a long-lived WebSocket to the relay with capped exponential
// backoff, an outbound queue, and per-channel inbound handlers. Designed to
// run unchanged in browsers and React Native (uses standard WebSocket API).

import { type Channel, type Envelope, makeRequest } from './envelope.js';
import { toWsURL } from './pair.js';

export type ConnectionState = 'connecting' | 'connected' | 'disconnected';

export type Handler = (env: Envelope) => void;

export interface RelayWSClientOptions {
  relayURL: string;
  accessToken: string;
  /** Default: 30000 (30s). Set to 0 to disable. */
  pingIntervalMs?: number;
  /** Default: 30000 (30s). */
  maxBackoffMs?: number;
  /** Generates ids for pings. Defaults to a millisecond timestamp. */
  newId?: () => string;
}

interface InternalState {
  ws: WebSocket | null;
  attempt: number;
  shouldRun: boolean;
  pingTimer: ReturnType<typeof setInterval> | null;
}

const DEFAULT_PING_MS = 30_000;
const DEFAULT_MAX_BACKOFF_MS = 30_000;

export class RelayWSClient {
  private readonly opts: Required<RelayWSClientOptions>;
  private readonly handlers = new Map<Channel, Handler>();
  private readonly stateListeners = new Set<(s: ConnectionState) => void>();
  private state: ConnectionState = 'disconnected';
  private internal: InternalState = {
    ws: null,
    attempt: 0,
    shouldRun: false,
    pingTimer: null,
  };
  private outbound: Envelope[] = [];

  constructor(opts: RelayWSClientOptions) {
    this.opts = {
      pingIntervalMs: opts.pingIntervalMs ?? DEFAULT_PING_MS,
      maxBackoffMs: opts.maxBackoffMs ?? DEFAULT_MAX_BACKOFF_MS,
      newId: opts.newId ?? (() => `ping-${Date.now()}`),
      ...opts,
    };
  }

  /** Register a handler for a channel; returns an unsubscribe function. */
  handle(channel: Channel, handler: Handler): () => void {
    this.handlers.set(channel, handler);
    return () => {
      if (this.handlers.get(channel) === handler) this.handlers.delete(channel);
    };
  }

  /** Subscribe to connection-state changes. Fires immediately with current state. */
  onState(listener: (s: ConnectionState) => void): () => void {
    this.stateListeners.add(listener);
    listener(this.state);
    return () => {
      this.stateListeners.delete(listener);
    };
  }

  getState(): ConnectionState {
    return this.state;
  }

  send(env: Envelope): void {
    const ws = this.internal.ws;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(env));
      return;
    }
    this.outbound.push(env);
    if (this.outbound.length > 256) this.outbound.shift();
  }

  /** Update the access token; takes effect on the next reconnect. */
  setAccessToken(tok: string): void {
    this.opts.accessToken = tok;
  }

  /** Open the connection. Idempotent. */
  start(): void {
    if (this.internal.shouldRun) return;
    this.internal.shouldRun = true;
    this.connect();
  }

  /** Close the connection and stop reconnecting. Idempotent. */
  stop(): void {
    this.internal.shouldRun = false;
    if (this.internal.pingTimer) {
      clearInterval(this.internal.pingTimer);
      this.internal.pingTimer = null;
    }
    if (this.internal.ws) {
      try {
        this.internal.ws.close();
      } catch {
        // ignore
      }
      this.internal.ws = null;
    }
    this.setState('disconnected');
  }

  private connect(): void {
    if (!this.internal.shouldRun) return;
    const url =
      toWsURL(this.opts.relayURL).replace(/\/$/, '') +
      `/v1/ws/connect?token=${encodeURIComponent(this.opts.accessToken)}`;

    this.setState('connecting');
    const ws = new WebSocket(url);
    this.internal.ws = ws;

    ws.onopen = () => {
      this.internal.attempt = 0;
      this.setState('connected');
      // Flush any queued outbound envelopes.
      const pending = this.outbound;
      this.outbound = [];
      for (const env of pending) ws.send(JSON.stringify(env));
      this.startPings();
    };

    ws.onmessage = (ev) => {
      let env: Envelope;
      try {
        env = JSON.parse(typeof ev.data === 'string' ? ev.data : '') as Envelope;
      } catch {
        return;
      }
      const handler = this.handlers.get(env.channel);
      if (handler) {
        try {
          handler(env);
        } catch {
          // swallow — caller's responsibility to log
        }
      }
    };

    const onClose = () => {
      this.stopPings();
      this.internal.ws = null;
      if (!this.internal.shouldRun) {
        this.setState('disconnected');
        return;
      }
      this.setState('disconnected');
      const delay = backoff(this.internal.attempt, this.opts.maxBackoffMs);
      this.internal.attempt += 1;
      setTimeout(() => this.connect(), delay);
    };

    ws.onclose = onClose;
    ws.onerror = () => {
      // The browser fires onerror immediately followed by onclose; the close
      // handler is enough to drive the reconnect path.
    };
  }

  private startPings(): void {
    if (this.opts.pingIntervalMs <= 0) return;
    this.internal.pingTimer = setInterval(() => {
      this.send(
        makeRequest({
          id: this.opts.newId(),
          channel: 'meta',
          method: 'ping',
          payload: { nonce: this.opts.newId() },
        }),
      );
    }, this.opts.pingIntervalMs);
  }

  private stopPings(): void {
    if (this.internal.pingTimer) {
      clearInterval(this.internal.pingTimer);
      this.internal.pingTimer = null;
    }
  }

  private setState(next: ConnectionState): void {
    if (this.state === next) return;
    this.state = next;
    for (const l of this.stateListeners) l(next);
  }
}

function backoff(attempt: number, max: number): number {
  const base = 250;
  const d = Math.min(base * 2 ** attempt, max);
  const jitter = Math.random() * d * 0.4 - d * 0.2;
  return Math.max(50, d + jitter);
}
