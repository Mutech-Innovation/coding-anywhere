// Pins the lightweight files-refresh behaviour: the daemon has no file-watch,
// so the tree must (a) cache listings under the resolved workspace id and
// (b) re-fetch on demand. refresh() drops the cache and bumps refreshToken so
// open directories revalidate; list() always hits the daemon so re-expanding a
// folder reflects on-disk changes instead of replaying a stale cache.
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import {
  base64ToBytes,
  bytesToBase64,
  type Envelope,
  type FilesEntry,
} from '@coding-anywhere/protocol';

import { useConnection } from './connection.js';
import { useFiles } from './files.js';

// Entries the fake daemon returns for the next files.list — mutable so a test
// can simulate the workspace changing between two list() calls.
let nextEntries: FilesEntry[] = [];

// In-memory "filesystem" the fake daemon serves for write/read/delete/move so
// tests can exercise the chunked upload/download paths end to end.
let fakeFiles: Map<string, Uint8Array> = new Map();

let filesDispatch: ((env: Envelope) => void) | null = null;

const fakeClient = {
  handle(channel: string, handler: (env: Envelope) => void) {
    if (channel === 'files') filesDispatch = handler;
    return () => {
      if (filesDispatch === handler) filesDispatch = null;
    };
  },
  send(env: Envelope) {
    // Answer on the next microtask, mirroring a relay round-trip.
    void Promise.resolve().then(() => {
      if (!filesDispatch) return;
      const reply = (payload: unknown) =>
        filesDispatch?.({ id: env.id, type: 'res', channel: 'files', payload });
      if (env.method === 'list') {
        const payload = env.payload as { path: string };
        reply({ workspace_id: 'default', path: payload.path, entries: nextEntries });
      } else if (env.method === 'list_workspace') {
        reply({ workspaces: [{ id: 'default', name: 'ws', root: '/root' }] });
      } else if (env.method === 'write') {
        const p = env.payload as {
          path: string;
          offset?: number;
          data: string;
          truncate?: boolean;
        };
        const chunk = base64ToBytes(p.data ?? '');
        const offset = p.offset ?? 0;
        const prev = p.truncate ? new Uint8Array(0) : (fakeFiles.get(p.path) ?? new Uint8Array(0));
        const next = new Uint8Array(Math.max(prev.length, offset + chunk.length));
        next.set(prev, 0);
        next.set(chunk, offset);
        fakeFiles.set(p.path, next);
        reply({ workspace_id: 'default', path: p.path, size: next.length });
      } else if (env.method === 'read') {
        const p = env.payload as { path: string; offset?: number; length?: number };
        const full = fakeFiles.get(p.path) ?? new Uint8Array(0);
        const start = p.offset ?? 0;
        const end = p.length ? Math.min(start + p.length, full.length) : full.length;
        const slice = full.subarray(start, end);
        reply({
          workspace_id: 'default',
          path: p.path,
          size: full.length,
          truncated: end < full.length,
          data: bytesToBase64(slice),
        });
      } else if (env.method === 'delete') {
        const p = env.payload as { path: string };
        fakeFiles.delete(p.path);
        reply({ workspace_id: 'default', path: p.path });
      } else if (env.method === 'move') {
        const p = env.payload as { from: string; to: string };
        const data = fakeFiles.get(p.from);
        if (data) {
          fakeFiles.delete(p.from);
          fakeFiles.set(p.to, data);
        }
        reply({ workspace_id: 'default', from: p.from, to: p.to });
      }
    });
  },
};

function entry(name: string): FilesEntry {
  return { name, path: name, kind: 'file', size: 1, mod_time: 0 };
}

// jsdom's File doesn't implement arrayBuffer(); real browsers do. Stub a
// File-like object so uploadFile() can read bytes in the test environment.
function fileLike(name: string, bytes: Uint8Array): File {
  return {
    name,
    arrayBuffer: async () =>
      bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
  } as unknown as File;
}

beforeEach(() => {
  nextEntries = [entry('a.txt')];
  fakeFiles = new Map();
  useConnection.setState({ client: fakeClient as never, status: 'connected' });
  useFiles.setState({ workspaceId: 'default', listings: {}, refreshToken: 0 });
});

afterEach(() => {
  useConnection.setState({ client: null, status: 'disconnected' });
});

describe('useFiles', () => {
  it('caches a listing under the resolved workspace id', async () => {
    const entries = await useFiles.getState().list('');
    expect(entries.map((e) => e.name)).toEqual(['a.txt']);
    // The tree reads s.listings[`${workspaceId}|${path}`]; the key must match.
    expect(useFiles.getState().listings['default|']).toEqual(entries);
  });

  it('refresh() drops cached listings and bumps the refresh token', () => {
    useFiles.setState({ listings: { 'default|': [entry('stale.txt')] }, refreshToken: 2 });
    useFiles.getState().refresh();
    expect(useFiles.getState().listings).toEqual({});
    expect(useFiles.getState().refreshToken).toBe(3);
  });

  it('re-fetches updated entries on a subsequent list (revalidation)', async () => {
    const first = await useFiles.getState().list('');
    expect(first.map((e) => e.name)).toEqual(['a.txt']);

    // The directory changed on disk; a fresh list() must reflect it rather
    // than short-circuiting on the cached value.
    nextEntries = [entry('a.txt'), entry('b.txt')];
    const second = await useFiles.getState().list('');
    expect(second.map((e) => e.name)).toEqual(['a.txt', 'b.txt']);
    expect(useFiles.getState().listings['default|']).toEqual(second);
  });

  it('caps file previews to one transport-safe chunk', async () => {
    fakeFiles.set('large.txt', new Uint8Array(600 * 1024).fill(97));
    const { text, res } = await useFiles.getState().readFile('large.txt');
    expect(text).toHaveLength(256 * 1024);
    expect(res.truncated).toBe(true);
    expect(res.size).toBe(600 * 1024);
  });

  it('uploads into a directory and rounds back the exact bytes (chunked)', async () => {
    // Larger than one 256 KiB chunk so the upload/download loops actually run
    // multiple times instead of a single shot.
    const payload = new Uint8Array(600 * 1024);
    for (let i = 0; i < payload.length; i++) payload[i] = i % 256;
    const file = fileLike('big.bin', payload);

    await useFiles.getState().uploadFile('sub', file);
    // Stored under the joined path (dir + name), not the bare filename.
    expect(fakeFiles.has('sub/big.bin')).toBe(true);

    const { bytes } = await useFiles.getState().downloadFile('sub/big.bin');
    expect(bytes.length).toBe(payload.length);
    expect(Array.from(bytes.slice(0, 4))).toEqual([0, 1, 2, 3]);
    expect(bytes[payload.length - 1]).toBe((payload.length - 1) % 256);
  });

  it('uploads an empty file as a single zero-length write', async () => {
    await useFiles.getState().uploadFile('', fileLike('empty.txt', new Uint8Array(0)));
    expect(fakeFiles.get('empty.txt')?.length).toBe(0);
  });

  it('moveEntry renames a stored file and deleteEntry removes it', async () => {
    fakeFiles.set('a.txt', new Uint8Array([1, 2, 3]));
    await useFiles.getState().moveEntry('a.txt', 'sub/a.txt');
    expect(fakeFiles.has('a.txt')).toBe(false);
    expect(fakeFiles.get('sub/a.txt')).toEqual(new Uint8Array([1, 2, 3]));

    await useFiles.getState().deleteEntry('sub/a.txt');
    expect(fakeFiles.has('sub/a.txt')).toBe(false);
  });
});
