// The parse core: CommonMark + GFM via micromark/mdast, replacing the
// hand-rolled line parser that OOM'd the tab twice (the ```c++ infinite
// loop was exactly the long-tail-edge-case class a spec parser kills off
// wholesale). mdast is the renderer-agnostic block model — each platform
// (web today, React Native later) walks the same tree with its own
// components; this package never touches React or the DOM.

import type { Root } from 'mdast';
import { fromMarkdown } from 'mdast-util-from-markdown';
import { gfmFromMarkdown } from 'mdast-util-gfm';
import { gfm } from 'micromark-extension-gfm';

// Extension configs are stateless; hoist so the per-chunk live-tail parses
// don't re-allocate them.
const EXTENSIONS = { extensions: [gfm()], mdastExtensions: [gfmFromMarkdown()] };

/**
 * Parse markdown into an mdast tree. Input is normalized to '\n' line
 * endings first, so node position offsets always refer to the normalized
 * text — the same normalization streamingMarkdownSegments applies, which is
 * what keeps segment slicing and parsing consistent.
 */
export function parseMarkdown(text: string): Root {
  return fromMarkdown(text.replace(/\r\n/g, '\n'), EXTENSIONS);
}
