import {
  type Envelope,
  type RelayWSClient,
  type TerminalDataEvent,
  type TerminalSessionInfo,
  TerminalEvent,
  TerminalMethod,
  base64ToBytes,
  bytesToBase64,
  makeRequest,
} from '@coding-anywhere/protocol';
import { newId } from '@coding-anywhere/utils';
import { create } from 'zustand';

import { useConnection } from './connection.js';

// In-flight request bookkeeping: id → resolver/rejecter pair.
type PendingMap = Map<string, { resolve: (env: Envelope) => void; reject: (err: Error) => void }>;
const pending: PendingMap = new Map();
let handlerClient: RelayWSClient | null = null;

/** Subscribers to live terminal data, keyed by session id. */
type DataListener = (chunk: Uint8Array) => void;
const dataListeners = new Map<string, Set<DataListener>>();

/** Subscribers to terminal.exit/closed, keyed by session id. */
type ExitListener = (exit: { code: number; err?: string }) => void;
const exitListeners = new Map<string, Set<ExitListener>>();

interface TerminalsStore {
  // Session metadata indexed by id.
  sessions: Record<string, TerminalSessionInfo>;
  // Currently focused session id, if any.
  activeId: string | null;

  // Initialised flag — call attach(client) once after pairing.
  attached: boolean;

  attach(client: RelayWSClient): void;
  detach(): void;
  refreshList(): Promise<void>;
  open(opts?: {
    command?: string[];
    cwd?: string;
    cols?: number;
    rows?: number;
  }): Promise<TerminalSessionInfo>;
  setActive(id: string | null): void;
  closeSession(id: string): Promise<void>;

  // Internal — exposed only for the component layer.
  sendInput(id: string, bytes: Uint8Array): Promise<void>;
  resize(id: string, cols: number, rows: number): Promise<void>;
  attachData(id: string): Promise<{ snapshot: Uint8Array; info: TerminalSessionInfo }>;
  onData(id: string, fn: DataListener): () => void;
  onExit(id: string, fn: ExitListener): () => void;
}

export const useTerminals = create<TerminalsStore>((set, get) => ({
  sessions: {},
  activeId: null,
  attached: false,

  attach(client) {
    if (get().attached && handlerClient === client) return;
    handlerClient = client;
    set({ attached: true });

    client.handle('terminal', (env) => {
      // Responses to in-flight requests.
      if (env.type === 'res') {
        const p = pending.get(env.id);
        if (p) {
          pending.delete(env.id);
          if (env.error) {
            p.reject(new Error(`${env.error.code}: ${env.error.message}`));
          } else {
            p.resolve(env);
          }
        }
        return;
      }
      if (env.type !== 'event') return;

      switch (env.method) {
        case TerminalEvent.Opened: {
          const payload = env.payload as { session: TerminalSessionInfo } | undefined;
          if (payload?.session) {
            set((state) => ({
              sessions: { ...state.sessions, [payload.session.id]: payload.session },
            }));
          }
          break;
        }
        case TerminalEvent.Closed: {
          const payload = env.payload as
            | { id: string; exit_code?: number; err?: string }
            | undefined;
          if (!payload?.id) return;
          set((state) => {
            const { [payload.id]: _gone, ...rest } = state.sessions;
            const activeId = state.activeId === payload.id ? null : state.activeId;
            return { sessions: rest, activeId };
          });
          notifyExit(payload.id, { code: payload.exit_code ?? 0, err: payload.err });
          break;
        }
        case TerminalEvent.Data: {
          const payload = env.payload as TerminalDataEvent | undefined;
          if (!payload?.id || !payload.data) return;
          notifyData(payload.id, base64ToBytes(payload.data));
          break;
        }
        case TerminalEvent.Exit: {
          const payload = env.payload as
            | { id: string; exit_code?: number; err?: string }
            | undefined;
          if (!payload?.id) return;
          notifyExit(payload.id, { code: payload.exit_code ?? 0, err: payload.err });
          break;
        }
        default:
        // ignored
      }
    });
  },

  detach() {
    handlerClient = null;
    set({ attached: false, sessions: {}, activeId: null });
    pending.clear();
    dataListeners.clear();
    exitListeners.clear();
  },

  async refreshList() {
    const env = await sendRequest(TerminalMethod.List);
    const sessions = (env.payload as { sessions?: TerminalSessionInfo[] }).sessions ?? [];
    set({
      sessions: Object.fromEntries(sessions.map((s) => [s.id, s])),
    });
  },

  async open(opts = {}) {
    const env = await sendRequest(TerminalMethod.Open, {
      command: opts.command,
      cwd: opts.cwd,
      cols: opts.cols,
      rows: opts.rows,
    });
    const session = (env.payload as { session: TerminalSessionInfo }).session;
    set((state) => ({
      sessions: { ...state.sessions, [session.id]: session },
      activeId: session.id,
    }));
    return session;
  },

  setActive(id) {
    set({ activeId: id });
  },

  async closeSession(id) {
    await sendRequest(TerminalMethod.Close, { id });
    set((state) => {
      const { [id]: _gone, ...rest } = state.sessions;
      const activeId = state.activeId === id ? null : state.activeId;
      return { sessions: rest, activeId };
    });
  },

  async sendInput(id, bytes) {
    await sendRequest(TerminalMethod.Input, { id, data: bytesToBase64(bytes) });
  },

  async resize(id, cols, rows) {
    await sendRequest(TerminalMethod.Resize, { id, cols, rows });
  },

  async attachData(id) {
    const env = await sendRequest(TerminalMethod.Attach, { id });
    const payload = env.payload as { session: TerminalSessionInfo; snapshot: string };
    return { snapshot: base64ToBytes(payload.snapshot ?? ''), info: payload.session };
  },

  onData(id, fn) {
    let set = dataListeners.get(id);
    if (!set) {
      set = new Set();
      dataListeners.set(id, set);
    }
    set.add(fn);
    return () => {
      set?.delete(fn);
    };
  },

  onExit(id, fn) {
    let set = exitListeners.get(id);
    if (!set) {
      set = new Set();
      exitListeners.set(id, set);
    }
    set.add(fn);
    return () => {
      set?.delete(fn);
    };
  },
}));

function notifyData(id: string, bytes: Uint8Array): void {
  const listeners = dataListeners.get(id);
  if (!listeners) return;
  for (const l of listeners) {
    try {
      l(bytes);
    } catch {
      // swallow — caller's responsibility
    }
  }
}

function notifyExit(id: string, exit: { code: number; err?: string }): void {
  const listeners = exitListeners.get(id);
  if (!listeners) return;
  for (const l of listeners) {
    try {
      l(exit);
    } catch {
      // swallow
    }
  }
  // Auto-cleanup once everyone's been notified.
  exitListeners.delete(id);
  dataListeners.delete(id);
}

async function sendRequest(method: string, payload: unknown = {}): Promise<Envelope> {
  const client = useConnection.getState().client;
  if (!client) throw new Error('not connected');
  const id = newId();
  const env = makeRequest({ id, channel: 'terminal', method, payload });
  return new Promise<Envelope>((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`terminal.${method} timed out`));
    }, 10_000);
    pending.set(id, {
      resolve: (e) => {
        clearTimeout(timer);
        resolve(e);
      },
      reject: (err) => {
        clearTimeout(timer);
        reject(err);
      },
    });
    client.send(env);
  });
}
