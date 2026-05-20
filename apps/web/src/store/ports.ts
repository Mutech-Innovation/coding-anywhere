import {
  type Envelope,
  type PortsEntry,
  PortsMethod,
  makeRequest,
} from '@coding-anywhere/protocol';
import { newId } from '@coding-anywhere/utils';
import { create } from 'zustand';

import { useConnection } from './connection.js';

interface PortsStore {
  entries: PortsEntry[];
  refresh(): Promise<void>;
}

const pending = new Map<
  string,
  { resolve: (env: Envelope) => void; reject: (err: Error) => void }
>();

let handlerInstalled = false;
function ensureHandler() {
  if (handlerInstalled) return;
  const client = useConnection.getState().client;
  if (!client) return;
  client.handle('ports', (env) => {
    if (env.type !== 'res') return;
    const p = pending.get(env.id);
    if (!p) return;
    pending.delete(env.id);
    if (env.error) p.reject(new Error(`${env.error.code}: ${env.error.message}`));
    else p.resolve(env);
  });
  handlerInstalled = true;
}

export const usePorts = create<PortsStore>((set) => ({
  entries: [],
  async refresh() {
    ensureHandler();
    const env = await sendRequest(PortsMethod.List);
    const payload = env.payload as { entries?: PortsEntry[] } | undefined;
    set({ entries: payload?.entries ?? [] });
  },
}));

async function sendRequest(method: string, payload: unknown = {}): Promise<Envelope> {
  const client = useConnection.getState().client;
  if (!client) throw new Error('not connected');
  const id = newId();
  const env = makeRequest({ id, channel: 'ports', method, payload });
  return new Promise<Envelope>((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`ports.${method} timed out`));
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
