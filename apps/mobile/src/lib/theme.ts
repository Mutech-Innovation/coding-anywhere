// Theme preference store. NativeWind's `colorScheme` is the source of truth for the
// active light/dark state (Tailwind darkMode is 'class'); this persists the user's
// CHOICE (system/light/dark) and re-applies it on launch. Default is 'system', so
// the app follows the device appearance until the user overrides it.
import { colorScheme } from 'nativewind';
import * as SecureStore from 'expo-secure-store';
import { create } from 'zustand';

export type ThemePref = 'system' | 'light' | 'dark';

const KEY = 'coding_anywhere_theme';

interface ThemeStore {
  pref: ThemePref;
  setPref(p: ThemePref): void;
  load(): Promise<void>;
}

export const useTheme = create<ThemeStore>((set) => ({
  pref: 'system',
  setPref(p) {
    set({ pref: p });
    colorScheme.set(p);
    void SecureStore.setItemAsync(KEY, p);
  },
  async load() {
    try {
      const raw = (await SecureStore.getItemAsync(KEY)) as ThemePref | null;
      const p: ThemePref = raw === 'light' || raw === 'dark' || raw === 'system' ? raw : 'system';
      set({ pref: p });
      colorScheme.set(p);
    } catch {
      colorScheme.set('system');
    }
  },
}));
