import { javascript } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { markdown } from '@codemirror/lang-markdown';
import { MergeView } from '@codemirror/merge';
import { EditorState, type Extension } from '@codemirror/state';
import { EditorView, lineNumbers } from '@codemirror/view';
import { Box } from '@mantine/core';
import { useEffect, useRef } from 'react';

interface Props {
  filename: string;
  original: string;
  current: string;
}

function languageFor(filename: string): Extension[] {
  const ext = filename.split('.').pop()?.toLowerCase() ?? '';
  switch (ext) {
    case 'ts':
    case 'tsx':
      return [javascript({ jsx: true, typescript: true })];
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
      return [javascript({ jsx: ext === 'jsx' })];
    case 'json':
    case 'jsonc':
      return [json()];
    case 'md':
    case 'markdown':
    case 'mdx':
      return [markdown()];
    default:
      return [];
  }
}

const MERGE_THEME = EditorView.theme(
  {
    '&': { fontSize: '13px', height: '100%' },
    '.cm-scroller': {
      fontFamily: 'ui-monospace, SFMono-Regular, "JetBrains Mono", Menlo, Consolas, monospace',
      lineHeight: '1.45',
    },
    '.cm-content': { padding: '8px 0' },
    '&.cm-editor': { backgroundColor: '#0b0f17' },
    '.cm-gutters': {
      backgroundColor: '#0b0f17',
      color: '#3d4a60',
      borderRight: '1px solid #19202c',
    },
  },
  { dark: true },
);

/** Side-by-side diff using @codemirror/merge. */
export function DiffViewer({ filename, original, current }: Props) {
  const ref = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<MergeView | null>(null);

  useEffect(() => {
    const host = ref.current;
    if (!host) return;
    const lang = languageFor(filename);
    const extensions = [
      EditorView.editable.of(false),
      EditorState.readOnly.of(true),
      lineNumbers(),
      MERGE_THEME,
      ...lang,
    ];
    const merge = new MergeView({
      a: { doc: original, extensions },
      b: { doc: current, extensions },
      parent: host,
      revertControls: 'a-to-b',
      collapseUnchanged: { margin: 4, minSize: 6 },
    });
    viewRef.current = merge;
    return () => {
      merge.destroy();
      viewRef.current = null;
    };
  }, [filename, original, current]);

  return (
    <Box
      ref={ref}
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        background: '#0b0f17',
        borderRadius: 6,
        overflow: 'hidden',
      }}
    />
  );
}
