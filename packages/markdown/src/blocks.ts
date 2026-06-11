// Renderer-agnostic markdown block parsing shared by the web client (and the
// future mobile app). Parsing returns plain data (`MarkdownBlock[]`) — each
// platform maps blocks to its own components. Moved verbatim from
// apps/web/src/components/ChatPanel.tsx.

export type TableAlign = 'left' | 'center' | 'right';

export type MarkdownBlock =
  | { type: 'paragraph'; text: string }
  | { type: 'heading'; level: number; text: string }
  | { type: 'list'; ordered: boolean; items: string[] }
  | { type: 'code'; language?: string; text: string }
  | { type: 'table'; header: string[]; align: (TableAlign | null)[]; rows: string[][] };

export function parseMarkdownBlocks(text: string): MarkdownBlock[] {
  return parseMarkdownLines(text.replace(/\r\n/g, '\n').split('\n')).blocks;
}

// Parses pre-split lines into blocks, also reporting each block's start line
// index (streamingMarkdownSegments cuts at those lines).
export function parseMarkdownLines(lines: string[]): { blocks: MarkdownBlock[]; starts: number[] } {
  const blocks: MarkdownBlock[] = [];
  const starts: number[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? '';
    if (!line.trim()) {
      i++;
      continue;
    }
    // Every branch below consumes at least one line and emits one block.
    starts.push(i);
    // Any ``` line opens a fence (CommonMark: the info string is arbitrary
    // text — ```c++, ``` js, ```ts title=x …). This MUST stay at least as
    // broad as the paragraph loop's /^```/ break below: a ```-prefixed line
    // that no branch consumed used to loop here forever, allocating blocks
    // until the renderer OOM'd ("Render process gone" with a tiny JS heap).
    const fence = line.match(/^```(.*)$/);
    if (fence) {
      const language = fence[1]!.trim().split(/\s+/)[0] || undefined;
      const body: string[] = [];
      i++;
      while (i < lines.length && !(lines[i] ?? '').match(/^```\s*$/)) {
        body.push(lines[i] ?? '');
        i++;
      }
      if (i < lines.length) i++;
      blocks.push({ type: 'code', language, text: body.join('\n') });
      continue;
    }
    const heading = line.match(/^(#{1,4})\s+(.+)$/);
    if (heading) {
      blocks.push({ type: 'heading', level: heading[1]!.length, text: heading[2]! });
      i++;
      continue;
    }
    // GFM table: a header row followed by a |---|---| delimiter row.
    if (line.includes('|') && isTableDelimiterRow(lines[i + 1] ?? '')) {
      const header = parseTableRow(line);
      const align = parseTableRow(lines[i + 1] ?? '').map(tableCellAlign);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length) {
        const candidate = lines[i] ?? '';
        if (!candidate.trim() || !candidate.includes('|')) break;
        rows.push(parseTableRow(candidate));
        i++;
      }
      blocks.push({ type: 'table', header, align, rows });
      continue;
    }
    const unordered = line.match(/^\s*[-*]\s+(.+)$/);
    const ordered = line.match(/^\s*\d+\.\s+(.+)$/);
    if (unordered || ordered) {
      const isOrdered = Boolean(ordered);
      const items: string[] = [];
      while (i < lines.length) {
        const candidate = lines[i] ?? '';
        const match = isOrdered
          ? candidate.match(/^\s*\d+\.\s+(.+)$/)
          : candidate.match(/^\s*[-*]\s+(.+)$/);
        if (!match) break;
        items.push(match[1]!);
        i++;
      }
      blocks.push({ type: 'list', ordered: isOrdered, items });
      continue;
    }
    const paragraph: string[] = [];
    while (i < lines.length) {
      const candidate = lines[i] ?? '';
      if (!candidate.trim()) break;
      if (
        candidate.match(/^```/) ||
        candidate.match(/^(#{1,4})\s+(.+)$/) ||
        candidate.match(/^\s*[-*]\s+(.+)$/) ||
        candidate.match(/^\s*\d+\.\s+(.+)$/) ||
        (candidate.includes('|') && isTableDelimiterRow(lines[i + 1] ?? ''))
      ) {
        break;
      }
      paragraph.push(candidate);
      i++;
    }
    blocks.push({ type: 'paragraph', text: paragraph.join('\n') });
    // Hard progress guarantee: if a future edit makes the paragraph loop
    // break without consuming anything, swallow the line as paragraph text
    // rather than spinning forever (the OOM failure mode above).
    if (i === starts[starts.length - 1]) {
      blocks[blocks.length - 1] = { type: 'paragraph', text: lines[i] ?? '' };
      i++;
    }
  }
  return { blocks, starts };
}

// A GFM delimiter row looks like `|---|:--:|---:|`: only pipes, dashes,
// colons and whitespace, with at least one dash and one column separator.
function isTableDelimiterRow(line: string): boolean {
  const s = line.trim();
  if (!s.includes('-') || !s.includes('|')) return false;
  return /^\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?$/.test(s);
}

// Split a table row into trimmed cells, dropping the leading/trailing
// pipes and honouring backslash-escaped pipes inside a cell.
function parseTableRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith('|')) s = s.slice(1);
  if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1);
  return s.split(/(?<!\\)\|/).map((cell) => cell.replace(/\\\|/g, '|').trim());
}

function tableCellAlign(cell: string): TableAlign | null {
  const left = cell.startsWith(':');
  const right = cell.endsWith(':');
  if (left && right) return 'center';
  if (right) return 'right';
  if (left) return 'left';
  return null;
}
