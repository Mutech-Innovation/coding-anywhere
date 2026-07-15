import {
  type Envelope,
  type FilesEntry,
  type FilesListResponse,
  type FilesReadResponse,
  FilesMethod,
  type RelayWSClient,
  type FilesWorkspaceInfo,
  base64ToBytes,
  bytesToBase64,
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
  // Upload a browser File into `dirPath`, chunked under the WS message cap.
  uploadFile(dirPath: string, file: File): Promise<void>;
  // Download the file at `path` as assembled bytes (chunked read).
  downloadFile(path: string): Promise<{ bytes: Uint8Array; contentType?: string }>;
  // Delete a file or directory (recursive on the daemon).
  deleteEntry(path: string): Promise<void>;
  // Move/rename an entry from one path to another.
  moveEntry(from: string, to: string): Promise<void>;
  // Drop every cached listing and signal open directories to re-fetch.
  refresh(): void;
}

// Chunk size for upload/download. 256 KiB of raw bytes is ~341 KiB once
// base64-encoded, leaving ample room for envelope overhead.
const CHUNK_BYTES = 256 * 1024;

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
      length: CHUNK_BYTES,
      max_bytes: CHUNK_BYTES,
    });
    const res = env.payload as FilesReadResponse;
    const text = res.is_binary ? null : TEXT_DECODER.decode(base64ToBytes(res.data ?? ''));
    return { text, res };
  },

  setActivePath(path) {
    set({ activePath: path });
  },

  async uploadFile(dirPath, file) {
    ensureHandlerInstalled();
    const wid = get().workspaceId ?? '';
    const target = joinPath(dirPath, file.name);
    const bytes = new Uint8Array(await file.arrayBuffer());
    // Empty file: a single truncating write creates it at zero length.
    if (bytes.length === 0) {
      await sendRequest(FilesMethod.Write, {
        workspace_id: wid,
        path: target,
        offset: 0,
        data: '',
        truncate: true,
      });
      return;
    }
    for (let offset = 0; offset < bytes.length; offset += CHUNK_BYTES) {
      const chunk = bytes.subarray(offset, Math.min(offset + CHUNK_BYTES, bytes.length));
      await sendRequest(FilesMethod.Write, {
        workspace_id: wid,
        path: target,
        offset,
        data: bytesToBase64(chunk),
        truncate: offset === 0,
      });
    }
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
      if (part.length === 0) break; // nothing more to read (e.g. empty file)
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

  async deleteEntry(path) {
    ensureHandlerInstalled();
    await sendRequest(FilesMethod.Delete, { workspace_id: get().workspaceId ?? '', path });
  },

  async moveEntry(from, to) {
    ensureHandlerInstalled();
    await sendRequest(FilesMethod.Move, { workspace_id: get().workspaceId ?? '', from, to });
  },

  refresh() {
    set((s) => ({ listings: {}, refreshToken: s.refreshToken + 1 }));
  },
}));

// Join a directory path and a child name with a forward slash. The workspace
// root is the empty string, so an empty dir yields just the name.
export function joinPath(dir: string, name: string): string {
  const d = dir.replace(/\/+$/, '');
  return d ? `${d}/${name}` : name;
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
