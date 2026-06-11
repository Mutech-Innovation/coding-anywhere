// Shared markdown core for chat transcripts: parse agent output into a
// renderer-agnostic mdast tree (CommonMark + GFM), and segment still-
// streaming text at top-level block boundaries so memoized per-platform
// renderers only re-render the live tail per chunk (the OOM-prevention
// invariant — see AGENTS.md). This package is pure TS — no React, no DOM —
// so the web renderer today and the React Native renderer later walk the
// exact same tree.

export { parseMarkdown } from './parse.js';
export { nextStreamingSegments, streamingMarkdownSegments } from './segment.js';
// Consumers type their renderers against mdast through this package so they
// don't each need a direct @types/mdast dependency.
export type * from 'mdast';
