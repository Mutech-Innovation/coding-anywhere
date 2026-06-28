import {
  type DiffChange,
  DiffMethod,
  type DiffVersionsResponse,
  type Envelope,
  type RelayWSClient,
  makeRequest,
} from '@coding-anywhere/protocol';
import { newId } from '@coding-anywhere/utils';
import { create } from 'zustand';

import { useConnection } from './connection';

interface DiffStore {
  isRepo: boolean;
  changes: DiffChange[];
  activePath: string | null;
  refresh(): Promise<void>;
  setActive(path: string | null): void;
  versions(path: string): Promise<DiffVersionsResponse>;
}

const pending = new Map<
  string,
  { resolve: (env: Envelope) => void; reject: (err: Error) => void }
>();

let handlerClient: RelayWSClient | null = null;
let unsubscribeHandler: (() => void) | null = null;
function ensureHandler() {
  const client = useConnection.getState().client;
  if (!client) return;
  if (handlerClient === client) return;
  if (unsubscribeHandler) unsubscribeHandler();
  rejectPending(new Error('git connection changed'));
  unsubscribeHandler = client.handle('git', (env) => {
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

export const useDiff = create<DiffStore>((set) => ({
  isRepo: true,
  changes: [],
  activePath: null,

  async refresh() {
    ensureHandler();
    const env = await sendRequest(DiffMethod.ListChanges);
    const payload = env.payload as { changes?: DiffChange[]; is_repo?: boolean };
    set({
      isRepo: payload.is_repo ?? false,
      changes: payload.changes ?? [],
    });
  },

  setActive(path) {
    set({ activePath: path });
  },

  async versions(path) {
    ensureHandler();
    const env = await sendRequest(DiffMethod.Versions, { path });
    return env.payload as DiffVersionsResponse;
  },
}));

async function sendRequest(method: string, payload: unknown = {}): Promise<Envelope> {
  const client = useConnection.getState().client;
  if (!client) throw new Error('not connected');
  const id = newId();
  const env = makeRequest({ id, channel: 'git', method, payload });
  return new Promise<Envelope>((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`git.${method} timed out`));
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
