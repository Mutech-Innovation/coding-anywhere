// Minimal ULID-like id generator: 26-char Crockford base32, lexicographically
// sortable by time. We avoid the `ulid` npm package to keep deps minimal in
// shared code and to make this trivially portable to React Native.

const ENCODING = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'; // Crockford base32, 32 chars
const TIME_LEN = 10;
const RANDOM_LEN = 16;

function encodeTime(now: number): string {
  let n = now;
  let out = '';
  for (let i = TIME_LEN - 1; i >= 0; i--) {
    const mod = n % 32;
    out = ENCODING[mod]! + out;
    n = (n - mod) / 32;
  }
  return out;
}

interface CryptoLike {
  getRandomValues(buf: Uint8Array): Uint8Array;
}

function encodeRandom(): string {
  // Use crypto if available (browser, Node 19+, RN with polyfill), fallback to Math.random.
  const cryptoObj = (globalThis as { crypto?: CryptoLike }).crypto;
  const bytes = new Uint8Array(RANDOM_LEN);
  if (cryptoObj && typeof cryptoObj.getRandomValues === 'function') {
    cryptoObj.getRandomValues(bytes);
  } else {
    for (let i = 0; i < RANDOM_LEN; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  let out = '';
  for (let i = 0; i < RANDOM_LEN; i++) {
    out += ENCODING[bytes[i]! % 32];
  }
  return out;
}

/** Generate a new 26-char monotonic-ish id usable for envelope correlation. */
export function newId(now: number = Date.now()): string {
  return encodeTime(now) + encodeRandom();
}
