// Streaming segmentation: split a still-streaming markdown text at block
// boundaries so every segment except the live last one keeps its exact string
// value from chunk to chunk — a memoized renderer then re-renders only the
// live tail per chunk instead of the whole answer (O(answer²) per turn was
// enough to OOM the tab twice).

import { parseMarkdown } from './parse.js';

/**
 * Incremental wrapper around streamingMarkdownSegments: when `text` merely
 * extends the previously segmented text (the streaming-append case), keep the
 * settled segments as-is and re-split only the previous live segment plus the
 * appended chunk. Returns the normalized text alongside the segments (which
 * partition it) so callers can cache both without re-joining.
 */
export function nextStreamingSegments(
  prevText: string,
  prevSegments: string[],
  text: string,
): { text: string; segments: string[] } {
  const norm = text.replace(/\r\n/g, '\n');
  if (prevSegments.length > 0 && norm.startsWith(prevText)) {
    if (norm.length === prevText.length) return { text: norm, segments: prevSegments };
    const live = prevSegments[prevSegments.length - 1]!;
    const tail = streamingMarkdownSegments(norm.slice(prevText.length - live.length));
    return { text: norm, segments: [...prevSegments.slice(0, -1), ...tail] };
  }
  return { text: norm, segments: streamingMarkdownSegments(norm) };
}

/** Cap re-render cost of the segment a new block lands in. */
const STREAMING_SEGMENT_BLOCKS = 32;

/**
 * Split markdown into segments that each parse to exactly the blocks the
 * whole text would: cuts happen only at top-level mdast node start offsets,
 * every STREAMING_SEGMENT_BLOCKS blocks and before the final (still
 * streaming) block. Top-level starts are safe cut points by construction —
 * a fence/list/blockquote is a single node, so a cut can never land inside
 * one. Returns [text] when there's nothing to split.
 */
export function streamingMarkdownSegments(text: string): string[] {
  const norm = text.replace(/\r\n/g, '\n');
  const starts: number[] = [];
  for (const node of parseMarkdown(norm).children) {
    const offset = node.position?.start.offset;
    // fromMarkdown always provides positions; skip (never cut at) any node
    // where that somehow stopped holding rather than corrupt a slice.
    if (typeof offset === 'number') starts.push(offset);
  }
  if (starts.length <= 1) return [norm];
  const cuts: number[] = [];
  for (let b = STREAMING_SEGMENT_BLOCKS; b < starts.length; b += STREAMING_SEGMENT_BLOCKS) {
    cuts.push(starts[b]!);
  }
  const lastStart = starts[starts.length - 1]!;
  if ((cuts[cuts.length - 1] ?? -1) < lastStart) cuts.push(lastStart);
  const segments: string[] = [];
  let prev = 0;
  for (const cut of cuts) {
    segments.push(norm.slice(prev, cut));
    prev = cut;
  }
  segments.push(norm.slice(prev));
  return segments;
}
