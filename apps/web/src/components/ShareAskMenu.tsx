import {
  Alert,
  Button,
  CopyButton,
  Group,
  Menu,
  Modal,
  NumberInput,
  SegmentedControl,
  Select,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconShare3 } from '@tabler/icons-react';
import { useEffect, useMemo, useState } from 'react';

import { RelayError } from '@coding-anywhere/protocol';

import { useChat } from '../store/chat.js';
import { useConnection } from '../store/connection.js';
import { DirectoryPicker } from './ChatPanel.js';

/**
 * Menu item that opens the "share a read-only ask link" modal. Like
 * AccountKeyMenu, it only flips a store flag (the Menu.Dropdown unmounts on
 * click) — the modal itself lives in {@link ShareAskModal}.
 */
export function ShareAskMenu() {
  const setShareAskOpen = useConnection((s) => s.setShareAskOpen);
  return (
    <Menu.Item leftSection={<IconShare3 size={14} />} onClick={() => setShareAskOpen(true)}>
      分享只读（ask）链接
    </Menu.Item>
  );
}

/**
 * Modal that mints a guest "ask"-mode share link. Render once, as a sibling to
 * the header menu (see App.tsx), so it survives the dropdown closing. TTL,
 * prompt limit, and session count are all fixed at generation time.
 */
export function ShareAskModal() {
  const opened = useConnection((s) => s.shareAskOpen);
  const setOpen = useConnection((s) => s.setShareAskOpen);
  const createAskShare = useConnection((s) => s.createAskShare);
  const status = useConnection((s) => s.status);

  const sessions = useChat((s) => s.sessions);
  const agents = useChat((s) => s.agents);
  const loadAgents = useChat((s) => s.loadAgents);

  const [agentId, setAgentId] = useState('devin');
  const [cwd, setCwd] = useState('');
  const [model, setModel] = useState<string | null>(null);
  const [ttlMinutes, setTtlMinutes] = useState<number | string>(30);
  const [promptLimit, setPromptLimit] = useState<number | string>(20);
  const [maxSessions, setMaxSessions] = useState<number | string>(1);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [link, setLink] = useState<string | null>(null);
  const [expiresAt, setExpiresAt] = useState<number | null>(null);

  // The modal can open before the chat tab ever loaded the agent list.
  useEffect(() => {
    if (!opened || status !== 'connected' || agents.length > 0) return;
    void loadAgents().catch(() => {});
  }, [opened, status, agents.length, loadAgents]);

  const availableAgents = useMemo(() => agents.filter((a) => a.available), [agents]);
  useEffect(() => {
    if (availableAgents.length === 0) return;
    if (!availableAgents.some((a) => a.id === agentId)) {
      setAgentId(availableAgents[0]!.id);
    }
  }, [availableAgents, agentId]);

  const agentName = agents.find((a) => a.id === agentId)?.name ?? agentDisplayName(agentId);

  // Model choices are advertised per-session by the agent; reuse them from an
  // open session of the SAME agent (model ids differ per agent) so the owner
  // can lock a model for the guest. Empty when no such session has surfaced a
  // model option yet (guest then uses the agent's default).
  const modelChoices = useMemo(() => {
    for (const s of Object.values(sessions)) {
      if (s.session.agent_id !== agentId) continue;
      const opt = s.configOptions.find((o) => o.id.toLowerCase() === 'model');
      if (opt?.options?.length) {
        const seen = new Set<string>();
        const out: { value: string; label: string }[] = [];
        for (const c of opt.options) {
          const value = c.value == null ? '' : String(c.value);
          if (!value || seen.has(value)) continue;
          seen.add(value);
          out.push({ value, label: asString(c.label) ?? asString(c.name) ?? value });
        }
        return out;
      }
    }
    return [];
  }, [sessions, agentId]);

  // A model locked for one agent is meaningless for another.
  useEffect(() => {
    setModel(null);
  }, [agentId]);

  function reset() {
    setLink(null);
    setError(null);
    setExpiresAt(null);
  }

  function close() {
    if (busy) return;
    setOpen(false);
    // Defer reset so the modal content doesn't flash while closing.
    setTimeout(reset, 200);
  }

  async function onGenerate() {
    setError(null);
    setBusy(true);
    try {
      const res = await createAskShare({
        ttlSeconds: Math.max(1, Number(ttlMinutes) || 0) * 60,
        promptLimit: Math.max(0, Number(promptLimit) || 0),
        maxSessions: Math.max(1, Number(maxSessions) || 1),
        cwd: cwd.trim(),
        model: model ?? '',
        agentId,
      });
      setLink(res.url);
      setExpiresAt(res.expiresAt);
    } catch (err) {
      setError(formatShareError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      opened={opened}
      onClose={close}
      centered
      size="lg"
      title={
        <Group gap={6}>
          <IconShare3 size={18} />
          <Text fw={700}>分享只读（ask）链接</Text>
        </Group>
      }
    >
      <Stack gap="md">
        <Alert color="yellow" variant="light">
          拿到链接的人可以在 <b>ask（只读）</b>模式下与你的 {agentName} 对话。{agentName}{' '}
          不能改文件、不能跑命令
          （权限请求会自动拒绝），也看不到你其它的会话。链接到期或额度用尽后自动失效。
        </Alert>

        {link ? (
          <Stack gap="sm">
            <TextInput
              label="分享链接"
              value={link}
              readOnly
              onFocus={(e) => e.currentTarget.select()}
            />
            {expiresAt ? (
              <Text size="xs" c="dimmed">
                到期时间：{new Date(expiresAt).toLocaleString()}
              </Text>
            ) : null}
            <Group justify="flex-end">
              <CopyButton value={link} timeout={1500}>
                {({ copied, copy }) => (
                  <Tooltip label={copied ? '已复制' : '点击复制'} withArrow>
                    <Button variant="default" onClick={copy}>
                      {copied ? '已复制' : '复制链接'}
                    </Button>
                  </Tooltip>
                )}
              </CopyButton>
              <Button variant="subtle" onClick={reset}>
                再建一个
              </Button>
            </Group>
          </Stack>
        ) : (
          <Stack gap="sm">
            {availableAgents.length > 1 ? (
              <Stack gap={4}>
                <Text size="sm" fw={500}>
                  Agent
                </Text>
                <SegmentedControl
                  value={agentId}
                  onChange={setAgentId}
                  data={availableAgents.map((a) => ({ value: a.id, label: a.name }))}
                />
                <Text size="xs" c="dimmed">
                  访客的会话固定使用这个 agent，无法更改。
                </Text>
              </Stack>
            ) : null}
            <TextInput
              label="工作目录"
              description={`必填。访客的 ${agentName} 会在这个目录里运行 —— 请选一个具体的项目目录（不会在 $HOME 这类宽泛根目录上工作）。访客无法更改它。`}
              placeholder="选择下方的一个目录"
              value={cwd}
              onChange={(e) => setCwd(e.currentTarget.value)}
            />
            <DirectoryPicker opened={opened} selectedPath={cwd} onSelect={setCwd} />
            <Select
              label="模型"
              description={
                modelChoices.length > 0
                  ? `可选。锁定访客使用的模型；留空则用 ${agentName} 默认模型。访客无法更改。`
                  : `打开一个 ${agentName} 会话后这里才会出现可选模型；当前留空，访客将使用 ${agentName} 默认模型。`
              }
              placeholder={modelChoices.length > 0 ? '默认模型' : '（无可选模型）'}
              data={modelChoices}
              value={model}
              onChange={setModel}
              disabled={modelChoices.length === 0}
              clearable
              searchable
              nothingFoundMessage="没有匹配的模型"
            />
            <NumberInput
              label="有效期（分钟）"
              min={1}
              value={ttlMinutes}
              onChange={setTtlMinutes}
            />
            <NumberInput
              label="提问次数上限（0 表示不限）"
              min={0}
              value={promptLimit}
              onChange={setPromptLimit}
            />
            <NumberInput label="会话数上限" min={1} value={maxSessions} onChange={setMaxSessions} />
            {error ? (
              <Alert color="red" variant="light">
                {error}
              </Alert>
            ) : null}
            <Group justify="flex-end">
              <Button variant="default" onClick={close} disabled={busy}>
                取消
              </Button>
              <Button onClick={() => void onGenerate()} loading={busy} disabled={!cwd.trim()}>
                生成链接
              </Button>
            </Group>
          </Stack>
        )}
      </Stack>
    </Modal>
  );
}

function formatShareError(err: unknown): string {
  if (err instanceof RelayError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return '生成失败，请重试。';
}

// asString narrows an unknown config-option field to a non-empty string.
function asString(value: unknown): string | null {
  return typeof value === 'string' && value.length > 0 ? value : null;
}

// agentDisplayName is the fallback rendering of an agent id before the
// daemon's chat.agents response has arrived.
function agentDisplayName(agentId: string): string {
  switch (agentId) {
    case 'devin':
      return 'Devin';
    case 'cursor-cli':
      return 'Cursor';
    default:
      return agentId;
  }
}
