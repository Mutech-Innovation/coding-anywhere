import { type ChatAttachment } from '@coding-anywhere/protocol';
import { colors } from '@coding-anywhere/tokens';
import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { SymbolView } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { useMemo, useState } from 'react';
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  Text,
  View,
} from 'react-native';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';

import { Composer } from '../components/Composer';
import { ShareAskModal } from '../components/ShareAskModal';
import {
  groupSessionList,
  groupSessionsByDay,
  workspaceLabel,
  type DaySection,
} from '../components/session-groups';
import { useChat, type SessionState } from '../store/chat';
import { useConnection } from '../store/connection';

const STATUS_DOT: Record<string, string> = {
  idle: 'bg-ink-300 dark:bg-ink-600',
  thinking: 'bg-warning',
  awaiting_permission: 'bg-danger',
  closed: 'bg-ink-300 dark:bg-ink-600',
};

const STATUS_HINT: Record<string, string> = {
  thinking: '思考中',
  awaiting_permission: '待授权',
  closed: '已关闭',
};

function agentShortName(agentId: string): string {
  switch (agentId) {
    case 'cursor-cli':
      return 'Cursor';
    case 'devin':
    case '':
      return 'Devin';
    default:
      return agentId;
  }
}

export default function WorkspaceScreen() {
  const router = useRouter();
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const insets = useSafeAreaInsets();
  const params = useLocalSearchParams<{ cwd?: string; all?: string; label?: string }>();
  const isAll = params.all === '1';
  const cwd = params.cwd ?? '';

  const sessions = useChat((s) => s.sessions);
  const create = useChat((s) => s.createSession);
  const sendPrompt = useChat((s) => s.sendPrompt);
  const setActive = useChat((s) => s.setActive);
  const agents = useChat((s) => s.agents);
  const status = useConnection((s) => s.status);

  const [draft, setDraft] = useState('');
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  const [busy, setBusy] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);

  const title = params.label || (isAll ? 'All Repos' : workspaceLabel(cwd));
  const defaultAgent = useMemo(() => {
    const avail = agents.filter((a) => a.available);
    return avail[0]?.id ?? 'devin';
  }, [agents]);

  const sections: DaySection[] = useMemo(() => {
    const { regular } = groupSessionList(sessions);
    const list = isAll ? regular : regular.filter((s) => (s.session.cwd ?? '') === cwd);
    return groupSessionsByDay(list);
  }, [sessions, isAll, cwd]);

  function openSession(sessionId: string) {
    setActive(sessionId);
    router.push({ pathname: '/chat/[id]', params: { id: sessionId } });
  }

  async function onSend() {
    if (!draft.trim() && attachments.length === 0) return;
    const text = draft;
    const atts = attachments;
    // Without a concrete cwd (All Repos) we route to the create screen so the
    // user picks a directory first; the prompt rides along and auto-sends.
    if (isAll || !cwd) {
      router.push({ pathname: '/new-session', params: { prompt: text } });
      return;
    }
    setDraft('');
    setAttachments([]);
    setBusy(true);
    try {
      const sess = await create({ cwd, agentId: defaultAgent });
      setActive(sess.id);
      void sendPrompt(sess.id, text, atts).catch(() => {});
      router.replace({ pathname: '/chat/[id]', params: { id: sess.id } });
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
        <View className="flex-row items-center px-md py-sm">
          <Pressable
            onPress={() => router.back()}
            className="h-9 w-9 items-center justify-center rounded-full bg-surface-muted active:opacity-70 dark:bg-surface-muted-dark"
          >
            <SymbolView name="chevron.left" size={18} tintColor={t.text} />
          </Pressable>
          <View className="flex-1" />
          <Pressable
            onPress={() => setMenuOpen(true)}
            className="h-9 w-9 items-center justify-center rounded-full bg-surface-muted active:opacity-70 dark:bg-surface-muted-dark"
          >
            <SymbolView name="ellipsis" size={18} tintColor={t.text} />
          </Pressable>
        </View>

        <ScrollView contentContainerClassName="px-xl pb-md">
          <Text
            className="pb-md pt-1 text-xxl font-bold text-text dark:text-text-dark"
            numberOfLines={1}
          >
            {title}
          </Text>

          {sections.length === 0 ? (
            <View className="mt-xxl items-center gap-sm">
              <SymbolView name="bubble.left.and.bubble.right" size={36} tintColor={t.textMuted} />
              <Text className="text-sm text-text-muted dark:text-text-muted-dark">
                还没有会话。在下面输入开始一个新任务。
              </Text>
            </View>
          ) : (
            sections.map((section) => (
              <View key={section.label} className="mb-md">
                <Text className="pb-xs text-sm text-text-muted dark:text-text-muted-dark">
                  {section.label}
                </Text>
                <View>
                  {section.sessions.map((state) => (
                    <SessionRow
                      key={state.session.id}
                      state={state}
                      showCwd={isAll}
                      onPress={() => openSession(state.session.id)}
                    />
                  ))}
                </View>
              </View>
            ))
          )}
        </ScrollView>

        <Composer
          draft={draft}
          onChangeDraft={setDraft}
          onSend={() => void onSend()}
          busy={busy || status !== 'connected'}
          headerLabel={isAll ? undefined : title}
          attachments={attachments}
          onAttachmentsChange={setAttachments}
        />
      </KeyboardAvoidingView>

      {/* Workspace ⋯ menu */}
      <Modal
        visible={menuOpen}
        transparent
        animationType="fade"
        onRequestClose={() => setMenuOpen(false)}
      >
        <Pressable className="flex-1" onPress={() => setMenuOpen(false)}>
          <View
            style={{ position: 'absolute', top: insets.top + 48, right: 12 }}
            className="w-64 overflow-hidden rounded-xl border border-border bg-surface shadow-lg dark:border-border-dark dark:bg-surface-elevated-dark"
          >
            <Pressable
              onPress={() => {
                setMenuOpen(false);
                setShareOpen(true);
              }}
              className="flex-row items-center gap-md px-md py-md active:opacity-70"
            >
              <SymbolView name="square.and.arrow.up" size={17} tintColor={t.text} />
              <Text className="text-md text-text dark:text-text-dark">分享只读（ask）链接</Text>
            </Pressable>
          </View>
        </Pressable>
      </Modal>

      <ShareAskModal
        visible={shareOpen}
        onClose={() => setShareOpen(false)}
        defaultCwd={isAll ? '' : cwd}
        defaultAgentId={defaultAgent}
      />
    </SafeAreaView>
  );
}

function SessionRow({
  state,
  showCwd,
  onPress,
}: {
  state: SessionState;
  showCwd: boolean;
  onPress: () => void;
}) {
  const s = state.session;
  const title = s.title || workspaceLabel(s.cwd) || '未命名会话';
  const subtitle = STATUS_HINT[s.status] ?? agentShortName(s.agent_id);
  return (
    <Pressable
      onPress={onPress}
      className="flex-row items-center gap-md border-b border-border py-md active:opacity-70 dark:border-border-dark"
    >
      <View className={`mt-1.5 h-2 w-2 rounded-full ${STATUS_DOT[s.status] ?? STATUS_DOT.idle}`} />
      <View className="flex-1">
        <Text className="text-md font-semibold text-text dark:text-text-dark" numberOfLines={1}>
          {title}
        </Text>
        <Text className="text-xs text-text-muted dark:text-text-muted-dark" numberOfLines={1}>
          {showCwd && s.cwd ? `${agentShortName(s.agent_id)} · ${s.cwd}` : subtitle}
        </Text>
      </View>
    </Pressable>
  );
}
