// Shared markdown core for chat transcripts: parse agent output into
// renderer-agnostic blocks, and segment still-streaming text at block
// boundaries so memoized per-platform renderers only re-render the live
// tail per chunk (the OOM-prevention invariant — see AGENTS.md).

export { parseMarkdownBlocks, type MarkdownBlock, type TableAlign } from './blocks.js';
export { nextStreamingSegments, streamingMarkdownSegments } from './segment.js';
