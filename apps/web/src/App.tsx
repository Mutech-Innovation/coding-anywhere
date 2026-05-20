import { AppShell, Badge, Box, Group, Stack, Text, Title } from '@mantine/core';
import { IconCode, IconTerminal2 } from '@tabler/icons-react';

import { AGENTS, listAgents } from '@coding-anywhere/agents';

import { ConnectionStatus } from './components/ConnectionStatus.js';

export function App() {
  const agents = listAgents();

  return (
    <AppShell padding="md" header={{ height: 56 }}>
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group gap="xs">
            <IconTerminal2 size={20} stroke={1.6} />
            <Title order={4} fw={600}>
              Coding Anywhere
            </Title>
            <Badge size="xs" variant="light">
              P0 scaffold
            </Badge>
          </Group>
          <ConnectionStatus />
        </Group>
      </AppShell.Header>
      <AppShell.Main>
        <Stack gap="lg">
          <Box>
            <Title order={2}>Welcome</Title>
            <Text c="dimmed" mt="xs">
              Remote-control your dev terminal and AI coding agents from any device.
            </Text>
          </Box>
          <Box>
            <Title order={4} mb="xs">
              Supported agents
            </Title>
            <Stack gap="xs">
              {agents.map((agent) => (
                <Group
                  key={agent.id}
                  justify="space-between"
                  p="sm"
                  style={{
                    border: '1px solid var(--mantine-color-default-border)',
                    borderRadius: 8,
                  }}
                >
                  <Group gap="sm">
                    <IconCode size={18} color={agent.accent} />
                    <Stack gap={0}>
                      <Text fw={500}>{agent.name}</Text>
                      <Text size="xs" c="dimmed">
                        {agent.tagline}
                      </Text>
                    </Stack>
                  </Group>
                  <Badge
                    color={agent.id === AGENTS.devin.id ? 'brand' : 'gray'}
                    variant={agent.available ? 'filled' : 'light'}
                  >
                    {agent.available ? 'Available' : 'Coming soon'}
                  </Badge>
                </Group>
              ))}
            </Stack>
          </Box>
        </Stack>
      </AppShell.Main>
    </AppShell>
  );
}
