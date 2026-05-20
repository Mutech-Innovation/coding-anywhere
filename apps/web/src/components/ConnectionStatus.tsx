import { Badge, Group, Tooltip } from '@mantine/core';

// Placeholder connection status indicator. Real wiring lands in P1 (relay
// auth + WS) and P3 (daemon presence).
export function ConnectionStatus() {
  return (
    <Tooltip label="Relay connection wires up in P1">
      <Group gap={6}>
        <Badge color="gray" variant="light" radius="sm">
          Offline
        </Badge>
      </Group>
    </Tooltip>
  );
}
