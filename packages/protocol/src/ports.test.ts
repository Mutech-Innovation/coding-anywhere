import { describe, expect, it } from 'vitest';

import { proxyURL } from './ports.js';

describe('proxyURL', () => {
  it('maps wss:// to https:// so the browser can open the tab', () => {
    const url = proxyURL({
      relayURL: 'wss://relay.example.com:2096',
      port: 8800,
      token: 'tok',
    });
    expect(url).toBe('https://relay.example.com:2096/v1/proxy/8800/?token=tok');
  });

  it('maps ws:// to http:// for LAN/dev relays', () => {
    const url = proxyURL({ relayURL: 'ws://localhost:8443', port: 5173, token: 't' });
    expect(url).toBe('http://localhost:8443/v1/proxy/5173/?token=t');
  });

  it('leaves an https:// relay URL untouched', () => {
    const url = proxyURL({ relayURL: 'https://relay.example.com', port: 3000, token: 't' });
    expect(url).toBe('https://relay.example.com/v1/proxy/3000/?token=t');
  });

  it('normalizes the path and encodes the token', () => {
    const url = proxyURL({
      relayURL: 'wss://relay.example.com/',
      port: 3000,
      path: 'app',
      token: 'a b/c',
    });
    expect(url).toBe('https://relay.example.com/v1/proxy/3000/app?token=a%20b%2Fc');
  });
});
