// Mobile port of apps/web/src/components/ShareAskMenu.tsx's ShareAskModal: mints
// a short-lived, read-only guest "ask" link for one project directory. The cwd
// is prefilled from wherever the modal is opened (a workspace or a session) and
// the agent/model/TTL/limits are all fixed at issuance. Shown link is copyable.
import { RelayError, type ChatAgentDescriptor } from '@coding-anywhere/protocol';
import { colors } from '@coding-anywhere/tokens';
import * as Clipboard from 'expo-clipboard';
import { SymbolView } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { useEffect, useMemo, useState } from 'react';
import { Modal, Pressable, ScrollView, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useChat } from '../store/chat';
import { useConnection } from '../store/connection';
import { DirectoryField } from './DirectoryPicker';
import { Button, ErrorBanner, SegmentedControl, TextField } from './ui';

function agentDisplayName(agentId: string): string {
  switch (agentId) {
    case 'devin':
    case '':
      return 'Devin';
    case 'cursor-cli':
      return 'Cursor';
    default:
      return agentId;
  }
}

function asString(value: unknown): string | null {
  return typeof value === 'string' && value.length > 0 ? value : null;
}

export function ShareAskModal({
  visible,
  onClose,
  defaultCwd = '',
  defaultAgentId,
}: {
  visible: boolean;
  onClose: () => void;
  defaultCwd?: string;
  defaultAgentId?: string;
}) {
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const insets = useSafeAreaInsets();

  const createAskShare = useConnection((s) => s.createAskShare);
  const status = useConnection((s) => s.status);
  const sessions = useChat((s) => s.sessions);
  const agents = useChat((s) => s.agents);
  const loadAgents = useChat((s) => s.loadAgents);

  const [agentId, setAgentId] = useState(defaultAgentId || 'devin');
  const [cwd, setCwd] = useState(defaultCwd);
  const [model, setModel] = useState<string>('');
  const [ttlMinutes, setTtlMinutes] = useState('30');
  const [promptLimit, setPromptLimit] = useState('20');
  const [maxSessions, setMaxSessions] = useState('1');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [link, setLink] = useState<string | null>(null);
  const [expiresAt, setExpiresAt] = useState<number | null>(null);
  const [copied, setCopied] = useState(false);

  // Re-seed the form each time the modal opens (cwd/agent come from the caller).
  useEffect(() => {
    if (!visible) return;
    setCwd(defaultCwd);
    if (defaultAgentId) setAgentId(defaultAgentId);
    setLink(null);
    setError(null);
    setExpiresAt(null);
    setCopied(false);
  }, [visible, defaultCwd, defaultAgentId]);

  useEffect(() => {
    if (!visible || status !== 'connected' || agents.length > 0) return;
    void loadAgents().catch(() => {});
  }, [visible, status, agents.length, loadAgents]);

  const availableAgents = useMemo(
    () => agents.filter((a: ChatAgentDescriptor) => a.available),
    [agents],
  );
  useEffect(() => {
    if (availableAgents.length === 0) return;
    if (!availableAgents.some((a) => a.id === agentId)) setAgentId(availableAgents[0]!.id);
  }, [availableAgents, agentId]);

  const agentName = agents.find((a) => a.id === agentId)?.name ?? agentDisplayName(agentId);

  // Reuse model choices advertised by an open session of the same agent.
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

  useEffect(() => setModel(''), [agentId]);

  async function onGenerate() {
    setError(null);
    setBusy(true);
    try {
      const res = await createAskShare({
        ttlSeconds: Math.max(1, Number(ttlMinutes) || 0) * 60,
        promptLimit: Math.max(0, Number(promptLimit) || 0),
        maxSessions: Math.max(1, Number(maxSessions) || 1),
        cwd: cwd.trim(),
        model,
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

  async function onCopy() {
    if (!link) return;
    await Clipboard.setStringAsync(link);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <Pressable className="flex-1 justify-end bg-black/40" onPress={onClose}>
        <Pressable
          onPress={(e) => e.stopPropagation()}
          className="max-h-[88%] rounded-t-xl bg-surface dark:bg-surface-elevated-dark"
          style={{ paddingBottom: Math.max(insets.bottom, 12) }}
        >
          <View className="items-center pt-sm">
            <View className="h-1 w-10 rounded-full bg-ink-300 dark:bg-ink-600" />
          </View>
          <View className="flex-row items-center gap-xs px-lg py-md">
            <SymbolView name="square.and.arrow.up" size={18} tintColor={t.text} />
            <Text className="flex-1 text-md font-bold text-text dark:text-text-dark">
              分享只读（ask）链接
            </Text>
            <Pressable onPress={onClose} hitSlop={8} className="active:opacity-60">
              <SymbolView name="xmark" size={16} tintColor={t.textMuted} />
            </Pressable>
          </View>

          <ScrollView
            contentContainerClassName="gap-md px-lg pb-lg"
            keyboardShouldPersistTaps="handled"
          >
            <View className="rounded-md border border-warning/40 bg-warning/10 p-md">
              <Text className="text-xs text-text dark:text-text-dark">
                拿到链接的人可以在 ask（只读）模式下与你的 {agentName} 对话。{agentName}{' '}
                不能改文件、不能跑命令，也看不到你其它的会话。链接到期或额度用尽后自动失效。
              </Text>
            </View>

            {link ? (
              <View className="gap-sm">
                <Text className="text-sm font-semibold text-text dark:text-text-dark">
                  分享链接
                </Text>
                <View className="rounded-md border border-border bg-surface-muted p-md dark:border-border-dark dark:bg-surface-muted-dark">
                  <Text selectable className="text-sm text-text dark:text-text-dark">
                    {link}
                  </Text>
                </View>
                {expiresAt ? (
                  <Text className="text-xs text-text-muted dark:text-text-muted-dark">
                    到期时间：{new Date(expiresAt).toLocaleString()}
                  </Text>
                ) : null}
                <Button title={copied ? '已复制' : '复制链接'} onPress={() => void onCopy()} />
                <Button title="再建一个" variant="default" onPress={() => setLink(null)} />
              </View>
            ) : (
              <View className="gap-md">
                {availableAgents.length > 1 ? (
                  <View className="gap-xs">
                    <Text className="text-sm font-semibold text-text dark:text-text-dark">
                      Agent
                    </Text>
                    <SegmentedControl
                      value={agentId}
                      onChange={setAgentId}
                      options={availableAgents.map((a) => ({ value: a.id, label: a.name }))}
                    />
                  </View>
                ) : null}

                <DirectoryField
                  label="工作目录"
                  description={`必填。访客的 ${agentName} 会在这个目录里运行。访客无法更改它。`}
                  value={cwd}
                  onChange={setCwd}
                />

                {modelChoices.length > 0 ? (
                  <View className="gap-xs">
                    <Text className="text-sm font-semibold text-text dark:text-text-dark">
                      模型（可选）
                    </Text>
                    <View className="flex-row flex-wrap gap-xs">
                      <ModelChip label="默认" active={model === ''} onPress={() => setModel('')} />
                      {modelChoices.map((c) => (
                        <ModelChip
                          key={c.value}
                          label={c.label}
                          active={model === c.value}
                          onPress={() => setModel(c.value)}
                        />
                      ))}
                    </View>
                  </View>
                ) : null}

                <View className="flex-row gap-md">
                  <View className="flex-1">
                    <TextField
                      label="有效期(分钟)"
                      value={ttlMinutes}
                      onChangeText={setTtlMinutes}
                      keyboardType="number-pad"
                    />
                  </View>
                  <View className="flex-1">
                    <TextField
                      label="提问上限(0不限)"
                      value={promptLimit}
                      onChangeText={setPromptLimit}
                      keyboardType="number-pad"
                    />
                  </View>
                  <View className="flex-1">
                    <TextField
                      label="会话数上限"
                      value={maxSessions}
                      onChangeText={setMaxSessions}
                      keyboardType="number-pad"
                    />
                  </View>
                </View>

                <ErrorBanner message={error} />
                <Button
                  title="生成链接"
                  loading={busy}
                  disabled={!cwd.trim()}
                  onPress={() => void onGenerate()}
                />
              </View>
            )}
          </ScrollView>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

function ModelChip({
  label,
  active,
  onPress,
}: {
  label: string;
  active: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={`rounded-full border px-md py-1.5 active:opacity-70 ${
        active
          ? 'border-accent bg-accent-soft dark:border-accent-dark dark:bg-accent-soft-dark'
          : 'border-border dark:border-border-dark'
      }`}
    >
      <Text
        className={
          active
            ? 'text-xs font-semibold text-accent dark:text-accent-dark'
            : 'text-xs text-text dark:text-text-dark'
        }
      >
        {label}
      </Text>
    </Pressable>
  );
}

function formatShareError(err: unknown): string {
  if (err instanceof RelayError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return '生成失败，请重试。';
}
