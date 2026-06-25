import {
  type Envelope,
  type FilesEntry,
  type FilesListResponse,
  type FilesReadResponse,
  FilesMethod,
  type RelayWSClient,
  type FilesWorkspaceInfo,
  base64ToBytes,
  makeRequest,
} from '@coding-anywhere/protocol';
import { newId } from '@coding-anywhere/utils';
import { create } from 'zustand';

import { useConnection } from './connection.js';

interface FilesStore {
  workspaces: FilesWorkspaceInfo[];
  workspaceId: string | null;
  // Listing cache: workspaceId|path → entries
  listings: Record<string, FilesEntry[]>;
  // Currently selected file (path within workspace).
  activePath: string | null;
  // Bumped by refresh() so open directories re-fetch (the daemon has no
  // file-watch yet, so this is how stale listings get invalidated).
  refreshToken: number;

  loadWorkspaces(): Promise<void>;
  setWorkspace(id: string): void;
  list(path: string, opts?: { includeHidden?: boolean }): Promise<FilesEntry[]>;
  readFile(path: string): Promise<{ text: string | null; res: FilesReadResponse }>;
  setActivePath(path: string | null): void;
  // Drop every cached listing and signal open directories to re-fetch.
  refresh(): void;
}

const pending = new Map<
  string,
  { resolve: (env: Envelope) => void; reject: (err: Error) => void }
>();

const TEXT_DECODER = new TextDecoder();

export const useFiles = create<FilesStore>((set, get) => ({
  workspaces: [],
  workspaceId: null,
  listings: {},
  activePath: null,
  refreshToken: 0,

  async loadWorkspaces() {
    // We're not registering a global handler for files (handler in store
    // doesn't strictly need it because we don't subscribe to events on this
    // channel today), but we need a single place to dispatch responses.
    ensureHandlerInstalled();
    const env = await sendRequest(FilesMethod.ListWorkspace);
    const payload = env.payload as { workspaces?: FilesWorkspaceInfo[] };
    const ws = payload.workspaces ?? [];
    set((s) => ({
      workspaces: ws,
      workspaceId: s.workspaceId ?? ws[0]?.id ?? null,
    }));
  },

  setWorkspace(id) {
    set({ workspaceId: id, activePath: null, listings: {} });
  },

  async list(path, opts = {}) {
    ensureHandlerInstalled();
    const wid = get().workspaceId ?? '';
    const env = await sendRequest(FilesMethod.List, {
      workspace_id: wid,
      path,
      include_hidden: opts.includeHidden ?? false,
    });
    const payload = env.payload as FilesListResponse;
    const cacheKey = `${wid}|${path}`;
    set((s) => ({
      listings: { ...s.listings, [cacheKey]: payload.entries ?? [] },
    }));
    return payload.entries ?? [];
  },

  async readFile(path) {
    ensureHandlerInstalled();
    const env = await sendRequest(FilesMethod.Read, {
      workspace_id: get().workspaceId ?? '',
      path,
    });
    const res = env.payload as FilesReadResponse;
    const text = res.is_binary ? null : TEXT_DECODER.decode(base64ToBytes(res.data ?? ''));
    return { text, res };
  },

  setActivePath(path) {
    set({ activePath: path });
  },

  refresh() {
    set((s) => ({ listings: {}, refreshToken: s.refreshToken + 1 }));
  },
}));

let handlerClient: RelayWSClient | null = null;
let unsubscribeHandler: (() => void) | null = null;
function ensureHandlerInstalled() {
  const client = useConnection.getState().client;
  if (!client) return;
  if (handlerClient === client) return;
  if (unsubscribeHandler) unsubscribeHandler();
  rejectPending(new Error('files connection changed'));
  unsubscribeHandler = client.handle('files', (env) => {
    if (env.type !== 'res') return;
    const p = pending.get(env.id);
    if (!p) return;
    pending.delete(env.id);
    if (env.error) p.reject(new Error(formatFilesError(env.error.code, env.error.message)));
    else p.resolve(env);
  });
  handlerClient = client;
}

function formatFilesError(code: string, message: string): string {
  if (code === 'no_daemon') {
    return 'No daemon is connected to this browser account. Sign out and pair this browser with the active daemon again.';
  }
  return `${code}: ${message}`;
}

function rejectPending(err: Error) {
  for (const p of pending.values()) p.reject(err);
  pending.clear();
}

async function sendRequest(method: string, payload: unknown = {}): Promise<Envelope> {
  const client = useConnection.getState().client;
  if (!client) throw new Error('not connected');
  const id = newId();
  const env = makeRequest({ id, channel: 'files', method, payload });
  return new Promise<Envelope>((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`files.${method} timed out`));
    }, 15_000);
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
