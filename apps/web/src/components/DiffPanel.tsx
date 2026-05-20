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
  IconCircleDashed,
  IconFileDescription,
  IconFileMinus,
  IconFilePlus,
  IconRefresh,
} from '@tabler/icons-react';
import { useEffect, useMemo, useState } from 'react';

import type { DiffChangeKind, DiffVersionsResponse } from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';
import { useDiff } from '../store/diff.js';

import { DiffViewer } from './DiffViewer.js';

const KIND_COLOR: Record<DiffChangeKind, string> = {
  added: 'green',
  modified: 'yellow',
  deleted: 'red',
  renamed: 'blue',
  copied: 'cyan',
  untracked: 'gray',
};

function KindIcon({ kind }: { kind: DiffChangeKind }) {
  const size = 14;
  switch (kind) {
    case 'added':
    case 'untracked':
      return <IconFilePlus size={size} />;
    case 'deleted':
      return <IconFileMinus size={size} />;
    case 'modified':
    case 'renamed':
    case 'copied':
      return <IconFileDescription size={size} />;
    default:
      return <IconCircleDashed size={size} />;
  }
}

export function DiffPanel() {
  const status = useConnection((s) => s.status);
  const isRepo = useDiff((s) => s.isRepo);
  const changes = useDiff((s) => s.changes);
  const refresh = useDiff((s) => s.refresh);
  const activePath = useDiff((s) => s.activePath);
  const setActive = useDiff((s) => s.setActive);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (status !== 'connected') return;
    refresh().catch((e: Error) => setError(e.message));
  }, [status, refresh]);

  const sorted = useMemo(
    () => [...changes].sort((a, b) => a.path.localeCompare(b.path)),
    [changes],
  );

  return (
    <Group align="stretch" gap="md" h="100%" wrap="nowrap" style={{ minHeight: 0 }}>
      <Stack
        gap="xs"
        miw={300}
        maw={380}
        p="sm"
        style={{
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
          flexShrink: 0,
        }}
      >
        <Group justify="space-between">
          <Text fw={600}>Changes</Text>
          <Tooltip label="Reload">
            <ActionIcon
              variant="subtle"
              onClick={() => refresh().catch((e: Error) => setError(e.message))}
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
        {!isRepo ? (
          <Text size="sm" c="dimmed">
            Workspace is not a git repository.
          </Text>
        ) : null}
        <ScrollArea.Autosize mah="calc(100vh - 200px)">
          <Stack gap={2}>
            {sorted.length === 0 ? (
              <Text size="sm" c="dimmed">
                No changes.
              </Text>
            ) : (
              sorted.map((c) => (
                <Group
                  key={c.path}
                  gap="xs"
                  px="xs"
                  py={6}
                  wrap="nowrap"
                  onClick={() => setActive(c.path)}
                  style={{
                    cursor: 'pointer',
                    borderRadius: 6,
                    background:
                      activePath === c.path ? 'var(--mantine-color-brand-9)' : 'transparent',
                  }}
                >
                  <KindIcon kind={c.kind} />
                  <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
                    <Text size="xs" fw={500} truncate>
                      {c.path}
                    </Text>
                    {c.old_path ? (
                      <Text size="xs" c="dimmed" truncate>
                        from {c.old_path}
                      </Text>
                    ) : null}
                  </Stack>
                  <Badge size="xs" color={KIND_COLOR[c.kind]} variant="light">
                    {c.kind}
                  </Badge>
                </Group>
              ))
            )}
          </Stack>
        </ScrollArea.Autosize>
      </Stack>

      <Box
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          minWidth: 0,
          minHeight: 0,
          padding: 8,
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
        }}
      >
        {activePath ? (
          <ActiveDiff path={activePath} />
        ) : (
          <Stack gap="xs" align="center" justify="center" style={{ flex: 1 }}>
            <IconFileDescription size={32} stroke={1.4} color="var(--mantine-color-dimmed)" />
            <Text c="dimmed">Pick a changed file to see its diff.</Text>
          </Stack>
        )}
      </Box>
    </Group>
  );
}

function ActiveDiff({ path }: { path: string }) {
  const versions = useDiff((s) => s.versions);
  const [data, setData] = useState<DiffVersionsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setData(null);
    setError(null);
    versions(path)
      .then(setData)
      .catch((e: Error) => setError(e.message));
  }, [versions, path]);

  if (error) {
    return (
      <Alert color="red" variant="light" radius="md">
        {error}
      </Alert>
    );
  }
  if (!data) {
    return (
      <Text c="dimmed" px="xs">
        Loading…
      </Text>
    );
  }
  return (
    <Stack style={{ flex: 1, minHeight: 0 }} gap="xs">
      <Group justify="space-between" px="xs">
        <Stack gap={0}>
          <Text fw={600} size="sm">
            {path.split('/').pop()}
          </Text>
          <Text size="xs" c="dimmed">
            {path}
          </Text>
        </Stack>
        <Badge size="xs" variant="light">
          HEAD vs working tree
        </Badge>
      </Group>
      <DiffViewer
        filename={path.split('/').pop() ?? path}
        original={data.original}
        current={data.current}
      />
    </Stack>
  );
}
