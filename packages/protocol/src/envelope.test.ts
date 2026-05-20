import { describe, expect, it } from 'vitest';

import { Envelope, makeEvent, makeRequest, makeResponse } from './envelope.js';

describe('Envelope', () => {
  it('round-trips a request through zod', () => {
    const env = makeRequest({
      id: 'abc',
      channel: 'sessions',
      method: 'create',
      payload: { workspace: '/Users/me/app' },
    });
    const parsed = Envelope.parse(env);
    expect(parsed.type).toBe('req');
    expect(parsed.method).toBe('create');
  });

  it('builds responses correlated to a request id', () => {
    const res = makeResponse({ id: 'abc', channel: 'sessions', payload: { ok: true } });
    expect(res.type).toBe('res');
    expect(res.id).toBe('abc');
  });

  it('rejects an envelope with an unknown channel', () => {
    expect(() =>
      Envelope.parse({ id: 'x', type: 'req', channel: 'mystery', payload: {} }),
    ).toThrow();
  });

  it('builds events without a correlation', () => {
    const ev = makeEvent({ id: 'evt-1', channel: 'meta', method: 'pong', payload: { x: 1 } });
    expect(ev.type).toBe('event');
  });
});
