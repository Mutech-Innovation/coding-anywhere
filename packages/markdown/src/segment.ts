// Streaming segmentation: split a still-streaming markdown text at block
// boundaries so every segment except the live last one keeps its exact string
// value from chunk to chunk — a memoized renderer then re-renders only the
// live tail per chunk instead of the whole answer (O(answer²) per turn was
// enough to OOM the tab twice). Moved verbatim from
// apps/web/src/components/ChatPanel.tsx.

import { parseMarkdownLines } from './blocks.js';

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
 * whole text would: cuts happen only at block start lines, every
 * STREAMING_SEGMENT_BLOCKS blocks and before the final (still-streaming)
 * block. Returns [text] when there's nothing to split.
 */
export function streamingMarkdownSegments(text: string): string[] {
  const norm = text.replace(/\r\n/g, '\n');
  const lines = norm.split('\n');
  const { starts } = parseMarkdownLines(lines);
  if (starts.length <= 1) return [norm];
  const cutLines: number[] = [];
  for (let b = STREAMING_SEGMENT_BLOCKS; b < starts.length; b += STREAMING_SEGMENT_BLOCKS) {
    cutLines.push(starts[b]!);
  }
  const lastStart = starts[starts.length - 1]!;
  if ((cutLines[cutLines.length - 1] ?? -1) < lastStart) cutLines.push(lastStart);
  // Map line indices to char offsets in `norm` ('\n' joins the lines back).
  const offsets: number[] = new Array<number>(lines.length);
  let acc = 0;
  for (let k = 0; k < lines.length; k++) {
    offsets[k] = acc;
    acc += (lines[k]?.length ?? 0) + 1;
  }
  const segments: string[] = [];
  let prev = 0;
  for (const cutLine of cutLines) {
    segments.push(norm.slice(prev, offsets[cutLine]!));
    prev = offsets[cutLine]!;
  }
  segments.push(norm.slice(prev));
  return segments;
}
