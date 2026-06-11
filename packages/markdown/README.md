# @coding-anywhere/markdown

Shared chat-markdown core for every client: the web app today, the v3 React
Native app next. Pure TS — no React, no DOM, runs on Hermes. Parsing, the
streaming segmentation and the OOM-prevention invariant live **here**, once;
each platform only writes a renderer that walks the same tree. Do not fork
any of this per platform.

## API

- `parseMarkdown(text): Root` — CommonMark + GFM (tables, strikethrough,
  task lists, autolinks) → [mdast](https://github.com/syntax-tree/mdast) via
  micromark. Input is CRLF-normalized first; node `position.*.offset`s refer
  to the **normalized** text.
- `streamingMarkdownSegments(text): string[]` — splits text at top-level
  mdast node start offsets, every 32 blocks and before the final
  (still-streaming) block. Guarantees: segments concatenate back to the
  normalized text, and a cut can never land inside a fence/list/blockquote —
  those are single nodes, so this holds by construction.
- `nextStreamingSegments(prevText, prevSegments, text)` — incremental wrapper
  for the streaming-append case: only the live tail is re-split, every other
  segment keeps its **exact string value** from chunk to chunk. That value
  stability is what lets a memoized renderer skip them.
- mdast node types are re-exported (`Root`, `RootContent`,
  `PhrasingContent`, `List`, `Table`, …) so consumers don't need their own
  `@types/mdast` dependency.

## The invariant every renderer must keep

Two OOM'd-tab incidents (see AGENTS.md "Streaming render perf invariant"):
a streaming chunk must do O(live tail) work, never O(answer) or
O(transcript). The renderer-side half of the contract:

1. A body component that parses with `parseMarkdown` inside `useMemo` and is
   wrapped in `memo` — its `text` prop is a segment string.
2. A streaming wrapper that caches the previous split in a ref, calls
   `nextStreamingSegments`, and renders one memoized body per segment.

`apps/web/src/components/Markdown.tsx` (`MarkdownBody` +
`StreamingMarkdownBody`) is the reference implementation; the RN version
should differ **only** in the component mapping.

## Blueprint: the React Native renderer (v3 mobile)

Port `apps/web/src/components/Markdown.tsx` 1:1 when `apps/mobile` lands:

- `MarkdownBody`: `memo` + `parseMarkdown` in `useMemo`, walk
  `root.children` with a block-node switch.
- `StreamingMarkdownBody`: copy verbatim — it is platform-neutral React
  (only the `MarkdownBody` import differs). If the copies ever drift, lift
  it into this package parameterized by a body component instead.
- Block mapping: paragraph, heading, code, blockquote, list (nested via
  `listItem.children`, GFM task items via `listItem.checked`), table
  (`node.align` per column), thematicBreak, html → **literal text**,
  definition → renders nothing.
- Inline mapping: text, strong, emphasis, delete, inlineCode, break, link,
  image, linkReference/imageReference (resolve against `definition` nodes in
  the same tree — see `collectDefinitions` in the web renderer),
  footnoteReference, html → **literal text**.
- Security posture, identical to web (the guest share page feeds untrusted
  input through this): never render raw HTML, never fetch images (render the
  alt text as a link instead), only `http(s):`/`mailto:` schemes become
  tappable links.
- Mirror the two web test pins: a fidelity/security suite
  (`markdown-render.test.tsx`) and a streamed-vs-one-shot suite
  (`markdown-stream.test.tsx` — streamed chunks must render the same text as
  a one-shot render).

## Tests

- `src/segment.test.ts` — segmentation invariants (segments concatenate
  back, settled-segment stability, never cut inside a fence) plus the
  ` ```c++ ` infinite-loop regression that OOM'd the old hand-rolled
  parser.
- `src/parse.test.ts` — what the mdast core buys over the old parser:
  nested lists, blockquotes, GFM extras, arbitrary fence info strings.

## Known limits (by design)

- Reference definitions (`[x][1]` … `[1]: url`) resolve within one parsed
  text only — during streaming that means within one segment. Rare in agent
  output; the old parser didn't support reference links at all.
- `html` nodes render as literal text everywhere. That is policy, not a gap.
