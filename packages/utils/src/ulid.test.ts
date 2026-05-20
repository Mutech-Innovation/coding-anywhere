import { describe, expect, it } from 'vitest';

import { newId } from './ulid.js';

describe('newId', () => {
  it('returns a 26-char id', () => {
    const id = newId();
    expect(id).toHaveLength(26);
  });

  it('is monotonic-ish across consecutive calls at the same ms', () => {
    const t = Date.now();
    const a = newId(t);
    const b = newId(t);
    expect(a.slice(0, 10)).toEqual(b.slice(0, 10));
  });

  it('uses Crockford base32 alphabet', () => {
    const id = newId();
    expect(id).toMatch(/^[0-9A-HJKMNP-TV-Z]+$/);
  });
});
