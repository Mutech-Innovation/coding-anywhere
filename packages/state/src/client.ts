// Client provider registry. The channel stores (chat/diff/ports) need the live
// RelayWSClient to send requests, but the connection store that owns it is
// platform-specific (web localStorage vs mobile SecureStore, plus web-only
// guest/key features). Rather than couple the shared stores to a particular
// connection store, each platform registers a getter here once at startup:
//
//   import { setClientProvider } from '@coding-anywhere/state';
//   setClientProvider(() => useConnection.getState().client);
//
// The closure is read lazily (per request), so it always returns the current
// client across reconnects.
import type { RelayWSClient } from '@coding-anywhere/protocol';

let provider: () => RelayWSClient | null = () => null;

/** Register how the shared stores obtain the live relay client. */
export function setClientProvider(fn: () => RelayWSClient | null): void {
  provider = fn;
}

/** The current relay client, or null when disconnected / not yet wired. */
export function getClient(): RelayWSClient | null {
  return provider();
}
