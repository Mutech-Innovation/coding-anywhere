// Pins the flight recorder's crash-detection semantics: a session that ends
// without pagehide/stop leaves an "unclean" record behind for the next load
// to surface; clean ends do not.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { lastCrashRecord, startFlightRecorder, takeCrashRecord } from './diagnostics.js';

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
    // The live record is consumed…
    expect(localStorage.getItem(KEY)).toBeNull();
    // …but the crash stays retrievable until the next crash replaces it:
    // reopening the page without DevTools must not destroy the evidence.
    expect(lastCrashRecord('test')?.snapshots).toEqual(crashed!.snapshots);
  });

  it('later loads still point at the durable crash record', () => {
    startFlightRecorder({ page: 'test', sample: () => ({}), intervalMs: 100 });
    vi.advanceTimersByTime(150);
    // Crash, then a DevTools-less reload consumes the live record.
    const stop2 = startFlightRecorder({ page: 'test', sample: () => ({}), intervalMs: 100 });
    stop2();
    // A LATER load (no fresh crash) still tells the user how to copy it.
    const warn = vi.mocked(console.warn);
    warn.mockClear();
    const stop3 = startFlightRecorder({ page: 'test', sample: () => ({}), intervalMs: 100 });
    expect(warn).toHaveBeenCalled();
    expect(String(warn.mock.calls[0]?.[0])).toContain('历史崩溃记录');
    expect(lastCrashRecord('test')).not.toBeNull();
    stop3();
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

  it('stays quiet while idle, heartbeats, and speaks up on activity', () => {
    let chars = 0;
    const stop = startFlightRecorder({
      page: 'test',
      sample: () => ({ chars }),
      intervalMs: 100,
      heartbeatMs: 1000,
    });
    const log = vi.mocked(console.log);
    log.mockClear(); // drop the "flight recorder started" line

    vi.advanceTimersByTime(100); // first tick → one baseline line
    expect(log).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(500); // idle ticks → silent (snapshots still persist)
    expect(log).toHaveBeenCalledTimes(1);
    expect(storedRecord()?.snapshots).toHaveLength(6);

    vi.advanceTimersByTime(500); // crosses heartbeatMs → exactly one heartbeat
    expect(log).toHaveBeenCalledTimes(2);

    chars = 123; // gauge moved → immediate line on the next tick
    vi.advanceTimersByTime(100);
    expect(log).toHaveBeenCalledTimes(3);
    expect(String(log.mock.calls[2]?.[0])).toContain('chars=123');
    stop();
  });

  it('records window errors into the persisted record', () => {
    const stop = startFlightRecorder({ page: 'test', sample: () => ({}), intervalMs: 100 });
    window.dispatchEvent(new ErrorEvent('error', { message: 'boom' }));
    const raw = JSON.parse(localStorage.getItem(KEY)!) as { errors: { msg: string }[] };
    expect(raw.errors[0]?.msg).toBe('error: boom');
    stop();
  });
});
