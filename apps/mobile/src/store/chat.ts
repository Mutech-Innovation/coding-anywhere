// The chat store now lives in @coding-anywhere/state (shared with web). This file
// re-exports it so existing mobile imports keep working. The connection store wires
// the live client via setClientProvider (see connection.ts).
export { useChat } from '@coding-anywhere/state';
export type {
  ChatRole,
  ChatMessage,
  ChatToolCall,
  ChatPermission,
  SessionState,
} from '@coding-anywhere/state';
