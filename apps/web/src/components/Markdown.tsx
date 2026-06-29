// Web renderer for chat markdown: walks the shared mdast tree from
// @coding-anywhere/markdown and maps nodes to Mantine components (the future
// mobile app walks the same tree with React Native components). Security
// posture: this renderer NEVER emits raw HTML (html nodes render as literal
// text), never fetches remote images (image nodes render as links), and only
// links http(s)/mailto URLs — chat content is untrusted, the guest share
// page especially.
import { Anchor, Box, Code, Divider, List, Stack, Table, Text } from '@mantine/core';
import {
  nextStreamingSegments,
  parseMarkdown,
  type Definition,
  type List as MdList,
  type ListItem as MdListItem,
  type PhrasingContent,
  type Root,
  type RootContent,
  type Table as MdTable,
} from '@coding-anywhere/markdown';
import { IconSquare, IconSquareCheck } from '@tabler/icons-react';
import { Fragment, memo, type ReactNode, useMemo, useRef } from 'react';

type MarkdownSize = 'xs' | 'sm';

// Threaded through the walk so every node renders with the bubble's size and
// color, and reference-style links can resolve against the tree's
// definitions ([docs][d] … [d]: https://…).
type RenderCtx = { size: MarkdownSize; c?: string; defs: Map<string, Definition> };

// memo matters twice over: transcript bubbles re-render on every streaming
// chunk of their own text, and StreamingMarkdownBody renders the stable
// prefix through this component with segment strings that only change value
// when a new block boundary settles — so the (potentially huge) prefix bails
// out of re-parsing and re-rendering on every chunk.
export const MarkdownBody = memo(function MarkdownBody({
  text,
  size = 'sm',
  c,
}: {
  text: string;
  size?: MarkdownSize;
  c?: string;
}) {
  const root = useMemo(() => parseMarkdown(text), [text]);
  const ctx = useMemo<RenderCtx>(
    () => ({ size, c, defs: collectDefinitions(root) }),
    [root, size, c],
  );
  if (root.children.length === 0) return null;
  return (
    <Stack gap={6} style={{ minWidth: 0, maxWidth: '100%', overflowWrap: 'anywhere' }}>
      {root.children.map((node, i) => (
        <BlockNode key={i} node={node} ctx={ctx} />
      ))}
    </Stack>
  );
});

/**
 * Markdown body for a still-streaming message. Re-parsing and re-rendering
 * the WHOLE text on every chunk is O(answer²) per turn — long answers from
 * fast-streaming agents (cursor's composer models emit hundreds of chunks a
 * second) saturated the main thread and OOM'd the tab even after the
 * transcript itself was memoized. Instead, split the text into segments at
 * block boundaries reported by the parser: every segment string except the
 * last is value-stable while the answer streams, so the memo'd MarkdownBody
 * bails out for them and each chunk re-renders only the live last block
 * (plus, on new-block chunks, one bounded segment).
 */
export function StreamingMarkdownBody({
  text,
  size,
  c,
}: {
  text: string;
  size?: MarkdownSize;
  c?: string;
}) {
  // Cache the previous split so an appended chunk re-segments only the live
  // tail instead of re-parsing the whole text. Stale/discarded renders are
  // harmless: nextStreamingSegments revalidates with startsWith and falls
  // back to a full split.
  const cache = useRef<{ text: string; segments: string[] }>({ text: '', segments: [] });
  const segments = useMemo(() => {
    const prev = cache.current;
    cache.current = nextStreamingSegments(prev.text, prev.segments, text);
    return cache.current.segments;
  }, [text]);
  if (segments.length <= 1) return <MarkdownBody text={segments[0] ?? ''} size={size} c={c} />;
  return (
    <Stack gap={6} style={{ minWidth: 0, maxWidth: '100%' }}>
      {segments.map((segment, i) => (
        <MarkdownBody key={i} text={segment} size={size} c={c} />
      ))}
    </Stack>
  );
}

// First definition wins per CommonMark; definitions can sit inside
// blockquotes/lists, so walk the whole tree.
function collectDefinitions(root: Root): Map<string, Definition> {
  const defs = new Map<string, Definition>();
  const walk = (nodes: RootContent[]) => {
    for (const node of nodes) {
      if (node.type === 'definition') {
        if (!defs.has(node.identifier)) defs.set(node.identifier, node);
      } else if ('children' in node) {
        walk(node.children as RootContent[]);
      }
    }
  };
  walk(root.children);
  return defs;
}

// Chat content is untrusted: only protocols that can't execute script or
// leak beyond a navigation get a real <a>.
function safeHref(url: string): string | null {
  return /^(https?:|mailto:)/i.test(url) ? url : null;
}

function BlockNode({ node, ctx }: { node: RootContent; ctx: RenderCtx }): ReactNode {
  const { size, c } = ctx;
  switch (node.type) {
    case 'paragraph':
      return (
        <Text size={size} c={c} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'heading':
      return (
        <Text size={node.depth <= 2 ? 'md' : 'sm'} fw={700} c={c}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'code':
      return (
        <Box>
          {node.lang ? (
            <Text size="xs" c="dimmed" mb={2}>
              {node.lang}
            </Text>
          ) : null}
          <Code block style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontSize: 12 }}>
            {node.value}
          </Code>
        </Box>
      );
    case 'blockquote':
      return (
        <Box
          pl={10}
          style={{ borderLeft: '3px solid light-dark(#d1d6de, var(--mantine-color-dark-4))' }}
        >
          <Stack gap={6}>
            {node.children.map((child, i) => (
              <BlockNode key={i} node={child} ctx={ctx} />
            ))}
          </Stack>
        </Box>
      );
    case 'list':
      return <ListBlock node={node} ctx={ctx} />;
    case 'table':
      return <TableBlock node={node} ctx={ctx} />;
    case 'thematicBreak':
      return <Divider my={2} />;
    case 'html':
      // Literal text on purpose — never parse or inject markup from chat.
      return (
        <Text size={size} c={c} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
          {node.value}
        </Text>
      );
    case 'definition':
      // Link-reference metadata; consumed by collectDefinitions, renders nothing.
      return null;
    case 'footnoteDefinition':
      return (
        <Box>
          <Text size="xs" c="dimmed" span>
            [{node.label ?? node.identifier}]:{' '}
          </Text>
          <Stack gap={4} style={{ display: 'inline-flex' }}>
            {node.children.map((child, i) => (
              <BlockNode key={i} node={child} ctx={{ ...ctx, size: 'xs' }} />
            ))}
          </Stack>
        </Box>
      );
    default:
      // Future/unknown node kinds: degrade to their text content rather
      // than dropping content silently.
      if ('children' in node) {
        return (
          <Text size={size} c={c} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
            {renderInline(node.children as PhrasingContent[], ctx)}
          </Text>
        );
      }
      if ('value' in node) {
        return (
          <Text size={size} c={c} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
            {node.value}
          </Text>
        );
      }
      return null;
  }
}

function ListBlock({ node, ctx, nested }: { node: MdList; ctx: RenderCtx; nested?: boolean }) {
  return (
    <List
      type={node.ordered ? 'ordered' : 'unordered'}
      size={ctx.size}
      spacing={3}
      c={ctx.c}
      withPadding={nested}
    >
      {node.children.map((item, i) => (
        <ListItemNode key={i} item={item} ctx={ctx} />
      ))}
    </List>
  );
}

function ListItemNode({ item, ctx }: { item: MdListItem; ctx: RenderCtx }) {
  // GFM task list items get their checkbox as the list marker.
  const icon =
    typeof item.checked === 'boolean' ? (
      item.checked ? (
        <IconSquareCheck size={14} style={{ verticalAlign: 'text-bottom' }} />
      ) : (
        <IconSquare size={14} style={{ verticalAlign: 'text-bottom' }} />
      )
    ) : undefined;
  return (
    // Mantine's List.Item is `white-space: nowrap` by default, which lets
    // long inline content overflow the bubble; force wrapping so list items
    // behave like paragraphs.
    <List.Item icon={icon} style={{ whiteSpace: 'normal' }}>
      {item.children.map((child, i) => {
        // The first paragraph renders inline so tight lists stay tight;
        // nested lists and any further blocks render as blocks below it.
        if (i === 0 && child.type === 'paragraph') {
          return <Fragment key={i}>{renderInline(child.children, ctx)}</Fragment>;
        }
        if (child.type === 'list') {
          return <ListBlock key={i} node={child} ctx={ctx} nested />;
        }
        return (
          <Box key={i} mt={4}>
            <BlockNode node={child} ctx={ctx} />
          </Box>
        );
      })}
    </List.Item>
  );
}

function TableBlock({ node, ctx }: { node: MdTable; ctx: RenderCtx }) {
  const [header, ...rows] = node.children;
  if (!header) return null;
  const align = node.align ?? [];
  return (
    <Table.ScrollContainer minWidth={0} type="native">
      <Table
        striped
        withTableBorder
        withColumnBorders
        verticalSpacing={4}
        horizontalSpacing="xs"
        fz={ctx.size}
        style={ctx.c ? { color: ctx.c } : undefined}
      >
        <Table.Thead>
          <Table.Tr>
            {header.children.map((cell, j) => (
              <Table.Th key={j} style={{ textAlign: align[j] ?? undefined }}>
                {renderInline(cell.children, ctx)}
              </Table.Th>
            ))}
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map((row, r) => (
            <Table.Tr key={r}>
              {header.children.map((_, j) => {
                const cell = row.children[j];
                return (
                  <Table.Td key={j} style={{ textAlign: align[j] ?? undefined }}>
                    {cell ? renderInline(cell.children, ctx) : ''}
                  </Table.Td>
                );
              })}
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

function renderInline(children: PhrasingContent[], ctx: RenderCtx): ReactNode[] {
  return children.map((node, i) => renderInlineNode(node, ctx, i));
}

function renderInlineNode(node: PhrasingContent, ctx: RenderCtx, key: number): ReactNode {
  switch (node.type) {
    case 'text':
      return node.value;
    case 'strong':
      return (
        <Text key={key} span fw={700} inherit>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'emphasis':
      return (
        <Text key={key} span fs="italic" inherit>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'delete':
      return (
        <Text key={key} span td="line-through" inherit>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'inlineCode':
      return (
        <Code key={key} fz="0.85em" style={{ overflowWrap: 'anywhere' }}>
          {node.value}
        </Code>
      );
    case 'break':
      return '\n';
    case 'link': {
      const href = safeHref(node.url);
      if (!href) return <Fragment key={key}>{renderInline(node.children, ctx)}</Fragment>;
      return (
        <Anchor key={key} href={href} target="_blank" rel="noreferrer" c={ctx.c} underline="always">
          {renderInline(node.children, ctx)}
        </Anchor>
      );
    }
    case 'image': {
      // No <img>: chat content must not trigger remote fetches. Render the
      // alt text as a link to the image instead.
      const href = safeHref(node.url);
      const label = node.alt || node.url;
      if (!href) return label;
      return (
        <Anchor key={key} href={href} target="_blank" rel="noreferrer" c={ctx.c} underline="always">
          {label}
        </Anchor>
      );
    }
    case 'linkReference': {
      const def = ctx.defs.get(node.identifier);
      const href = def ? safeHref(def.url) : null;
      if (!href) {
        // Unresolved references render literally, brackets and all, per
        // CommonMark.
        return <Fragment key={key}>[{renderInline(node.children, ctx)}]</Fragment>;
      }
      return (
        <Anchor key={key} href={href} target="_blank" rel="noreferrer" c={ctx.c} underline="always">
          {renderInline(node.children, ctx)}
        </Anchor>
      );
    }
    case 'imageReference': {
      const def = ctx.defs.get(node.identifier);
      const href = def ? safeHref(def.url) : null;
      const label = node.alt || node.identifier;
      if (!href) return label;
      return (
        <Anchor key={key} href={href} target="_blank" rel="noreferrer" c={ctx.c} underline="always">
          {label}
        </Anchor>
      );
    }
    case 'footnoteReference':
      return (
        <Text key={key} span size="xs" c="dimmed" inherit={false}>
          [{node.label ?? node.identifier}]
        </Text>
      );
    case 'html':
      // Literal text on purpose — React escapes it, nothing is injected.
      return node.value;
    default:
      // PhrasingContent is a closed union and every kind is handled above;
      // this only runs if mdast grows a new node type.
      return null;
  }
}
