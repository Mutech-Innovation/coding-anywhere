import { Badge, Group, Tooltip } from '@mantine/core';

import { useConnection } from '../store/connection.js';

const STATUS_LABELS: Record<string, { label: string; color: string }> = {
  connecting: { label: 'Connecting…', color: 'yellow' },
  connected: { label: 'Online', color: 'green' },
  disconnected: { label: 'Offline', color: 'gray' },
};

export function ConnectionStatus() {
  // Select primitives independently — returning a fresh object literal would
  // cause an infinite render loop in zustand v5.
  const credentials = useConnection((s) => s.credentials);
  const status = useConnection((s) => s.status);
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
