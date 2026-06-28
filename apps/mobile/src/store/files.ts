// Mobile files store — RN counterpart of apps/web/src/store/files.ts, trimmed to
// the v1 read surface: browse workspaces/dirs, read a file, download (chunked).
// Write ops (upload/delete/move) are deferred. Same request/response plumbing as
// the web store; mobile-local for now.
import {
  base64ToBytes,
  type Envelope,
  type FilesEntry,
  type FilesListResponse,
  type FilesReadResponse,
  FilesMethod,
  type FilesWorkspaceInfo,
  makeRequest,
  type RelayWSClient,
} from '@coding-anywhere/protocol';
import { newId } from '@coding-anywhere/utils';
import { create } from 'zustand';

import { useConnection } from './connection';

const CHUNK_BYTES = 256 * 1024;

interface FilesStore {
  workspaces: FilesWorkspaceInfo[];
  workspaceId: string | null;
  listings: Record<string, FilesEntry[]>;
  activePath: string | null;
  refreshToken: number;

  loadWorkspaces(): Promise<void>;
  setWorkspace(id: string): void;
  list(path: string, opts?: { includeHidden?: boolean }): Promise<FilesEntry[]>;
  readFile(path: string): Promise<{ text: string | null; res: FilesReadResponse }>;
  setActivePath(path: string | null): void;
  downloadFile(path: string): Promise<{ bytes: Uint8Array; contentType?: string }>;
  refresh(): void;
}

const pending = new Map<
  string,
  { resolve: (env: Envelope) => void; reject: (err: Error) => void }
>();

export const useFiles = create<FilesStore>((set, get) => ({
  workspaces: [],
  workspaceId: null,
  listings: {},
  activePath: null,
  refreshToken: 0,

  async loadWorkspaces() {
    ensureHandlerInstalled();
    const env = await sendRequest(FilesMethod.ListWorkspace);
    const payload = env.payload as { workspaces?: FilesWorkspaceInfo[] };
    const ws = payload.workspaces ?? [];
    set((s) => ({ workspaces: ws, workspaceId: s.workspaceId ?? ws[0]?.id ?? null }));
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
    set((s) => ({ listings: { ...s.listings, [cacheKey]: payload.entries ?? [] } }));
    return payload.entries ?? [];
  },

  async readFile(path) {
    ensureHandlerInstalled();
    const env = await sendRequest(FilesMethod.Read, {
      workspace_id: get().workspaceId ?? '',
      path,
    });
    const res = env.payload as FilesReadResponse;
    const text = res.is_binary ? null : decodeUtf8(base64ToBytes(res.data ?? ''));
    return { text, res };
  },

  setActivePath(path) {
    set({ activePath: path });
  },

  async downloadFile(path) {
    ensureHandlerInstalled();
    const wid = get().workspaceId ?? '';
    const chunks: Uint8Array[] = [];
    let offset = 0;
    let total = Number.POSITIVE_INFINITY;
    let contentType: string | undefined;
    while (offset < total) {
      const env = await sendRequest(FilesMethod.Read, {
        workspace_id: wid,
        path,
        offset,
        length: CHUNK_BYTES,
        max_bytes: CHUNK_BYTES,
        allow_binary: true,
      });
      const res = env.payload as FilesReadResponse;
      total = res.size;
      contentType = res.content_type;
      const part = base64ToBytes(res.data ?? '');
      if (part.length === 0) break;
      chunks.push(part);
      offset += part.length;
    }
    const out = new Uint8Array(chunks.reduce((n, c) => n + c.length, 0));
    let at = 0;
    for (const c of chunks) {
      out.set(c, at);
      at += c.length;
    }
    return { bytes: out, contentType };
  },

  refresh() {
    set((s) => ({ listings: {}, refreshToken: s.refreshToken + 1 }));
  },
}));

export function joinPath(dir: string, name: string): string {
  const d = dir.replace(/\/+$/, '');
  return d ? `${d}/${name}` : name;
}

// Hermes may not ship TextDecoder; use it when present, else a compact UTF-8 fallback.
function decodeUtf8(bytes: Uint8Array): string {
  if (typeof TextDecoder !== 'undefined') {
    try {
      return new TextDecoder().decode(bytes);
    } catch {
      /* fall through */
    }
  }
  let out = '';
  for (let i = 0; i < bytes.length; ) {
    const b0 = bytes[i++]!;
    let cp: number;
    if (b0 < 0x80) cp = b0;
    else if (b0 < 0xe0) cp = ((b0 & 0x1f) << 6) | (bytes[i++]! & 0x3f);
    else if (b0 < 0xf0)
      cp = ((b0 & 0x0f) << 12) | ((bytes[i++]! & 0x3f) << 6) | (bytes[i++]! & 0x3f);
    else {
      cp =
        ((b0 & 0x07) << 18) |
        ((bytes[i++]! & 0x3f) << 12) |
        ((bytes[i++]! & 0x3f) << 6) |
        (bytes[i++]! & 0x3f);
    }
    if (cp > 0xffff) {
      cp -= 0x10000;
      out += String.fromCharCode(0xd800 + (cp >> 10), 0xdc00 + (cp & 0x3ff));
    } else {
      out += String.fromCharCode(cp);
    }
  }
  return out;
}

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
    if (env.error) p.reject(new Error(`${env.error.code}: ${env.error.message}`));
    else p.resolve(env);
  });
  handlerClient = client;
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
