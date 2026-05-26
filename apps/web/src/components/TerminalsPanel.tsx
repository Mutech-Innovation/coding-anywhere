import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Group,
  ScrollArea,
  Stack,
  Text,
  Tooltip,
} from '@mantine/core';
import { IconPlus, IconTerminal2, IconX } from '@tabler/icons-react';
import { useEffect, useMemo, useState } from 'react';

import { useConnection } from '../store/connection.js';
import { useTerminals } from '../store/terminals.js';

import { TerminalView } from './TerminalView.js';

/**
 * Standalone-terminal page: list of PTY sessions on the left, the active one
 * on the right. Hooks into the live RelayWSClient via the terminals store
 * once the connection is paired.
 */
export function TerminalsPanel() {
  const status = useConnection((s) => s.status);
  const client = useConnection((s) => s.client);
  const attach = useTerminals((s) => s.attach);
  const refreshList = useTerminals((s) => s.refreshList);
  const sessions = useTerminals((s) => s.sessions);
  const activeId = useTerminals((s) => s.activeId);
  const open = useTerminals((s) => s.open);
  const setActive = useTerminals((s) => s.setActive);
  const closeSession = useTerminals((s) => s.closeSession);

  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Wire the store to the relay WS client whenever we reconnect.
  useEffect(() => {
    if (!client) return;
    attach(client);
  }, [client, attach]);

  // Refresh session list when we come online.
  useEffect(() => {
    if (status !== 'connected') return;
    refreshList().catch((e: Error) => setError(e.message));
  }, [status, refreshList]);

  const list = useMemo(
    () => Object.values(sessions).sort((a, b) => (a.id < b.id ? -1 : 1)),
    [sessions],
  );

  async function onOpen() {
    setError(null);
    setBusy(true);
    try {
      await open({});
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function onClose(id: string) {
    setError(null);
    try {
      await closeSession(id);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  return (
    <Group align="stretch" gap="md" h="100%" wrap="nowrap" style={{ minHeight: 0 }}>
      <Stack
        gap="xs"
        miw={240}
        maw={300}
        p="sm"
        style={{
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
          flexShrink: 0,
        }}
      >
        <Group justify="space-between">
          <Text fw={600}>Terminals</Text>
          <Tooltip label="Open new shell">
            <ActionIcon
              variant="filled"
              color="brand"
              onClick={onOpen}
              loading={busy}
              disabled={status !== 'connected'}
            >
              <IconPlus size={16} />
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
        <ScrollArea.Autosize mah="60vh">
          <Stack gap={4}>
            {list.length === 0 ? (
              <Text size="sm" c="dimmed">
                No sessions yet. Click + to open one.
              </Text>
            ) : (
              list.map((s) => (
                <Group
                  key={s.id}
                  gap="xs"
                  px="xs"
                  py={6}
                  wrap="nowrap"
                  onClick={() => setActive(s.id)}
                  style={{
                    cursor: 'pointer',
                    borderRadius: 6,
                    background:
                      activeId === s.id
                        ? 'light-dark(#e9eefe, var(--mantine-color-brand-9))'
                        : 'transparent',
                    color:
                      activeId === s.id
                        ? 'light-dark(var(--mantine-color-brand-8), #ffffff)'
                        : undefined,
                  }}
                >
                  <IconTerminal2 size={14} />
                  <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
                    <Text size="xs" fw={500} truncate>
                      {(s.command && s.command[0]) ?? 'shell'}
                    </Text>
                    <Text size="xs" c="dimmed" truncate>
                      pid {s.pid} · {s.cols}×{s.rows}
                    </Text>
                  </Stack>
                  <Tooltip label="Close">
                    <ActionIcon
                      size="sm"
                      variant="subtle"
                      color="gray"
                      onClick={(e) => {
                        e.stopPropagation();
                        void onClose(s.id);
                      }}
                    >
                      <IconX size={14} />
                    </ActionIcon>
                  </Tooltip>
                </Group>
              ))
            )}
          </Stack>
        </ScrollArea.Autosize>
        <Group gap="xs" mt="auto">
          <Badge size="xs" variant="light">
            {list.length} session{list.length === 1 ? '' : 's'}
          </Badge>
        </Group>
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
        {activeId ? (
          <TerminalView sessionId={activeId} />
        ) : (
          <Stack gap="xs" align="center" justify="center" style={{ flex: 1 }}>
            <IconTerminal2 size={32} stroke={1.4} color="var(--mantine-color-dimmed)" />
            <Text c="dimmed">Open or pick a terminal session to start.</Text>
            <Button
              leftSection={<IconPlus size={14} />}
              size="sm"
              variant="light"
              onClick={onOpen}
              loading={busy}
              disabled={status !== 'connected'}
            >
              New shell
            </Button>
          </Stack>
        )}
      </Box>
    </Group>
  );
}
