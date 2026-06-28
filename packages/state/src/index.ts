// @coding-anywhere/state — cross-platform zustand stores shared by the web and
// mobile apps. The pure channel stores (chat, git diff, ports) live here once;
// each platform keeps its own connection/credentials store (storage + auth differ)
// and registers a client getter via setClientProvider so these stores can reach
// the live RelayWSClient.
export { getClient, setClientProvider } from './client.js';
export * from './chat.js';
export * from './diff.js';
export * from './ports.js';
export * from './transcript.js';
