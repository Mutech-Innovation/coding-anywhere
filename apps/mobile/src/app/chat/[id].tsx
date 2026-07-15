import { type ChatAttachment } from '@coding-anywhere/protocol';
import { colors } from '@coding-anywhere/tokens';
import * as Clipboard from 'expo-clipboard';
import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { SymbolView } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  Text,
  View,
} from 'react-native';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';

import {
  MessageBubble,
  PermissionCard,
  PlanCard,
  ToolCallCard,
  ToolCallGroupCard,
} from '../../components/chat';
import { Composer } from '../../components/Composer';
import { ShareAskModal } from '../../components/ShareAskModal';
import { SegmentedControl } from '../../components/ui';
import { useChat } from '../../store/chat';
import { groupTranscript, type TranscriptEntry } from '../../components/transcript';

const STATUS_HINT: Record<string, string> = {
  thinking: '思考中…',
  awaiting_permission: '待授权',
  closed: '已关闭',
};

// Mode names can be long (e.g. "Bypass Permissions"); show just the first word
// so they fit the segmented control.
function shortMode(name: string): string {
  return name.split(/\s+/)[0] || name;
}

export default function ChatScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const session = useChat((s) => (id ? s.sessions[id] : undefined));
  const sendPrompt = useChat((s) => s.sendPrompt);
  const cancel = useChat((s) => s.cancel);
  const setMode = useChat((s) => s.setMode);
  const setConfigOption = useChat((s) => s.setConfigOption);
  const resolvePermission = useChat((s) => s.resolvePermission);
  const refreshHistory = useChat((s) => s.refreshHistory);
  const closeSession = useChat((s) => s.closeSession);

  const [draft, setDraft] = useState('');
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const scrollRef = useRef<ScrollView | null>(null);
  const insets = useSafeAreaInsets();
  const modesFetchedRef = useRef<string | null>(null);

  // Pull history once for sessions learned via list/broadcast.
  useEffect(() => {
    if (!id || !session || session.historyLoaded || session.historyLoading) return;
    if (modesFetchedRef.current === id) return;
    modesFetchedRef.current = id;
    void refreshHistory(id).catch(() => {
      if (modesFetchedRef.current === id) modesFetchedRef.current = null;
    });
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
    const t2 = setTimeout(() => scrollRef.current?.scrollToEnd({ animated: true }), 50);
    return () => clearTimeout(t2);
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
    if (!draft.trim() && attachments.length === 0) return;
    setError(null);
    setBusy(true);
    const text = draft;
    const atts = attachments;
    setDraft('');
    setAttachments([]);
    try {
      await sendPrompt(id!, text, atts);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function onCopyId() {
    setMenuOpen(false);
    await Clipboard.setStringAsync(id!);
  }

  function onArchive() {
    setMenuOpen(false);
    Alert.alert('归档会话', '关闭后该会话会从列表移除。确定要继续吗？', [
      { text: '取消', style: 'cancel' },
      {
        text: '归档',
        style: 'destructive',
        onPress: () => {
          void closeSession(id!).catch(() => {});
          router.back();
        },
      },
    ]);
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
        <View className="flex-row items-center px-md py-sm">
          <Pressable
            onPress={() => router.back()}
            className="h-9 w-9 items-center justify-center rounded-full bg-surface-muted active:opacity-70 dark:bg-surface-muted-dark"
          >
            <SymbolView name="chevron.left" size={18} tintColor={t.text} />
          </Pressable>
          <View className="flex-1 px-sm">
            <Text
              className="text-center text-md font-semibold text-text dark:text-text-dark"
              numberOfLines={1}
            >
              {s.title || s.cwd || '会话'}
            </Text>
            {STATUS_HINT[status] ? (
              <Text className="text-center text-xs text-text-muted dark:text-text-muted-dark">
                {STATUS_HINT[status]}
              </Text>
            ) : null}
          </View>
          <Pressable
            onPress={() => setMenuOpen(true)}
            className="h-9 w-9 items-center justify-center rounded-full bg-surface-muted active:opacity-70 dark:bg-surface-muted-dark"
          >
            <SymbolView name="ellipsis" size={18} tintColor={t.text} />
          </Pressable>
        </View>

        {/* Mode picker (Plan / Ask / …) — shown at the top when the agent
            advertises modes for this session. */}
        {session.modes.length > 0 && !isGuest ? (
          <View className="px-md pb-1">
            <SegmentedControl
              value={session.currentModeId ?? session.modes[0]!.id}
              onChange={(v) => void setMode(id!, v)}
              options={session.modes.map((m) => ({ value: m.id, label: shortMode(m.name) }))}
            />
          </View>
        ) : null}

        <ScrollView
          ref={scrollRef}
          contentContainerClassName="gap-md p-md"
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
            onSend={() => void onSend()}
            busy={busy}
            thinking={status === 'thinking'}
            onCancel={() => void cancel(id!)}
            placeholder="Follow up…"
            attachments={attachments}
            onAttachmentsChange={setAttachments}
            modelOption={modelOption}
            onModelChange={(v) =>
              modelOption ? void setConfigOption(id!, modelOption.id, v) : undefined
            }
            usage={session.usage}
            commands={session.availableCommands}
          />
        )}
      </KeyboardAvoidingView>

      {/* Session ⋯ menu */}
      <Modal
        visible={menuOpen}
        transparent
        animationType="fade"
        onRequestClose={() => setMenuOpen(false)}
      >
        <Pressable className="flex-1" onPress={() => setMenuOpen(false)}>
          <View
            style={{ position: 'absolute', top: insets.top + 48, right: 12 }}
            className="w-56 overflow-hidden rounded-xl border border-border bg-surface shadow-lg dark:border-border-dark dark:bg-surface-elevated-dark"
          >
            <MenuItem
              icon="doc.on.doc"
              label="复制 ID"
              tint={t.text}
              onPress={() => void onCopyId()}
            />
            <MenuItem
              icon="square.and.arrow.up"
              label="分享"
              tint={t.text}
              onPress={() => {
                setMenuOpen(false);
                setShareOpen(true);
              }}
            />
            <View className="h-px bg-border dark:bg-border-dark" />
            <MenuItem icon="archivebox" label="归档" tint="#dc2626" onPress={onArchive} last />
          </View>
        </Pressable>
      </Modal>

      <ShareAskModal
        visible={shareOpen}
        onClose={() => setShareOpen(false)}
        defaultCwd={s.cwd}
        defaultAgentId={s.agent_id}
      />
    </SafeAreaView>
  );
}

function MenuItem({
  icon,
  label,
  tint,
  onPress,
  last,
}: {
  icon: Parameters<typeof SymbolView>[0]['name'];
  label: string;
  tint: string;
  onPress: () => void;
  last?: boolean;
}) {
  return (
    <Pressable
      onPress={onPress}
      className={`flex-row items-center gap-md px-md py-md active:opacity-70 ${
        last ? '' : 'border-b border-border dark:border-border-dark'
      }`}
    >
      <SymbolView name={icon} size={17} tintColor={tint} />
      <Text className="text-md" style={{ color: tint }}>
        {label}
      </Text>
    </Pressable>
  );
}
