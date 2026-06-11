// Pins the streaming-markdown segmentation that keeps long streamed answers
// from re-parsing/re-rendering the whole bubble per chunk (the guest-page OOM:
// per-chunk cost grew with answer length, O(answer²) per turn). The DOM-level
// streamed-vs-one-shot render test lives with the web renderer in
// apps/web/src/components/markdown-stream.test.tsx.
import { describe, expect, it } from 'vitest';

import { nextStreamingSegments, streamingMarkdownSegments } from './segment.js';

const CORPUS = [
  'Intro paragraph with **bold** and `code`.\n',
  '\n',
  '# Heading\n',
  'Another paragraph\nwith a continuation line.\n',
  '\n',
  '- list item one\n',
  '- list item two\n',
  '1. ordered item\n',
  '```go\n',
  'func main() {\n',
  '\n', // blank line INSIDE the fence — must not become a cut point
  '\tfmt.Println("hi")\n',
  '}\n',
  '```\n',
  '\n',
  // Fence whose info string defeats the old strict opener regex — used to
  // spin parseMarkdownLines forever (renderer OOM, "Render process gone").
  '```c++\n',
  'int main() { return 0; }\n',
  '```\n',
  '| a | b |\n',
  '|---|---|\n',
  '| 1 | 2 |\n',
  'closing words\n',
].join('');

describe('streamingMarkdownSegments', () => {
  it('segments concatenate back to the normalized text', () => {
    const segments = streamingMarkdownSegments(CORPUS);
    expect(segments.join('')).toBe(CORPUS);
  });

  it('never cuts inside a fenced code block', () => {
    // Build enough blocks to force intermediate cuts, then a fence with
    // blank lines inside.
    const blocks = Array.from({ length: 40 }, (_, i) => `para ${i}\n\n`).join('');
    const text = `${blocks}\`\`\`txt\nline\n\nline after blank\n`; // fence still open
    const segments = streamingMarkdownSegments(text);
    expect(segments.join('')).toBe(text);
    // The open fence must be entirely inside the live (last) segment.
    const live = segments[segments.length - 1]!;
    expect(live.startsWith('```txt')).toBe(true);
  });

  it('every prefix split keeps each settled segment parse-equivalent', () => {
    // Stream the corpus in awkward 7-char chunks; at every step the
    // segments must concatenate to the streamed text.
    let text = '';
    for (let i = 0; i < CORPUS.length; i += 7) {
      text = CORPUS.slice(0, i + 7);
      expect(streamingMarkdownSegments(text).join('')).toBe(text);
    }
  });

  it('terminates on fences the strict opener regex used to reject', () => {
    // Each of these spun the parser forever pre-fix (the loop never
    // consumed the ``` line): infinite blocks → renderer OOM in seconds.
    const nasty = [
      '```c++\nint main() {}\n```\n',
      '``` js\nx\n```\n', // space before the language
      '```ts title=app.ts\nexport {};\n```\n',
      '```c#\nclass A {}\n', // still-open fence, streaming
      'para\n```\n', // bare/stray fence line at EOF
    ];
    for (const text of nasty) {
      expect(streamingMarkdownSegments(text).join('')).toBe(text);
    }
  });
});

describe('nextStreamingSegments', () => {
  it('settled segments stay identical while chunks stream in', () => {
    // The whole point of the split: every segment except the live last one
    // must keep its exact string value from chunk to chunk, so the memo'd
    // renderer component bails out for them.
    let prev: { text: string; segments: string[] } = { text: '', segments: [] };
    for (let i = 0; i < CORPUS.length; i += 5) {
      const text = CORPUS.slice(0, i + 5);
      const next = nextStreamingSegments(prev.text, prev.segments, text);
      expect(next.segments.join('')).toBe(text);
      const settled = prev.segments.slice(0, -1);
      expect(next.segments.slice(0, settled.length)).toEqual(settled);
      prev = next;
    }
  });

  it('falls back to a full split when the text does not extend the cache', () => {
    const a = nextStreamingSegments('', [], CORPUS);
    const b = nextStreamingSegments(a.text, a.segments, 'totally different');
    expect(b.segments.join('')).toBe('totally different');
  });
});
