import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Group,
  ScrollArea,
  Stack,
  Text,
  Tooltip,
} from '@mantine/core';
import {
  IconChevronDown,
  IconChevronRight,
  IconFile,
  IconFileText,
  IconFolder,
  IconLink,
  IconRefresh,
} from '@tabler/icons-react';
import { useCallback, useEffect, useMemo, useState } from 'react';

import type { FilesEntry, FilesReadResponse } from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';
import { useFiles } from '../store/files.js';

import { CodeViewer } from './CodeViewer.js';

export function FilesPanel() {
  const status = useConnection((s) => s.status);
  const workspaces = useFiles((s) => s.workspaces);
  const workspaceId = useFiles((s) => s.workspaceId);
  const loadWorkspaces = useFiles((s) => s.loadWorkspaces);
  const activePath = useFiles((s) => s.activePath);
  const setActivePath = useFiles((s) => s.setActivePath);
  const [error, setError] = useState<string | null>(null);
  const [openDirs, setOpenDirs] = useState<Set<string>>(() => new Set(['']));

  useEffect(() => {
    if (status !== 'connected') return;
    loadWorkspaces().catch((e: Error) => setError(e.message));
  }, [status, loadWorkspaces]);

  const ws = workspaces.find((w) => w.id === workspaceId);

  return (
    <Group align="stretch" gap="md" h="calc(100vh - 90px)" wrap="nowrap">
      <Stack
        gap="xs"
        miw={280}
        maw={360}
        p="sm"
        style={{
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
          flexShrink: 0,
        }}
      >
        <Group justify="space-between">
          <Stack gap={0} style={{ minWidth: 0 }}>
            <Text fw={600}>{ws?.name ?? 'Workspace'}</Text>
            {ws ? (
              <Tooltip label={ws.root}>
                <Text size="xs" c="dimmed" truncate>
                  {ws.root}
                </Text>
              </Tooltip>
            ) : null}
          </Stack>
          <Tooltip label="Reload">
            <ActionIcon
              variant="subtle"
              onClick={() => {
                setOpenDirs(new Set(['']));
                setActivePath(null);
                loadWorkspaces().catch((e: Error) => setError(e.message));
              }}
            >
              <IconRefresh size={14} />
            </ActionIcon>
          </Tooltip>
        </Group>
        {error ? (
          <Alert color="red" variant="light" radius="md">
            {error}
          </Alert>
        ) : null}
        {status !== 'connected' ? (
          <Text size="sm" c="dimmed">
            Waiting for relay connection…
          </Text>
        ) : null}
        <ScrollArea.Autosize mah="calc(100vh - 200px)">
          <DirNode
            path=""
            label={ws?.name ?? 'workspace'}
            depth={0}
            openDirs={openDirs}
            setOpenDirs={setOpenDirs}
            setActivePath={setActivePath}
            activePath={activePath}
          />
        </ScrollArea.Autosize>
      </Stack>

      <Box
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          minWidth: 0,
          padding: 8,
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
        }}
      >
        {activePath ? <FileViewer path={activePath} /> : <EmptyViewer />}
      </Box>
    </Group>
  );
}

function EmptyViewer() {
  return (
    <Stack gap="xs" align="center" justify="center" style={{ flex: 1 }}>
      <IconFile size={32} stroke={1.4} color="var(--mantine-color-dimmed)" />
      <Text c="dimmed">Pick a file from the tree to preview it.</Text>
    </Stack>
  );
}

function DirNode({
  path,
  label,
  depth,
  openDirs,
  setOpenDirs,
  setActivePath,
  activePath,
}: {
  path: string;
  label: string;
  depth: number;
  openDirs: Set<string>;
  setOpenDirs: (s: Set<string>) => void;
  setActivePath: (path: string | null) => void;
  activePath: string | null;
}) {
  const list = useFiles((s) => s.list);
  const workspaceId = useFiles((s) => s.workspaceId);
  const cacheKey = `${workspaceId ?? ''}|${path}`;
  const cached = useFiles((s) => s.listings[cacheKey]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const isOpen = openDirs.has(path);

  useEffect(() => {
    if (!isOpen || cached) return;
    setLoading(true);
    list(path)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, [isOpen, cached, list, path]);

  const toggle = useCallback(() => {
    const next = new Set(openDirs);
    if (next.has(path)) next.delete(path);
    else next.add(path);
    setOpenDirs(next);
  }, [openDirs, path, setOpenDirs]);

  return (
    <Stack gap={0}>
      <Group
        gap={4}
        py={3}
        px="xs"
        wrap="nowrap"
        onClick={toggle}
        style={{
          cursor: 'pointer',
          paddingLeft: depth * 12 + 4,
        }}
      >
        {isOpen ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
        <IconFolder size={14} />
        <Text size="xs" fw={depth === 0 ? 600 : 500} truncate>
          {label}
        </Text>
      </Group>
      {isOpen ? (
        <Stack gap={0}>
          {error ? (
            <Text size="xs" c="red" px="md">
              {error}
            </Text>
          ) : null}
          {loading && !cached ? (
            <Text size="xs" c="dimmed" px="md">
              Loading…
            </Text>
          ) : null}
          {(cached ?? []).map((entry) =>
            entry.kind === 'directory' ? (
              <DirNode
                key={entry.path}
                path={entry.path}
                label={entry.name}
                depth={depth + 1}
                openDirs={openDirs}
                setOpenDirs={setOpenDirs}
                setActivePath={setActivePath}
                activePath={activePath}
              />
            ) : (
              <FileLeaf
                key={entry.path}
                entry={entry}
                depth={depth + 1}
                active={activePath === entry.path}
                onClick={() => setActivePath(entry.path)}
              />
            ),
          )}
        </Stack>
      ) : null}
    </Stack>
  );
}

function FileLeaf({
  entry,
  depth,
  active,
  onClick,
}: {
  entry: FilesEntry;
  depth: number;
  active: boolean;
  onClick: () => void;
}) {
  const Icon =
    entry.kind === 'symlink' ? IconLink : isLikelyText(entry.name) ? IconFileText : IconFile;
  return (
    <Group
      gap={4}
      py={3}
      px="xs"
      wrap="nowrap"
      onClick={onClick}
      style={{
        cursor: 'pointer',
        paddingLeft: depth * 12 + 4 + 16,
        background: active ? 'var(--mantine-color-brand-9)' : 'transparent',
        borderRadius: 4,
      }}
    >
      <Icon size={14} />
      <Text size="xs" truncate>
        {entry.name}
      </Text>
    </Group>
  );
}

function FileViewer({ path }: { path: string }) {
  const readFile = useFiles((s) => s.readFile);
  const [text, setText] = useState<string | null>(null);
  const [meta, setMeta] = useState<FilesReadResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setText(null);
    setError(null);
    readFile(path)
      .then((r) => {
        setText(r.text ?? '');
        setMeta(r.res);
      })
      .catch((e: Error) => setError(e.message));
  }, [readFile, path]);

  const filename = useMemo(() => path.split('/').pop() ?? path, [path]);

  return (
    <Stack style={{ flex: 1, minHeight: 0 }} gap="xs">
      <Group justify="space-between" px="xs">
        <Stack gap={0}>
          <Text fw={600} size="sm" truncate>
            {filename}
          </Text>
          <Tooltip label={path}>
            <Text size="xs" c="dimmed" truncate>
              {path}
            </Text>
          </Tooltip>
        </Stack>
        <Group gap="xs">
          {meta ? (
            <Badge size="xs" variant="light">
              {humanSize(meta.size)}
            </Badge>
          ) : null}
          {meta?.is_binary ? (
            <Badge size="xs" color="yellow" variant="light">
              binary
            </Badge>
          ) : null}
          {meta?.truncated ? (
            <Badge size="xs" color="orange" variant="light">
              truncated
            </Badge>
          ) : null}
        </Group>
      </Group>
      {error ? (
        <Alert color="red" variant="light" radius="md">
          {error}
        </Alert>
      ) : meta?.is_binary ? (
        <Stack align="center" justify="center" style={{ flex: 1 }} gap="xs">
          <IconFile size={32} stroke={1.4} />
          <Text c="dimmed">{meta.content_type ?? 'binary file'} — preview not shown.</Text>
        </Stack>
      ) : text == null ? (
        <Text c="dimmed" px="xs">
          Loading…
        </Text>
      ) : (
        <CodeViewer text={text} filename={filename} />
      )}
    </Stack>
  );
}

function isLikelyText(name: string): boolean {
  return /\.(md|txt|ts|tsx|js|jsx|json|go|py|rs|toml|yaml|yml|css|html|sh|conf|env|gitignore|prettierrc|prettierignore)$/i.test(
    name,
  );
}

function humanSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
