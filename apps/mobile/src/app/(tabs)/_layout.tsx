import { colors } from '@coding-anywhere/tokens';
import { Tabs } from 'expo-router';
import { SymbolView } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { useEffect } from 'react';

import { useChat } from '../../store/chat';
import { useConnection } from '../../store/connection';

// Wire the chat store to the live relay client: (re)attach the WS 'chat' handler
// whenever the client changes, and refresh sessions + agents once connected.
// Mounted here so it runs for the whole authed app shell regardless of tab.
function useChatAttach() {
  const client = useConnection((s) => s.client);
  const status = useConnection((s) => s.status);
  const attach = useChat((s) => s.attach);
  const refresh = useChat((s) => s.refreshSessions);
  const loadAgents = useChat((s) => s.loadAgents);

  useEffect(() => {
    if (!client) return;
    attach(client);
  }, [client, attach]);

  useEffect(() => {
    if (status !== 'connected') return;
    void refresh().catch(() => {});
    void loadAgents().catch(() => {});
  }, [status, refresh, loadAgents]);
}

export default function TabsLayout() {
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  useChatAttach();

  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: t.accent,
        tabBarInactiveTintColor: t.textMuted,
        tabBarStyle: {
          backgroundColor: t.surface,
          borderTopColor: t.border,
        },
      }}
    >
      <Tabs.Screen
        name="index"
        options={{
          title: '对话',
          tabBarIcon: ({ color, size }) => (
            <SymbolView name="message.fill" tintColor={color} size={size} />
          ),
        }}
      />
      <Tabs.Screen
        name="files"
        options={{
          title: '文件',
          tabBarIcon: ({ color, size }) => (
            <SymbolView name="folder.fill" tintColor={color} size={size} />
          ),
        }}
      />
      <Tabs.Screen
        name="diff"
        options={{
          title: 'Diff',
          tabBarIcon: ({ color, size }) => (
            <SymbolView name="arrow.triangle.branch" tintColor={color} size={size} />
          ),
        }}
      />
      <Tabs.Screen
        name="ports"
        options={{
          title: '端口',
          tabBarIcon: ({ color, size }) => (
            <SymbolView name="server.rack" tintColor={color} size={size} />
          ),
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          title: '账户',
          tabBarIcon: ({ color, size }) => (
            <SymbolView name="gearshape.fill" tintColor={color} size={size} />
          ),
        }}
      />
    </Tabs>
  );
}
