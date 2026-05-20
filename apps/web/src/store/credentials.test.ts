import { afterEach, describe, expect, it } from 'vitest';

import {
  loadCredentials,
  normalizeCredentials,
  saveCredentials,
  updateAccessToken,
  type StoredCredentials,
} from './credentials.js';

const base: StoredCredentials = {
  relay_url: 'https://relay.example.com',
  bundle: {
    account_id: 'acc',
    device_id: 'dev',
    device_kind: 'client',
    device_label: 'Browser',
    access_token: 'access-1',
    access_token_expires_in: 3600,
    refresh_token: 'refresh-1',
    refresh_token_expires_in: 86400,
    relay_protocol_version: 1,
  },
};

describe('credentials storage', () => {
  afterEach(() => window.localStorage.clear());

  it('persists absolute expiry times when saving credentials', () => {
    const saved = saveCredentials(base, 1_000);

    expect(saved.access_token_expires_at).toBe(3_601_000);
    expect(saved.refresh_token_expires_at).toBe(86_401_000);
    expect(loadCredentials(2_000)).toEqual(saved);
  });

  it('marks old stored credentials without access expiry as needing refresh', () => {
    window.localStorage.setItem('coding-anywhere/credentials', JSON.stringify(base));

    const loaded = loadCredentials(2_000);

    expect(loaded?.access_token_expires_at).toBe(0);
    expect(loaded?.refresh_token_expires_at).toBe(86_402_000);
  });

  it('updates the access token without changing the refresh token metadata', () => {
    const normalized = normalizeCredentials(base, 1_000);

    const updated = updateAccessToken(normalized, 'access-2', 1800, 5_000);

    expect(updated.bundle.access_token).toBe('access-2');
    expect(updated.access_token_expires_at).toBe(1_805_000);
    expect(updated.bundle.refresh_token).toBe('refresh-1');
    expect(updated.refresh_token_expires_at).toBe(normalized.refresh_token_expires_at);
  });
});
