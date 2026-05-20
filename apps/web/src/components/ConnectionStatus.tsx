import { Badge, Group, Tooltip } from '@mantine/core';

import { useConnection } from '../store/connection.js';

const STATUS_LABELS: Record<string, { label: string; color: string }> = {
  connecting: { label: 'Connecting…', color: 'yellow' },
  connected: { label: 'Online', color: 'green' },
  disconnected: { label: 'Offline', color: 'gray' },
};

export function ConnectionStatus() {
  const { credentials, status } = useConnection((s) => ({
    credentials: s.credentials,
    status: s.status,
  }));
  const meta = STATUS_LABELS[status] ?? STATUS_LABELS.disconnected;

  if (!credentials) {
    return (
      <Tooltip label="Not paired yet">
        <Badge color="gray" variant="light" radius="sm">
          Not paired
        </Badge>
      </Tooltip>
    );
  }

  return (
    <Group gap="xs">
      <Tooltip label={`${credentials.bundle.device_label} · ${credentials.relay_url}`}>
        <Badge color={meta?.color ?? 'gray'} variant="light" radius="sm">
          {meta?.label ?? status}
        </Badge>
      </Tooltip>
    </Group>
  );
}
