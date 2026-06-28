import { Stack, useRouter } from 'expo-router';
import { useEffect, useMemo, useState } from 'react';
import { Pressable, ScrollView, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { Button, ErrorBanner, SegmentedControl, TextField } from '../components/ui';
import { useChat } from '../store/chat';

export default function NewSession() {
  const router = useRouter();
  const create = useChat((s) => s.createSession);
  const agents = useChat((s) => s.agents);
  const setActive = useChat((s) => s.setActive);

  const availableAgents = useMemo(() => agents.filter((a) => a.available), [agents]);
  const [agentId, setAgentId] = useState('devin');
  const [cwd, setCwd] = useState('');
  const [title, setTitle] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (availableAgents.length === 0) return;
    if (!availableAgents.some((a) => a.id === agentId)) setAgentId(availableAgents[0]!.id);
  }, [availableAgents, agentId]);

  async function onSubmit() {
    if (!cwd.trim()) return setError('请填写项目目录（agent 不能在 $HOME 这类根目录运行）。');
    setError(null);
    setBusy(true);
    try {
      const sess = await create({ cwd: cwd.trim(), agentId, title: title.trim() || undefined });
      setActive(sess.id);
      router.replace({ pathname: '/chat/[id]', params: { id: sess.id } });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
      <Stack.Screen options={{ headerShown: false }} />
      <View className="flex-row items-center justify-between border-b border-border px-md py-sm dark:border-border-dark">
        <Pressable onPress={() => router.back()} className="p-1 active:opacity-60">
          <Text className="text-sm text-accent dark:text-accent-dark">取消</Text>
        </Pressable>
        <Text className="text-md font-semibold text-text dark:text-text-dark">新会话</Text>
        <View className="w-10" />
      </View>

      <ScrollView contentContainerClassName="gap-md p-xl">
        {availableAgents.length > 1 ? (
          <View className="gap-xs">
            <Text className="text-sm font-semibold text-text dark:text-text-dark">Agent</Text>
            <SegmentedControl
              value={agentId}
              onChange={setAgentId}
              options={availableAgents.map((a) => ({ value: a.id, label: a.name }))}
            />
          </View>
        ) : null}

        <TextField
          label="项目目录 (cwd)"
          description="必填。填写一个项目目录的绝对路径，例如 /Users/you/projects/app。"
          value={cwd}
          onChangeText={setCwd}
          placeholder="/Users/you/projects/app"
          autoCapitalize="none"
          autoCorrect={false}
        />

        <TextField
          label="标题（可选）"
          value={title}
          onChangeText={setTitle}
          placeholder="（可选）"
        />

        {agents.length > 0 && availableAgents.length === 0 ? (
          <ErrorBanner message="该 daemon 上没有可用的 agent。请在开发机上安装并登录 devin 或 cursor-agent。" />
        ) : null}
        <ErrorBanner message={error} />

        <Button title="开始" loading={busy} disabled={!cwd.trim()} onPress={onSubmit} />
      </ScrollView>
    </SafeAreaView>
  );
}
