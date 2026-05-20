import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Code,
  Group,
  List,
  Menu,
  Modal,
  Paper,
  Progress,
  ScrollArea,
  SegmentedControl,
  Stack,
  Text,
  Textarea,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import type {
  AvailableCommand,
  PlanEntry,
  ToolCallContentBlock,
} from '@coding-anywhere/protocol';
import {
  IconArrowRight,
  IconBolt,
  IconBrain,
  IconCheck,
  IconCircle,
  IconCircleDashed,
  IconCirclePlus,
  IconCommand,
  IconEdit,
  IconFileSearch,
  IconFileText,
  IconMessageCircle,
  IconPlayerStop,
  IconRoute,
  IconSearch,
  IconSend,
  IconShieldCheck,
  IconTerminal2,
  IconTool,
  IconTrash,
  IconWorld,
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
  const setMode = useChat((s) => s.setMode);
  const resolvePermission = useChat((s) => s.resolvePermission);
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Auto-scroll on new content.
  useEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [
    session?.messages.length,
    session?.session.status,
    Object.keys(session?.toolCalls ?? {}).length,
    session?.plan?.length,
  ]);

  // Sort tool calls by insertion order so the timeline is stable.
  const orderedToolCalls = useMemo(
    () => Object.values(session?.toolCalls ?? {}).sort((a, b) => a.order - b.order),
    [session?.toolCalls],
  );

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
          {session.modes.length > 0 ? (
            <SegmentedControl
              size="xs"
              data={session.modes.map((m) => ({ label: m.name, value: m.id }))}
              value={session.currentModeId ?? session.modes[0]?.id}
              onChange={(value) => void setMode(sessionId, value)}
              transitionDuration={0}
            />
          ) : null}
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

      {session.plan && session.plan.length > 0 ? <PlanCard entries={session.plan} /> : null}

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
          {orderedToolCalls.map((tc) => (
            <ToolCallCard key={tc.id} call={tc} />
          ))}
        </Stack>
      </ScrollArea>

      {error ? (
        <Alert color="red" variant="light" radius="md">
          {error}
        </Alert>
      ) : null}

      <CommandInput
        commands={session.availableCommands}
        value={draft}
        onChange={setDraft}
        onSubmit={onSend}
        busy={busy}
      />
    </Stack>
  );
}

function PlanCard({ entries }: { entries: PlanEntry[] }) {
  const total = entries.length || 1;
  const done = entries.filter((e) => e.status === 'completed').length;
  const inProgress = entries.find((e) => e.status === 'in_progress');
  return (
    <Paper p="xs" radius="md" withBorder>
      <Group justify="space-between" mb={6}>
        <Group gap="xs">
          <ThemeIcon size="sm" variant="light" color="brand">
            <IconRoute size={14} />
          </ThemeIcon>
          <Text fw={600} size="sm">
            Plan
          </Text>
          <Text size="xs" c="dimmed">
            {done}/{entries.length} done
          </Text>
        </Group>
        {inProgress ? (
          <Text size="xs" c="dimmed" lineClamp={1}>
            {inProgress.content}
          </Text>
        ) : null}
      </Group>
      <Progress value={(done / total) * 100} size="xs" radius="xs" mb={6} color="brand" />
      <List size="xs" spacing={4} icon={<IconCircle size={10} />} center>
        {entries.map((e, i) => (
          <List.Item
            key={i}
            icon={planIcon(e.status)}
            style={{
              opacity: e.status === 'completed' ? 0.6 : 1,
              textDecoration: e.status === 'completed' ? 'line-through' : undefined,
              fontWeight: e.status === 'in_progress' ? 600 : 400,
            }}
          >
            {e.content}
            {e.priority === 'high' ? (
              <Badge size="xs" color="red" variant="light" ml={6}>
                high
              </Badge>
            ) : null}
          </List.Item>
        ))}
      </List>
    </Paper>
  );
}

function planIcon(status: PlanEntry['status']) {
  switch (status) {
    case 'completed':
      return (
        <ThemeIcon size="xs" color="green" variant="light">
          <IconCheck size={10} />
        </ThemeIcon>
      );
    case 'in_progress':
      return (
        <ThemeIcon size="xs" color="yellow" variant="light">
          <IconCircleDashed size={10} />
        </ThemeIcon>
      );
    default:
      return (
        <ThemeIcon size="xs" color="gray" variant="light">
          <IconCircle size={10} />
        </ThemeIcon>
      );
  }
}

function CommandInput({
  commands,
  value,
  onChange,
  onSubmit,
  busy,
}: {
  commands: AvailableCommand[];
  value: string;
  onChange: (v: string) => void;
  onSubmit: () => void | Promise<void>;
  busy: boolean;
}) {
  // Show the typeahead only when the user has typed `/` as the very first
  // non-whitespace character and the cursor sits in the command word.
  const trimmed = value.trimStart();
  const slashOpen = trimmed.startsWith('/') && !trimmed.includes(' ');
  const filter = slashOpen ? trimmed.slice(1).toLowerCase() : '';
  const matches = useMemo(
    () =>
      slashOpen
        ? commands.filter((c) => c.name.toLowerCase().startsWith(filter)).slice(0, 8)
        : [],
    [commands, slashOpen, filter],
  );
  const ref = useRef<HTMLTextAreaElement | null>(null);
  return (
    <Stack gap={4} px="xs">
      {matches.length > 0 ? (
        <Paper withBorder radius="md" p="xs">
          <Stack gap={2}>
            {matches.map((c) => (
              <Group
                key={c.name}
                gap="xs"
                py={4}
                px={6}
                style={{ cursor: 'pointer', borderRadius: 4 }}
                onClick={() => {
                  onChange(`/${c.name}${c.input ? ' ' : ''}`);
                  ref.current?.focus();
                }}
              >
                <ThemeIcon size="xs" variant="light">
                  <IconCommand size={10} />
                </ThemeIcon>
                <Text size="xs" fw={600}>
                  /{c.name}
                </Text>
                <Text size="xs" c="dimmed" truncate>
                  {c.description}
                  {c.input?.hint ? <Text component="span" c="dimmed">{` — ${c.input.hint}`}</Text> : null}
                </Text>
              </Group>
            ))}
          </Stack>
        </Paper>
      ) : null}
      <Group gap="xs" align="flex-end">
        <Textarea
          ref={ref}
          placeholder="Ask Devin... (try /plan, /clear, /fork)"
          autosize
          minRows={1}
          maxRows={6}
          value={value}
          style={{ flex: 1 }}
          onChange={(e) => onChange(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              void onSubmit();
            }
          }}
        />
        <Button leftSection={<IconSend size={14} />} onClick={() => void onSubmit()} loading={busy}>
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

const TOOL_STATUS_COLOR: Record<string, string> = {
  pending: 'gray',
  in_progress: 'yellow',
  completed: 'green',
  failed: 'red',
};

function ToolCallCard({ call }: { call: ChatToolCall }) {
  const [expanded, setExpanded] = useState(false);
  const title = call.title ?? humanizeToolFromInput(call) ?? '(tool)';
  const status = call.status ?? 'pending';
  const hasDetails =
    call.rawInput != null ||
    call.rawOutput != null ||
    (call.content && call.content.length > 0);
  return (
    <Card padding="sm" radius="md" withBorder>
      <Group gap="xs" justify="space-between" wrap="nowrap">
        <Group gap="xs" wrap="nowrap" style={{ minWidth: 0, flex: 1 }}>
          <ThemeIcon size="sm" variant="light" color={TOOL_STATUS_COLOR[status] ?? 'gray'}>
            {iconForKind(call.kind)}
          </ThemeIcon>
          <Text size="sm" fw={500} truncate>
            {title}
          </Text>
          <Badge size="xs" color={TOOL_STATUS_COLOR[status] ?? 'gray'} variant="light">
            {status}
          </Badge>
        </Group>
        {hasDetails ? (
          <Tooltip label={expanded ? 'Hide details' : 'Show details'}>
            <ActionIcon variant="subtle" size="sm" onClick={() => setExpanded((x) => !x)}>
              <IconArrowRight
                size={12}
                style={{ transform: expanded ? 'rotate(90deg)' : undefined, transition: 'transform 120ms' }}
              />
            </ActionIcon>
          </Tooltip>
        ) : null}
      </Group>
      {call.locations && call.locations.length > 0 ? (
        <Group gap={4} mt={6} wrap="wrap">
          {call.locations.map((loc, i) => (
            <Badge key={i} size="xs" variant="light" color="blue" leftSection={<IconFileText size={10} />}>
              {shortenPath(loc.path)}
              {loc.line ? `:${loc.line}` : ''}
            </Badge>
          ))}
        </Group>
      ) : null}
      {expanded ? (
        <Stack gap="xs" mt="sm">
          {call.content?.map((c, i) => <ToolContentBlockView key={i} block={c} />)}
          {call.rawInput != null ? (
            <Box>
              <Text size="xs" c="dimmed">
                input
              </Text>
              <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 11 }}>
                {safeStringify(call.rawInput)}
              </Code>
            </Box>
          ) : null}
          {call.rawOutput != null ? (
            <Box>
              <Text size="xs" c="dimmed">
                output
              </Text>
              <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 11 }}>
                {safeStringify(call.rawOutput)}
              </Code>
            </Box>
          ) : null}
        </Stack>
      ) : null}
    </Card>
  );
}

function iconForKind(kind: ChatToolCall['kind']) {
  switch (kind) {
    case 'read':
      return <IconFileSearch size={12} />;
    case 'edit':
      return <IconEdit size={12} />;
    case 'delete':
      return <IconTrash size={12} />;
    case 'search':
      return <IconSearch size={12} />;
    case 'execute':
      return <IconTerminal2 size={12} />;
    case 'fetch':
      return <IconWorld size={12} />;
    case 'think':
      return <IconBrain size={12} />;
    case 'switch_mode':
      return <IconBolt size={12} />;
    case 'move':
      return <IconArrowRight size={12} />;
    default:
      return <IconTool size={12} />;
  }
}

/** Best-effort title from rawInput when the agent didn't send one. */
function humanizeToolFromInput(call: ChatToolCall): string | null {
  if (!call.rawInput || typeof call.rawInput !== 'object') return null;
  const input = call.rawInput as Record<string, unknown>;
  if (typeof input.command === 'string') return `$ ${input.command}`;
  if (typeof input.path === 'string') return input.path as string;
  if (typeof input.file_path === 'string') return input.file_path as string;
  if (typeof input.url === 'string') return input.url as string;
  if (typeof input.query === 'string') return `“${input.query}”`;
  return null;
}

function shortenPath(p: string): string {
  if (p.length <= 40) return p;
  const idx = p.lastIndexOf('/');
  if (idx < 0) return p.slice(-40);
  return '…/' + p.slice(idx + 1);
}

function ToolContentBlockView({ block }: { block: ToolCallContentBlock }) {
  if (block.type === 'content') {
    const inner = (block as { content?: { type?: string; text?: string } }).content;
    if (inner?.type === 'text' && typeof inner.text === 'string') {
      return (
        <Text size="xs" ff="monospace" style={{ whiteSpace: 'pre-wrap' }}>
          {inner.text}
        </Text>
      );
    }
  }
  if (block.type === 'diff') {
    const d = block as { path: string; oldText?: string | null; newText: string };
    return (
      <Box>
        <Text size="xs" c="dimmed">
          diff · {d.path}
        </Text>
        <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 11 }}>
          {d.newText}
        </Code>
      </Box>
    );
  }
  if (block.type === 'terminal') {
    const t = block as { terminalId: string };
    return (
      <Group gap="xs">
        <IconTerminal2 size={12} />
        <Text size="xs" c="dimmed">
          terminal {t.terminalId}
        </Text>
      </Group>
    );
  }
  return (
    <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 11 }}>
      {safeStringify(block)}
    </Code>
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
