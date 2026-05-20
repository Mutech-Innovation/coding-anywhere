import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Group,
  Modal,
  ScrollArea,
  Stack,
  Text,
  Textarea,
  TextInput,
  Title,
  Tooltip,
} from '@mantine/core';
import {
  IconCheck,
  IconCirclePlus,
  IconMessageCircle,
  IconPlayerStop,
  IconSend,
  IconShieldCheck,
  IconTool,
  IconX,
} from '@tabler/icons-react';
import { useEffect, useMemo, useRef, useState } from 'react';

import type { ChatToolCall } from '../store/chat.js';
import { useChat } from '../store/chat.js';
import { useConnection } from '../store/connection.js';

const STATUS_COLOR: Record<string, string> = {
  idle: 'gray',
  thinking: 'yellow',
  awaiting_permission: 'red',
  closed: 'dark',
};

export function ChatPanel() {
  const status = useConnection((s) => s.status);
  const client = useConnection((s) => s.client);
  const attach = useChat((s) => s.attach);
  const attached = useChat((s) => s.attached);
  const sessions = useChat((s) => s.sessions);
  const activeId = useChat((s) => s.activeId);
  const setActive = useChat((s) => s.setActive);
  const refresh = useChat((s) => s.refreshSessions);
  const loadAgents = useChat((s) => s.loadAgents);
  const closeSession = useChat((s) => s.closeSession);

  const [creatorOpen, setCreatorOpen] = useState(false);

  useEffect(() => {
    if (!client || attached) return;
    attach(client);
  }, [client, attached, attach]);

  useEffect(() => {
    if (status !== 'connected') return;
    void refresh().catch(() => {});
    void loadAgents().catch(() => {});
  }, [status, refresh, loadAgents]);

  const list = useMemo(
    () =>
      Object.values(sessions).sort(
        (a, b) =>
          new Date(b.session.started_at).getTime() - new Date(a.session.started_at).getTime(),
      ),
    [sessions],
  );

  return (
    <Group align="stretch" gap="md" h="calc(100vh - 90px)" wrap="nowrap">
      <Stack
        gap="xs"
        miw={260}
        maw={320}
        p="sm"
        style={{
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
          flexShrink: 0,
        }}
      >
        <Group justify="space-between">
          <Text fw={600}>Sessions</Text>
          <Tooltip label="Start a new session with Devin">
            <ActionIcon
              variant="filled"
              color="brand"
              onClick={() => setCreatorOpen(true)}
              disabled={status !== 'connected'}
            >
              <IconCirclePlus size={16} />
            </ActionIcon>
          </Tooltip>
        </Group>
        {status !== 'connected' ? (
          <Text size="sm" c="dimmed">
            Waiting for relay connection…
          </Text>
        ) : null}
        <ScrollArea.Autosize mah="60vh">
          <Stack gap={4}>
            {list.length === 0 ? (
              <Text size="sm" c="dimmed">
                No sessions yet. Click + to start one.
              </Text>
            ) : (
              list.map((s) => (
                <Group
                  key={s.session.id}
                  gap="xs"
                  px="xs"
                  py={6}
                  wrap="nowrap"
                  onClick={() => setActive(s.session.id)}
                  style={{
                    cursor: 'pointer',
                    borderRadius: 6,
                    background:
                      activeId === s.session.id ? 'var(--mantine-color-brand-9)' : 'transparent',
                  }}
                >
                  <IconMessageCircle size={14} />
                  <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
                    <Text size="xs" fw={500} truncate>
                      {s.session.title || s.session.cwd}
                    </Text>
                    <Text size="xs" c="dimmed" truncate>
                      {s.session.agent_id} · {s.session.cwd}
                    </Text>
                  </Stack>
                  <Badge size="xs" color={STATUS_COLOR[s.session.status] ?? 'gray'} variant="light">
                    {s.session.status}
                  </Badge>
                  <Tooltip label="Close session">
                    <ActionIcon
                      size="sm"
                      variant="subtle"
                      color="gray"
                      onClick={(e) => {
                        e.stopPropagation();
                        void closeSession(s.session.id);
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
        {activeId ? (
          <ChatView sessionId={activeId} />
        ) : (
          <Stack gap="xs" align="center" justify="center" style={{ flex: 1 }}>
            <IconMessageCircle size={32} stroke={1.4} color="var(--mantine-color-dimmed)" />
            <Text c="dimmed">Pick a session or start a new one to chat with Devin.</Text>
          </Stack>
        )}
      </Box>

      <CreateSessionModal opened={creatorOpen} onClose={() => setCreatorOpen(false)} />
    </Group>
  );
}

function CreateSessionModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const create = useChat((s) => s.createSession);
  const agents = useChat((s) => s.agents);
  const [cwd, setCwd] = useState('/Users');
  const [title, setTitle] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const devin = agents.find((a) => a.id === 'devin');
  const devinAvailable = devin?.available ?? true;

  async function onSubmit() {
    setError(null);
    setBusy(true);
    try {
      await create({ cwd, agentId: 'devin', title: title.trim() || undefined });
      onClose();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="Start a Devin session" centered>
      <Stack gap="md">
        <TextInput
          label="Working directory"
          description="Devin will run inside this directory."
          required
          value={cwd}
          onChange={(e) => setCwd(e.currentTarget.value)}
        />
        <TextInput
          label="Title"
          placeholder="(optional)"
          value={title}
          onChange={(e) => setTitle(e.currentTarget.value)}
        />
        {!devinAvailable ? (
          <Alert color="yellow" variant="light" radius="md">
            Devin reports as unavailable on this daemon. The CLI may not be installed or
            authenticated.
          </Alert>
        ) : null}
        {error ? (
          <Alert color="red" variant="light" radius="md">
            {error}
          </Alert>
        ) : null}
        <Group justify="flex-end">
          <Button variant="subtle" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={onSubmit} loading={busy}>
            Start
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}

function ChatView({ sessionId }: { sessionId: string }) {
  const session = useChat((s) => s.sessions[sessionId]);
  const sendPrompt = useChat((s) => s.sendPrompt);
  const cancel = useChat((s) => s.cancel);
  const resolvePermission = useChat((s) => s.resolvePermission);
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Auto-scroll on new content.
  useEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [session?.messages.length, session?.session.status]);

  if (!session) return null;

  const status = session.session.status;
  const pendingPermissions = Object.values(session.permissions).filter((p) => p.pending);

  async function onSend() {
    if (!draft.trim()) return;
    setError(null);
    setBusy(true);
    try {
      const text = draft;
      setDraft('');
      await sendPrompt(sessionId, text);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Stack style={{ flex: 1, minHeight: 0 }} gap="xs">
      <Group justify="space-between" px="xs">
        <Stack gap={0}>
          <Text fw={600} size="sm" truncate>
            {session.session.title || session.session.cwd}
          </Text>
          <Text size="xs" c="dimmed" truncate>
            {session.session.agent_id} · {session.session.cwd}
          </Text>
        </Stack>
        <Group gap="xs">
          <Badge color={STATUS_COLOR[status] ?? 'gray'} variant="light">
            {status}
          </Badge>
          {status === 'thinking' || status === 'awaiting_permission' ? (
            <Tooltip label="Cancel">
              <ActionIcon variant="subtle" color="red" onClick={() => void cancel(sessionId)}>
                <IconPlayerStop size={14} />
              </ActionIcon>
            </Tooltip>
          ) : null}
        </Group>
      </Group>

      <ScrollArea
        viewportRef={scrollRef}
        style={{ flex: 1, minHeight: 0 }}
        type="auto"
        offsetScrollbars
      >
        <Stack gap="sm" px="xs">
          {pendingPermissions.map((p) => (
            <PermissionCard
              key={p.requestId}
              params={p.params}
              onAllow={() => void resolvePermission(sessionId, p.requestId, { decision: 'allow' })}
              onDeny={() => void resolvePermission(sessionId, p.requestId, { decision: 'deny' })}
            />
          ))}
          {session.messages.map((m) => (
            <MessageBubble key={m.id} message={m} />
          ))}
          {Object.values(session.toolCalls).map((tc) => (
            <ToolCallCard key={tc.id} call={tc} />
          ))}
        </Stack>
      </ScrollArea>

      {error ? (
        <Alert color="red" variant="light" radius="md">
          {error}
        </Alert>
      ) : null}

      <Group gap="xs" px="xs" align="flex-end">
        <Textarea
          placeholder="Ask Devin..."
          autosize
          minRows={1}
          maxRows={6}
          value={draft}
          style={{ flex: 1 }}
          onChange={(e) => setDraft(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              void onSend();
            }
          }}
        />
        <Button leftSection={<IconSend size={14} />} onClick={onSend} loading={busy}>
          Send
        </Button>
      </Group>
    </Stack>
  );
}

function MessageBubble({ message }: { message: import('../store/chat.js').ChatMessage }) {
  const isUser = message.role === 'user';
  const isThought = message.role === 'agent_thought';
  const isSystem = message.role === 'system';
  return (
    <Group justify={isUser ? 'flex-end' : 'flex-start'} align="flex-start">
      <Card
        radius="md"
        padding="sm"
        bg={isUser ? 'brand.7' : isSystem ? 'dark.6' : 'dark.7'}
        c={isUser ? 'white' : undefined}
        miw={isUser ? 100 : 200}
        maw="80%"
        style={{
          opacity: isThought ? 0.7 : 1,
          fontStyle: isThought ? 'italic' : undefined,
        }}
      >
        {isSystem ? (
          <Text size="xs" c="dimmed">
            {message.text}
          </Text>
        ) : (
          <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
            {message.text || (message.streaming ? '…' : '')}
          </Text>
        )}
      </Card>
    </Group>
  );
}

function ToolCallCard({ call }: { call: ChatToolCall }) {
  return (
    <Card padding="sm" radius="md" withBorder>
      <Group gap="xs">
        <IconTool size={14} />
        <Text size="sm" fw={500}>
          {call.name ?? '(tool)'}
        </Text>
        {call.status ? (
          <Badge size="xs" variant="light">
            {call.status}
          </Badge>
        ) : null}
      </Group>
      {call.input != null ? (
        <Box mt="xs">
          <Text size="xs" c="dimmed">
            input
          </Text>
          <Text size="xs" ff="monospace" style={{ whiteSpace: 'pre-wrap' }}>
            {safeStringify(call.input)}
          </Text>
        </Box>
      ) : null}
      {call.output != null ? (
        <Box mt="xs">
          <Text size="xs" c="dimmed">
            output
          </Text>
          <Text size="xs" ff="monospace" style={{ whiteSpace: 'pre-wrap' }}>
            {safeStringify(call.output)}
          </Text>
        </Box>
      ) : null}
    </Card>
  );
}

function PermissionCard({
  params,
  onAllow,
  onDeny,
}: {
  params: Record<string, unknown>;
  onAllow: () => void;
  onDeny: () => void;
}) {
  return (
    <Card padding="sm" radius="md" withBorder bg="yellow.9">
      <Group gap="xs">
        <IconShieldCheck size={14} />
        <Text size="sm" fw={600}>
          Permission requested
        </Text>
      </Group>
      <Text size="xs" ff="monospace" mt="xs" style={{ whiteSpace: 'pre-wrap' }}>
        {safeStringify(params)}
      </Text>
      <Group gap="xs" mt="xs">
        <Button
          size="xs"
          color="green"
          variant="filled"
          leftSection={<IconCheck size={12} />}
          onClick={onAllow}
        >
          Allow
        </Button>
        <Button
          size="xs"
          color="red"
          variant="light"
          leftSection={<IconX size={12} />}
          onClick={onDeny}
        >
          Deny
        </Button>
      </Group>
    </Card>
  );
}

function safeStringify(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}
