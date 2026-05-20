import { toHttpURL } from '@coding-anywhere/protocol';
import { create } from 'zustand';

import { useConnection } from './connection.js';

export type PermissionState = 'default' | 'granted' | 'denied';
export type SubscriptionState = 'unsupported' | 'idle' | 'subscribed' | 'subscribing' | 'error';

interface NotificationsStore {
  permission: PermissionState;
  state: SubscriptionState;
  error: string | null;

  init(): Promise<void>;
  subscribe(): Promise<void>;
  unsubscribe(): Promise<void>;
  sendTest(): Promise<void>;
}

const SW_PATH = '/service-worker.js';

function isSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    'Notification' in window
  );
}

export const useNotifications = create<NotificationsStore>((set) => ({
  permission: 'default',
  state: 'idle',
  error: null,

  async init() {
    if (!isSupported()) {
      set({ state: 'unsupported' });
      return;
    }
    try {
      await navigator.serviceWorker.register(SW_PATH);
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.getSubscription();
      set({
        permission: Notification.permission as PermissionState,
        state: sub ? 'subscribed' : 'idle',
        error: null,
      });
    } catch (e) {
      set({ state: 'error', error: (e as Error).message });
    }
  },

  async subscribe() {
    if (!isSupported()) return;
    const credentials = useConnection.getState().credentials;
    if (!credentials) {
      set({ state: 'error', error: 'not paired' });
      return;
    }
    set({ state: 'subscribing', error: null });
    try {
      const permission = await Notification.requestPermission();
      set({ permission: permission as PermissionState });
      if (permission !== 'granted') {
        set({ state: 'idle', error: 'permission denied' });
        return;
      }
      const reg = await navigator.serviceWorker.ready;
      const vapidKeyResp = await fetch(
        toHttpURL(credentials.relay_url) + '/v1/push/vapid_public_key',
      );
      if (!vapidKeyResp.ok) throw new Error('vapid_unavailable');
      const { public_key: vapid } = (await vapidKeyResp.json()) as { public_key: string };
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        // The DOM type for applicationServerKey is BufferSource; TypeScript's
        // strict mode rejects Uint8Array because its buffer might be a
        // SharedArrayBuffer. Convert via a fresh ArrayBuffer.
        applicationServerKey: urlBase64ToBuffer(vapid),
      });
      const json = sub.toJSON();
      const res = await fetch(toHttpURL(credentials.relay_url) + '/v1/push/subscribe', {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          authorization: `Bearer ${credentials.bundle.access_token}`,
        },
        body: JSON.stringify({
          ...json,
          user_agent:
            typeof navigator !== 'undefined' ? navigator.userAgent : 'coding-anywhere/web',
        }),
      });
      if (!res.ok) throw new Error('subscribe http ' + res.status);
      set({ state: 'subscribed', error: null });
    } catch (e) {
      set({ state: 'error', error: (e as Error).message });
    }
  },

  async unsubscribe() {
    if (!isSupported()) return;
    const credentials = useConnection.getState().credentials;
    try {
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.getSubscription();
      if (sub) {
        const endpoint = sub.endpoint;
        await sub.unsubscribe();
        if (credentials) {
          await fetch(toHttpURL(credentials.relay_url) + '/v1/push/unsubscribe', {
            method: 'POST',
            headers: {
              'content-type': 'application/json',
              authorization: `Bearer ${credentials.bundle.access_token}`,
            },
            body: JSON.stringify({ endpoint }),
          });
        }
      }
      set({ state: 'idle' });
    } catch (e) {
      set({ state: 'error', error: (e as Error).message });
    }
  },

  async sendTest() {
    const credentials = useConnection.getState().credentials;
    if (!credentials) return;
    await fetch(toHttpURL(credentials.relay_url) + '/v1/push/test', {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        authorization: `Bearer ${credentials.bundle.access_token}`,
      },
      body: JSON.stringify({
        title: 'Coding Anywhere',
        body: 'Test notification from the relay',
      }),
    });
  },
}));

function urlBase64ToBuffer(base64String: string): ArrayBuffer {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  const buf = new ArrayBuffer(raw.length);
  const view = new Uint8Array(buf);
  for (let i = 0; i < raw.length; i++) view[i] = raw.charCodeAt(i);
  return buf;
}
