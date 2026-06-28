// Transcript grouping now lives in @coding-anywhere/state (shared with mobile).
export {
  groupTranscript,
  isActiveToolCall,
  hasMeaningfulPayload,
  TOOL_GROUP_THRESHOLD,
} from '@coding-anywhere/state';
export type { TranscriptEntry, TranscriptRenderItem } from '@coding-anywhere/state';
