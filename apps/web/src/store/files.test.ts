// Pins the lightweight files-refresh behaviour: the daemon has no file-watch,
// so the tree must (a) cache listings under the resolved workspace id and
// (b) re-fetch on demand. refresh() drops the cache and bumps refreshToken so
// open directories revalidate; list() always hits the daemon so re-expanding a
// folder reflects on-disk changes instead of replaying a stale cache.
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import type { Envelope, FilesEntry } from '@coding-anywhere/protocol';

import { useConnection } from './connection.js';
import { useFiles } from './files.js';

// Entries the fake daemon returns for the next files.list — mutable so a test
// can simulate the workspace changing between two list() calls.
let nextEntries: FilesEntry[] = [];

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
      if (env.method === 'list') {
        const payload = env.payload as { path: string };
        filesDispatch({
          id: env.id,
          type: 'res',
          channel: 'files',
          payload: { workspace_id: 'default', path: payload.path, entries: nextEntries },
        });
      } else if (env.method === 'list_workspace') {
        filesDispatch({
          id: env.id,
          type: 'res',
          channel: 'files',
          payload: { workspaces: [{ id: 'default', name: 'ws', root: '/root' }] },
        });
      }
    });
  },
};

function entry(name: string): FilesEntry {
  return { name, path: name, kind: 'file', size: 1, mod_time: 0 };
}

beforeEach(() => {
  nextEntries = [entry('a.txt')];
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
});
