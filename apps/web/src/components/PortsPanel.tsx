import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Group,
  ScrollArea,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconExternalLink, IconRefresh, IconServer } from '@tabler/icons-react';
import { useEffect, useMemo, useState } from 'react';

import { proxyURL } from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';
import { usePorts } from '../store/ports.js';

export function PortsPanel() {
  const status = useConnection((s) => s.status);
  const credentials = useConnection((s) => s.credentials);
  const entries = usePorts((s) => s.entries);
  const refresh = usePorts((s) => s.refresh);
  const [error, setError] = useState<string | null>(null);
  const [auto, setAuto] = useState(true);

  useEffect(() => {
    if (status !== 'connected') return;
    refresh().catch((e: Error) => setError(e.message));
  }, [status, refresh]);

  // Optional polling: refresh every 5s while the panel is mounted.
  useEffect(() => {
    if (!auto || status !== 'connected') return;
    const t = setInterval(() => {
      refresh().catch(() => {});
    }, 5000);
    return () => clearInterval(t);
  }, [auto, status, refresh]);

  const sorted = useMemo(() => [...entries].sort((a, b) => a.port - b.port), [entries]);

  return (
    <Stack
      gap="md"
      p="md"
      style={{
        border: '1px solid var(--mantine-color-default-border)',
        borderRadius: 8,
        height: 'calc(100vh - 90px)',
      }}
    >
      <Group justify="space-between">
        <Group gap="xs">
          <IconServer size={18} />
          <Text fw={600}>Listening ports</Text>
          <Badge size="xs" variant="light">
            {sorted.length}
          </Badge>
        </Group>
        <Group gap="xs">
          <Button size="xs" variant={auto ? 'filled' : 'light'} onClick={() => setAuto((v) => !v)}>
            {auto ? 'auto-refresh on' : 'auto-refresh off'}
          </Button>
          <Tooltip label="Reload">
            <ActionIcon
              variant="subtle"
              onClick={() => refresh().catch((e: Error) => setError(e.message))}
            >
              <IconRefresh size={14} />
            </ActionIcon>
          </Tooltip>
        </Group>
      </Group>
      {error ? (
        <Alert color="red" variant="light" radius="md">
          {error}
        </Alert>
      ) : null}
      <ScrollArea style={{ flex: 1, minHeight: 0 }} type="auto" offsetScrollbars>
        <Stack gap="xs">
          {sorted.length === 0 ? (
            <Text size="sm" c="dimmed">
              {status === 'connected' ? 'No listening ports detected.' : 'Waiting for relay…'}
            </Text>
          ) : (
            sorted.map((entry) => (
              <PortRow
                key={entry.port}
                port={entry.port}
                address={entry.address}
                family={entry.family}
                pid={entry.pid}
                command={entry.command}
                relayURL={credentials?.relay_url ?? ''}
                accessToken={credentials?.bundle.access_token ?? ''}
              />
            ))
          )}
        </Stack>
      </ScrollArea>
    </Stack>
  );
}

interface PortRowProps {
  port: number;
  address: string;
  family: string;
  pid?: number;
  command?: string;
  relayURL: string;
  accessToken: string;
}

function PortRow({ port, address, family, pid, command, relayURL, accessToken }: PortRowProps) {
  const [path, setPath] = useState('/');
  const url = useMemo(() => {
    if (!relayURL || !accessToken) return '';
    return proxyURL({ relayURL, port, path, token: accessToken });
  }, [relayURL, accessToken, port, path]);

  return (
    <Card padding="sm" radius="md" withBorder>
      <Group gap="md" wrap="nowrap" align="flex-start">
        <Stack gap={2} style={{ minWidth: 80 }}>
          <Text fw={600}>:{port}</Text>
          <Badge size="xs" variant="light">
            {family}
          </Badge>
        </Stack>
        <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
          <Text size="sm" truncate>
            {command ?? 'unknown'}
            {pid ? ` · pid ${pid}` : null}
          </Text>
          <Text size="xs" c="dimmed">
            bound to {address}
          </Text>
          <Group gap="xs" mt={6}>
            <TextInput
              size="xs"
              style={{ flex: 1, minWidth: 0 }}
              value={path}
              onChange={(e) => setPath(e.currentTarget.value)}
              placeholder="/"
              aria-label="path"
            />
            <Button
              size="xs"
              leftSection={<IconExternalLink size={12} />}
              component="a"
              href={url}
              target="_blank"
              rel="noreferrer noopener"
              disabled={!url}
            >
              Open
            </Button>
          </Group>
        </Stack>
      </Group>
    </Card>
  );
}
