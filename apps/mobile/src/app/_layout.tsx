import { Stack, useRouter, useSegments } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useColorScheme } from 'nativewind';
import { useEffect } from 'react';
import { ActivityIndicator, View } from 'react-native';

import '../global.css';
import { useTheme } from '../lib/theme';
import { useConnection } from '../store/connection';

export default function RootLayout() {
  const { colorScheme } = useColorScheme();
  const hydrate = useConnection((s) => s.hydrate);
  const hydrated = useConnection((s) => s.hydrated);
  const credentials = useConnection((s) => s.credentials);
  const loadTheme = useTheme((s) => s.load);
  const segments = useSegments();
  const router = useRouter();

  // Restore the saved session + theme preference once on launch.
  useEffect(() => {
    void hydrate();
    void loadTheme();
  }, [hydrate, loadTheme]);

  // Auth gate: bounce between the sign-in screen and the app shell based on
  // whether we hold credentials. Runs only after hydration resolves.
  useEffect(() => {
    if (!hydrated) return;
    const onSignIn = segments[0] === 'sign-in';
    if (!credentials && !onSignIn) router.replace('/sign-in');
    else if (credentials && onSignIn) router.replace('/');
  }, [hydrated, credentials, segments, router]);

  if (!hydrated) {
    return (
      <View className="flex-1 items-center justify-center bg-surface dark:bg-surface-dark">
        <ActivityIndicator />
        <StatusBar style={colorScheme === 'dark' ? 'light' : 'dark'} />
      </View>
    );
  }

  return (
    <>
      <Stack screenOptions={{ headerShown: false }} />
      <StatusBar style={colorScheme === 'dark' ? 'light' : 'dark'} />
    </>
  );
}
