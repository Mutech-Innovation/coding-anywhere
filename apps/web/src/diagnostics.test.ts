// Pins the flight recorder's crash-detection semantics: a session that ends
// without pagehide/stop leaves an "unclean" record behind for the next load
// to surface; clean ends do not.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { startFlightRecorder, takeCrashRecord } from './diagnostics.js';

const KEY = 'coding-anywhere/flight/test';

function storedRecord(): { endedCleanly: boolean; snapshots: unknown[] } | null {
  const raw = localStorage.getItem(KEY);
  return raw ? (JSON.parse(raw) as { endedCleanly: boolean; snapshots: unknown[] }) : null;
}

describe('flight recorder', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    localStorage.clear();
    vi.spyOn(console, 'log').mockImplementation(() => {});
    vi.spyOn(console, 'warn').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('persists snapshots every tick and marks the record clean on stop', () => {
    const stop = startFlightRecorder({ page: 'test', sample: () => ({ n: 1 }), intervalMs: 100 });
    vi.advanceTimersByTime(350);
    expect(storedRecord()?.snapshots).toHaveLength(3);
    expect(storedRecord()?.endedCleanly).toBe(false);
    stop();
    expect(storedRecord()?.endedCleanly).toBe(true);
    // A clean record is not reported as a crash on the next load.
    expect(takeCrashRecord('test')).toBeNull();
  });

  it('marks the record clean on pagehide', () => {
    const stop = startFlightRecorder({ page: 'test', sample: () => ({}), intervalMs: 100 });
    vi.advanceTimersByTime(150);
    window.dispatchEvent(new Event('pagehide'));
    expect(storedRecord()?.endedCleanly).toBe(true);
    stop();
  });

  it('surfaces the previous record when the page never ended cleanly', () => {
    const stop = startFlightRecorder({ page: 'test', sample: () => ({ n: 42 }), intervalMs: 100 });
    vi.advanceTimersByTime(250);
    // Simulate a crash: no stop(), no pagehide — just a fresh "next load".
    stop; // (intentionally not called)
    const crashed = takeCrashRecord('test');
    expect(crashed).not.toBeNull();
    expect(crashed!.snapshots.length).toBeGreaterThan(0);
    // takeCrashRecord clears storage so the report fires once.
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  it('startFlightRecorder warns with the crashed record on the next load', () => {
    startFlightRecorder({ page: 'test', sample: () => ({}), intervalMs: 100 });
    vi.advanceTimersByTime(150);
    // "Crash": start a second recorder for the same page without stopping.
    const warn = vi.mocked(console.warn);
    warn.mockClear();
    const stop2 = startFlightRecorder({ page: 'test', sample: () => ({}), intervalMs: 100 });
    expect(warn).toHaveBeenCalled();
    expect(String(warn.mock.calls[0]?.[0])).toContain('未正常退出');
    stop2();
  });

  it('records window errors into the persisted record', () => {
    const stop = startFlightRecorder({ page: 'test', sample: () => ({}), intervalMs: 100 });
    window.dispatchEvent(new ErrorEvent('error', { message: 'boom' }));
    const raw = JSON.parse(localStorage.getItem(KEY)!) as { errors: { msg: string }[] };
    expect(raw.errors[0]?.msg).toBe('error: boom');
    stop();
  });
});
