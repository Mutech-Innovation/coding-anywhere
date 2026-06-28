import {
  type AvailableCommand,
  type ChatConfigOption,
  type ChatUsage,
} from '@coding-anywhere/protocol';
import { colors } from '@coding-anywhere/tokens';
import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { SymbolView } from 'expo-symbols';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useColorScheme } from 'nativewind';
import {
  Keyboard,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';

import {
  MessageBubble,
  PermissionCard,
  PlanCard,
  StatusBadge,
  ToolCallCard,
  ToolCallGroupCard,
} from '../../components/chat';
import { SegmentedControl } from '../../components/ui';
import { useChat } from '../../store/chat';
import { groupTranscript, type TranscriptEntry } from '../../components/transcript';

// Tracks keyboard visibility so the composer can drop its bottom safe-area
// padding while the keyboard is up (KeyboardAvoidingView already lifts it above
// the keyboard, which covers the home-indicator area).
function useKeyboardShown(): boolean {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const show = Keyboard.addListener('keyboardWillShow', () => setShown(true));
    const hide = Keyboard.addListener('keyboardWillHide', () => setShown(false));
    return () => {
      show.remove();
      hide.remove();
    };
  }, []);
  return shown;
}

function choiceValue(c: { value?: unknown }): string {
  return typeof c.value === 'string' ? c.value : String(c.value ?? '');
}
function choiceLabel(c: { value?: unknown; label?: string; name?: string }): string {
  return c.label ?? c.name ?? choiceValue(c);
}

// Mode names can be long (e.g. "Bypass Permissions") and wrap on the narrow
// segmented control; show just the first word (Code / Ask / Plan / Bypass).
function shortMode(name: string): string {
  return name.split(/\s+/)[0] || name;
}

export default function ChatScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const session = useChat((s) => (id ? s.sessions[id] : undefined));
  const sendPrompt = useChat((s) => s.sendPrompt);
  const cancel = useChat((s) => s.cancel);
  const setMode = useChat((s) => s.setMode);
  const setConfigOption = useChat((s) => s.setConfigOption);
  const resolvePermission = useChat((s) => s.resolvePermission);
  const refreshHistory = useChat((s) => s.refreshHistory);

  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<ScrollView | null>(null);
  const insets = useSafeAreaInsets();
  const modesFetchedRef = useRef<string | null>(null);

  // Sessions learned about via a session_created broadcast (created on another
  // device, or before this client connected) arrive without modes/config — those
  // ride the create_session response or get_history, not the live events. When we
  // open such a session and it has no modes yet, pull its history once so the mode
  // picker + model option populate.
  useEffect(() => {
    if (!id || !session || session.session.origin === 'guest') return;
    if (session.modes.length > 0) return;
    if (modesFetchedRef.current === id) return;
    modesFetchedRef.current = id;
    void refreshHistory(id).catch(() => {});
  }, [id, session, refreshHistory]);

  const transcriptItems = useMemo(() => {
    const entries: TranscriptEntry[] = [
      ...(session?.messages ?? []).map((message) => ({
        type: 'message' as const,
        order: message.order,
        message,
      })),
      ...Object.values(session?.toolCalls ?? {}).map((call) => ({
        type: 'tool' as const,
        order: call.order,
        call,
      })),
    ].sort((a, b) => a.order - b.order);
    return groupTranscript(entries);
  }, [session?.messages, session?.toolCalls]);

  useEffect(() => {
    const t = setTimeout(() => scrollRef.current?.scrollToEnd({ animated: true }), 50);
    return () => clearTimeout(t);
  }, [transcriptItems.length, session?.session.status, session?.plan?.length]);

  if (!id || !session) {
    return (
      <SafeAreaView className="flex-1 items-center justify-center bg-surface dark:bg-surface-dark">
        <Text className="text-sm text-text-muted dark:text-text-muted-dark">会话不存在</Text>
      </SafeAreaView>
    );
  }

  const s = session.session;
  const status = s.status;
  const isGuest = s.origin === 'guest';
  const pending = Object.values(session.permissions).filter((p) => p.pending);
  const modelOption = session.configOptions.find((o) => o.id.toLowerCase() === 'model') ?? null;

  async function onSend() {
    if (!draft.trim()) return;
    setError(null);
    setBusy(true);
    const text = draft;
    setDraft('');
    try {
      await sendPrompt(id!, text);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
      <Stack.Screen options={{ headerShown: false }} />
      <KeyboardAvoidingView
        className="flex-1"
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        keyboardVerticalOffset={insets.top}
      >
        {/* Header */}
        <View className="flex-row items-center gap-sm border-b border-border px-md py-sm dark:border-border-dark">
          <Pressable onPress={() => router.back()} className="p-1 active:opacity-60">
            <SymbolView name="chevron.left" size={20} tintColor="#3d54ee" />
          </Pressable>
          <View className="flex-1">
            <Text className="text-md font-semibold text-text dark:text-text-dark" numberOfLines={1}>
              {s.title || s.cwd || '会话'}
            </Text>
            <Text className="text-xs text-text-muted dark:text-text-muted-dark" numberOfLines={1}>
              {s.agent_id} · {s.cwd}
            </Text>
          </View>
          <StatusBadge status={status} />
        </View>

        {/* Mode picker */}
        {session.modes.length > 0 && !isGuest ? (
          <View className="px-md pt-sm">
            <SegmentedControl
              value={session.currentModeId ?? session.modes[0]!.id}
              onChange={(v) => void setMode(id!, v)}
              options={session.modes.map((m) => ({ value: m.id, label: shortMode(m.name) }))}
            />
          </View>
        ) : null}

        <ScrollView
          ref={scrollRef}
          contentContainerClassName="gap-sm p-md"
          onContentSizeChange={() => scrollRef.current?.scrollToEnd({ animated: false })}
        >
          {session.plan && session.plan.length > 0 ? <PlanCard entries={session.plan} /> : null}
          {transcriptItems.map((item) =>
            item.type === 'message' ? (
              <MessageBubble key={item.key} message={item.message} />
            ) : item.type === 'tool' ? (
              <ToolCallCard key={item.key} call={item.call} />
            ) : (
              <ToolCallGroupCard key={item.key} calls={item.calls} />
            ),
          )}
          {pending.map((p) => (
            <PermissionCard
              key={p.requestId}
              params={p.params}
              onResolve={(decision) => void resolvePermission(id!, p.requestId, decision)}
            />
          ))}
        </ScrollView>

        {error ? <Text className="px-md pb-1 text-xs text-danger">{error}</Text> : null}

        {isGuest ? (
          <View className="border-t border-border p-md dark:border-border-dark">
            <Text className="text-xs text-text-muted dark:text-text-muted-dark">
              访客会话只读，无法发送。
            </Text>
          </View>
        ) : (
          <Composer
            draft={draft}
            onChangeDraft={setDraft}
            onSend={onSend}
            busy={busy}
            thinking={status === 'thinking'}
            onCancel={() => void cancel(id!)}
            commands={session.availableCommands}
            modelOption={modelOption}
            onModelChange={(v) =>
              modelOption ? void setConfigOption(id!, modelOption.id, v) : undefined
            }
            usage={session.usage}
          />
        )}
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

// ---- Composer -------------------------------------------------------------

function Composer({
  draft,
  onChangeDraft,
  onSend,
  busy,
  thinking,
  onCancel,
  commands,
  modelOption,
  onModelChange,
  usage,
}: {
  draft: string;
  onChangeDraft: (v: string) => void;
  onSend: () => void;
  busy: boolean;
  thinking: boolean;
  onCancel: () => void;
  commands: AvailableCommand[];
  modelOption: ChatConfigOption | null;
  onModelChange: (value: string) => void;
  usage: ChatUsage | null;
}) {
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const insets = useSafeAreaInsets();
  const kbShown = useKeyboardShown();
  const [modelOpen, setModelOpen] = useState(false);

  // Slash-command typeahead: a leading "/word" with no space yet.
  const slashMatch = /^\/(\w*)$/.exec(draft);
  const suggestions =
    slashMatch && commands.length > 0
      ? commands.filter((c) => c.name.startsWith(slashMatch[1]!)).slice(0, 5)
      : [];

  const modelChoices = modelOption?.options ?? [];
  const currentModel =
    typeof modelOption?.currentValue === 'string' ? modelOption.currentValue : undefined;
  const currentModelLabel = modelChoices.find((c) => choiceValue(c) === currentModel) ?? null;

  return (
    <View
      className="border-t border-border bg-surface px-md pt-sm dark:border-border-dark dark:bg-surface-dark"
      style={{ paddingBottom: kbShown ? 8 : Math.max(insets.bottom, 8) }}
    >
      {/* Slash suggestions */}
      {suggestions.length > 0 ? (
        <View className="mb-sm overflow-hidden rounded-md border border-border dark:border-border-dark">
          {suggestions.map((c) => (
            <Pressable
              key={c.name}
              onPress={() => onChangeDraft(`/${c.name} `)}
              className="border-b border-border px-md py-sm last:border-b-0 active:opacity-70 dark:border-border-dark"
            >
              <Text className="text-sm font-semibold text-text dark:text-text-dark">/{c.name}</Text>
              {c.description ? (
                <Text
                  className="text-xs text-text-muted dark:text-text-muted-dark"
                  numberOfLines={1}
                >
                  {c.description}
                </Text>
              ) : null}
            </Pressable>
          ))}
        </View>
      ) : null}

      {/* Meta row: model + usage */}
      <View className="mb-sm flex-row items-center gap-md">
        {modelOption ? (
          <Pressable
            onPress={() => setModelOpen(true)}
            className="flex-row items-center gap-xs rounded-md border border-border px-sm py-1 active:opacity-70 dark:border-border-dark"
          >
            <SymbolView name="cpu" size={12} tintColor={t.textMuted} />
            <Text className="text-xs text-text-muted dark:text-text-muted-dark">
              {currentModelLabel ? choiceLabel(currentModelLabel) : '模型'}
            </Text>
          </Pressable>
        ) : null}
        {usage ? (
          <Text className="text-xs text-text-muted dark:text-text-muted-dark">
            {usage.used}/{usage.size}
            {usage.cost ? ` · ${usage.cost.amount}${usage.cost.currency}` : ''}
          </Text>
        ) : null}
      </View>

      {/* Input row */}
      <View className="flex-row items-end gap-sm">
        <TextInput
          className="max-h-32 flex-1 rounded-md border border-border bg-surface px-md py-sm text-md text-text dark:border-border-dark dark:bg-surface-elevated-dark dark:text-text-dark"
          placeholder="问问 agent…"
          placeholderTextColor="#9aa1ac"
          value={draft}
          onChangeText={onChangeDraft}
          multiline
        />
        {thinking ? (
          <Pressable
            onPress={onCancel}
            className="h-11 w-11 items-center justify-center rounded-md bg-danger active:opacity-80"
          >
            <SymbolView name="stop.fill" size={16} tintColor="#ffffff" />
          </Pressable>
        ) : (
          <Pressable
            onPress={onSend}
            disabled={busy || !draft.trim()}
            className={`h-11 w-11 items-center justify-center rounded-md bg-ink-900 dark:bg-ink-100 ${
              busy || !draft.trim() ? 'opacity-50' : 'active:opacity-80'
            }`}
          >
            <SymbolView
              name="arrow.up"
              size={18}
              tintColor={scheme === 'dark' ? '#0e0e10' : '#ffffff'}
            />
          </Pressable>
        )}
      </View>

      {/* Model picker modal */}
      <Modal
        visible={modelOpen}
        transparent
        animationType="fade"
        onRequestClose={() => setModelOpen(false)}
      >
        <Pressable className="flex-1 justify-end bg-black/40" onPress={() => setModelOpen(false)}>
          <Pressable
            className="max-h-[60%] rounded-t-xl bg-surface p-lg dark:bg-surface-elevated-dark"
            onPress={(e) => e.stopPropagation()}
          >
            <Text className="mb-sm text-md font-bold text-text dark:text-text-dark">选择模型</Text>
            <ScrollView>
              {modelChoices.map((c) => {
                const v = choiceValue(c);
                const active = v === currentModel;
                return (
                  <Pressable
                    key={v}
                    onPress={() => {
                      onModelChange(v);
                      setModelOpen(false);
                    }}
                    className="flex-row items-center justify-between border-b border-border py-md active:opacity-70 dark:border-border-dark"
                  >
                    <Text
                      className={`text-sm ${
                        active
                          ? 'font-semibold text-accent dark:text-accent-dark'
                          : 'text-text dark:text-text-dark'
                      }`}
                    >
                      {choiceLabel(c)}
                    </Text>
                    {active ? <SymbolView name="checkmark" size={14} tintColor={t.accent} /> : null}
                  </Pressable>
                );
              })}
            </ScrollView>
          </Pressable>
        </Pressable>
      </Modal>
    </View>
  );
}
