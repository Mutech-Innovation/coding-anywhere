// In-page "flight recorder" for hunting tab crashes (OOM) we can't reproduce
// locally — e.g. the guest page dying on a colleague's Win11 Chrome. Every
// tick it logs a compact one-liner to the console AND persists a rolling
// window of snapshots to localStorage. A crashed tab never runs pagehide, so
// the record stays marked "unclean"; the next page load detects that and
// prints the previous session's final snapshots + errors, which would
// otherwise be lost with the crashed console.
//
// Console lines are prefixed `[ca-fr]` so they're easy to filter. The
// recorder also doubles as a build marker: if a user's console shows no
// [ca-fr] lines at all, they are running a stale bundle.

import { useChat } from './store/chat.js';

interface FlightSnapshot {
  /** epoch ms */
  t: number;
  /** Chrome-only (performance.memory); absent elsewhere. */
  heapUsedMB?: number;
  heapTotalMB?: number;
  /** document element count. */
  dom: number;
  /** Long-task time observed since the previous snapshot. */
  longMs: number;
  longMax: number;
  longCount: number;
  /** Caller-provided gauges (chat stats etc.). */
  custom: Record<string, number>;
}

interface FlightRecord {
  page: string;
  startedAt: number;
  endedCleanly: boolean;
  ua: string;
  deviceMemoryGB?: number;
  heapLimitMB?: number;
  snapshots: FlightSnapshot[];
  errors: { t: number; msg: string }[];
}

const SNAPSHOT_CAP = 90; // ~3 min at the default 2s interval
const ERROR_CAP = 20;

const storageKey = (page: string) => `coding-anywhere/flight/${page}`;

interface ChromeMemory {
  usedJSHeapSize: number;
  totalJSHeapSize: number;
  jsHeapSizeLimit: number;
}

function chromeMemory(): ChromeMemory | undefined {
  return (performance as { memory?: ChromeMemory }).memory;
}

const toMB = (bytes: number) => Math.round(bytes / 1024 / 1024);

function readRecord(page: string): FlightRecord | null {
  try {
    const raw = localStorage.getItem(storageKey(page));
    return raw ? (JSON.parse(raw) as FlightRecord) : null;
  } catch {
    return null;
  }
}

function writeRecord(record: FlightRecord): void {
  try {
    localStorage.setItem(storageKey(record.page), JSON.stringify(record));
  } catch {
    // Private mode / quota — recorder degrades to console-only.
  }
}

/**
 * If the previous session on this page died without a clean pagehide (tab
 * crash, OOM, kill), return its record so it can be surfaced. Clears the
 * stored record either way. Exported for tests.
 */
export function takeCrashRecord(page: string): FlightRecord | null {
  const prev = readRecord(page);
  try {
    localStorage.removeItem(storageKey(page));
  } catch {
    // ignore
  }
  return prev && !prev.endedCleanly && prev.snapshots.length > 0 ? prev : null;
}

/** Chat-store gauges for the snapshot's `custom` field. */
export function sampleChatStats(): Record<string, number> {
  const sessions = Object.values(useChat.getState().sessions);
  let msgs = 0;
  let chars = 0;
  let maxMsg = 0;
  let tools = 0;
  for (const s of sessions) {
    msgs += s.messages.length;
    tools += Object.keys(s.toolCalls).length;
    for (const m of s.messages) {
      chars += m.text.length;
      if (m.text.length > maxMsg) maxMsg = m.text.length;
    }
  }
  return { sessions: sessions.length, msgs, chars, maxMsg, tools };
}

export interface FlightRecorderOptions {
  /** Storage namespace + console tag, e.g. "guest" or "app". */
  page: string;
  /** Extra gauges sampled every tick. Defaults to sampleChatStats. */
  sample?: () => Record<string, number>;
  intervalMs?: number;
  /** Max console silence while idle; snapshots still persist every tick. */
  heartbeatMs?: number;
}

// Console-noise thresholds: a tick prints only when something moved —
// any custom gauge changed, a long task fired, an error landed, or
// heap/DOM drifted past these deltas since the LAST PRINTED line.
const LOG_HEAP_DELTA_MB = 16;
const LOG_DOM_DELTA = 200;

/**
 * Start the recorder; returns a stop function (usable as a useEffect
 * cleanup). Also prints the previous session's record if it crashed.
 */
export function startFlightRecorder({
  page,
  sample = sampleChatStats,
  intervalMs = 2000,
  heartbeatMs = 30_000,
}: FlightRecorderOptions): () => void {
  const crashed = takeCrashRecord(page);
  if (crashed) {
    const last = crashed.snapshots[crashed.snapshots.length - 1];
    console.warn(
      `[ca-fr] 检测到上次 ${page} 页面未正常退出(疑似崩溃)。` +
        `最后快照: ${last ? new Date(last.t).toLocaleString() : '?'}` +
        `${last?.heapUsedMB != null ? `, heap ${last.heapUsedMB}MB` : ''}` +
        `, dom ${last?.dom ?? '?'}。完整记录(请复制给开发者):`,
    );
    console.warn(JSON.stringify(crashed));
  }

  const mem = chromeMemory();
  const record: FlightRecord = {
    page,
    startedAt: Date.now(),
    endedCleanly: false,
    ua: navigator.userAgent,
    deviceMemoryGB: (navigator as { deviceMemory?: number }).deviceMemory,
    heapLimitMB: mem ? toMB(mem.jsHeapSizeLimit) : undefined,
    snapshots: [],
    errors: [],
  };
  console.log(
    `[ca-fr] flight recorder started (${page}), ua=${record.ua}` +
      `${record.deviceMemoryGB ? `, deviceMemory=${record.deviceMemoryGB}GB` : ''}` +
      `${record.heapLimitMB ? `, heapLimit=${record.heapLimitMB}MB` : ''}`,
  );
  writeRecord(record);

  // Long tasks (main-thread stalls >50ms) accumulated between ticks.
  let longMs = 0;
  let longMax = 0;
  let longCount = 0;
  let observer: PerformanceObserver | null = null;
  if (typeof PerformanceObserver !== 'undefined') {
    try {
      observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          longMs += entry.duration;
          longCount += 1;
          if (entry.duration > longMax) longMax = entry.duration;
        }
      });
      observer.observe({ entryTypes: ['longtask'] });
    } catch {
      observer = null; // entry type unsupported
    }
  }

  let errorsTotal = 0;
  const pushError = (msg: string) => {
    errorsTotal += 1;
    record.errors.push({ t: Date.now(), msg: msg.slice(0, 300) });
    if (record.errors.length > ERROR_CAP) record.errors.shift();
    writeRecord(record);
  };
  const onError = (e: ErrorEvent) => pushError(`error: ${e.message}`);
  const onRejection = (e: PromiseRejectionEvent) =>
    pushError(`unhandledrejection: ${String(e.reason)}`);
  window.addEventListener('error', onError);
  window.addEventListener('unhandledrejection', onRejection);

  // Snapshots persist EVERY tick (the black box wants fine resolution right
  // before a crash), but the console only speaks when something moved since
  // the last printed line, or every heartbeatMs while idle. Long-task stats
  // accumulate separately for printing so quiet ticks aren't lost.
  let logged: {
    t: number;
    heap: number | null;
    dom: number;
    custom: Record<string, number>;
    errorsTotal: number;
  } | null = null;
  let printLongMs = 0;
  let printLongMax = 0;
  let printLongCount = 0;
  const tick = () => {
    const m = chromeMemory();
    const custom = sample();
    const snap: FlightSnapshot = {
      t: Date.now(),
      heapUsedMB: m ? toMB(m.usedJSHeapSize) : undefined,
      heapTotalMB: m ? toMB(m.totalJSHeapSize) : undefined,
      dom: document.getElementsByTagName('*').length,
      longMs: Math.round(longMs),
      longMax: Math.round(longMax),
      longCount,
      custom,
    };
    printLongMs += longMs;
    printLongCount += longCount;
    if (longMax > printLongMax) printLongMax = longMax;
    longMs = longMax = longCount = 0;
    record.snapshots.push(snap);
    if (record.snapshots.length > SNAPSHOT_CAP) record.snapshots.shift();
    writeRecord(record);

    const heap = snap.heapUsedMB ?? null;
    const interesting =
      logged === null ||
      printLongCount > 0 ||
      errorsTotal > logged.errorsTotal ||
      Object.entries(custom).some(([k, v]) => v !== (logged!.custom[k] ?? 0)) ||
      (heap !== null &&
        logged.heap !== null &&
        Math.abs(heap - logged.heap) >= LOG_HEAP_DELTA_MB) ||
      Math.abs(snap.dom - logged.dom) >= LOG_DOM_DELTA;
    if (!interesting && snap.t - logged!.t < heartbeatMs) return;

    const dt = Math.max((snap.t - (logged?.t ?? record.startedAt)) / 1000, 0.001);
    const parts: string[] = [];
    if (heap !== null) parts.push(`heap=${heap}/${snap.heapTotalMB}MB`);
    parts.push(`dom=${snap.dom}`);
    if (printLongCount > 0)
      parts.push(
        `long=${Math.round(printLongMs)}ms(max${Math.round(printLongMax)},n${printLongCount})`,
      );
    for (const [k, v] of Object.entries(custom)) {
      const d = v - (logged?.custom[k] ?? 0);
      parts.push(d !== 0 ? `${k}=${v}(+${Math.round(d / dt)}/s)` : `${k}=${v}`);
    }
    if (errorsTotal > (logged?.errorsTotal ?? 0)) parts.push(`errors=${errorsTotal}`);
    console.log(`[ca-fr] ${parts.join(' ')}`);
    logged = { t: snap.t, heap, dom: snap.dom, custom, errorsTotal };
    printLongMs = printLongMax = printLongCount = 0;
  };
  // NOTE: Chrome throttles background-tab timers to ~1/min; sparse snapshots
  // while hidden are fine — the last one before a crash is what matters.
  const timer = setInterval(tick, intervalMs);

  const markClean = () => {
    record.endedCleanly = true;
    writeRecord(record);
  };
  // pagehide fires on navigation/close but NOT on a crash — exactly the
  // signal we want for "ended cleanly".
  window.addEventListener('pagehide', markClean);

  // Console convenience: copy(window.__caFlightRecorder.dump())
  (window as { __caFlightRecorder?: { dump: () => string } }).__caFlightRecorder = {
    dump: () => JSON.stringify(record),
  };

  return () => {
    clearInterval(timer);
    observer?.disconnect();
    window.removeEventListener('error', onError);
    window.removeEventListener('unhandledrejection', onRejection);
    window.removeEventListener('pagehide', markClean);
    markClean();
  };
}

/** Debug flag for pages where the recorder is opt-in (the owner app). */
export function isDebugEnabled(): boolean {
  try {
    if (new URLSearchParams(window.location.search).has('debug')) return true;
    return localStorage.getItem('coding-anywhere/debug') === '1';
  } catch {
    return false;
  }
}
