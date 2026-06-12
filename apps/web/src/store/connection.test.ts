// Pins guestRelayURL: share links must not embed a loopback relay URL when
// the app itself is being viewed from another machine — the guest's browser
// would dial its OWN localhost (instant refusal at best; a hang on whatever
// the guest runs on that port at worst).
import { describe, expect, it } from 'vitest';

import { guestRelayURL } from './connection.js';

describe('guestRelayURL', () => {
  it('substitutes the page host when the relay host is loopback', () => {
    expect(guestRelayURL('http://localhost:8443', '192.168.20.66')).toBe(
      'http://192.168.20.66:8443',
    );
    expect(guestRelayURL('http://127.0.0.1:8443', 'dev-box.local')).toBe(
      'http://dev-box.local:8443',
    );
  });

  it('keeps scheme and port', () => {
    expect(guestRelayURL('https://localhost:9999', 'example.com')).toBe('https://example.com:9999');
  });

  it('leaves non-loopback relay URLs alone', () => {
    expect(guestRelayURL('https://relay.example.com', '192.168.20.66')).toBe(
      'https://relay.example.com',
    );
  });

  it('leaves the URL alone when the page itself is viewed via loopback', () => {
    // Nothing to substitute with — the whole link is machine-local anyway.
    expect(guestRelayURL('http://localhost:8443', 'localhost')).toBe('http://localhost:8443');
    expect(guestRelayURL('http://localhost:8443', '127.0.0.1')).toBe('http://localhost:8443');
  });

  it('passes malformed relay URLs through unchanged', () => {
    expect(guestRelayURL('not a url', '192.168.20.66')).toBe('not a url');
  });
});
