import { useRouter } from 'expo-router';
import { SymbolView } from 'expo-symbols';
import { Pressable, ScrollView, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { StatusBadge } from '../../components/chat';
import { useChat, type SessionState } from '../../store/chat';
import { useConnection } from '../../store/connection';

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

function SessionRow({ state, onPress }: { state: SessionState; onPress: () => void }) {
  const s = state.session;
  const title = s.title || s.cwd || '未命名会话';
  return (
    <Pressable
      onPress={onPress}
      className="flex-row items-center gap-md rounded-lg border border-border bg-surface p-md active:opacity-80 dark:border-border-dark dark:bg-surface-elevated-dark"
    >
      <View className="flex-1">
        <Text className="text-md font-semibold text-text dark:text-text-dark" numberOfLines={1}>
          {title}
        </Text>
        <Text className="text-xs text-text-muted dark:text-text-muted-dark" numberOfLines={1}>
          {agentShortName(s.agent_id)}
          {s.cwd ? ` · ${s.cwd}` : ''}
        </Text>
      </View>
      <StatusBadge status={s.status} />
    </Pressable>
  );
}

export default function Sessions() {
  const router = useRouter();
  const sessions = useChat((s) => s.sessions);
  const setActive = useChat((s) => s.setActive);
  const status = useConnection((s) => s.status);
  const list = Object.values(sessions);

  function openSession(id: string) {
    setActive(id);
    router.push({ pathname: '/chat/[id]', params: { id } });
  }

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
      <View className="flex-row items-center justify-between px-xl pb-sm pt-lg">
        <Text className="text-xxl font-bold text-text dark:text-text-dark">对话</Text>
        <Pressable
          disabled={status !== 'connected'}
          onPress={() => router.push('/new-session')}
          className={`h-10 flex-row items-center gap-xs rounded-md bg-ink-900 px-md dark:bg-ink-100 ${
            status !== 'connected' ? 'opacity-50' : 'active:opacity-80'
          }`}
        >
          <SymbolView name="plus" tintColor="#ffffff" size={16} />
          <Text className="text-sm font-semibold text-white dark:text-ink-900">新会话</Text>
        </Pressable>
      </View>

      {status !== 'connected' ? (
        <Text className="px-xl text-sm text-text-muted dark:text-text-muted-dark">
          等待 relay 连接…
        </Text>
      ) : null}

      <ScrollView contentContainerClassName="gap-sm p-xl pt-sm">
        {list.length === 0 ? (
          <View className="mt-xxl items-center gap-sm">
            <SymbolView name="message" size={40} tintColor="#9aa1ac" />
            <Text className="text-sm text-text-muted dark:text-text-muted-dark">
              还没有会话。点「新会话」开始与 agent 对话。
            </Text>
          </View>
        ) : (
          list.map((state) => (
            <SessionRow
              key={state.session.id}
              state={state}
              onPress={() => openSession(state.session.id)}
            />
          ))
        )}
      </ScrollView>
    </SafeAreaView>
  );
}
