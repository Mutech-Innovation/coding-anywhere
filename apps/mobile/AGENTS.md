# Agent notes for apps/mobile (Coding Anywhere — iOS/Android client)

The React Native (Expo) client for Coding Anywhere. It is the third client surface
alongside `apps/web`, talking to the same relay over WSS and reusing the shared
TypeScript packages. See the repo-root `AGENTS.md` and `docs/architecture.md` for
the overall system; this file covers the mobile app specifically.

## Stack

- **Expo SDK 56** (React 19, React Native 0.85, New Architecture only, Hermes v1).
- **Expo Router** (file-based routing, typed routes) — `src/app/`.
- **NativeWind v4** (Tailwind for RN) for styling; **zustand** for state.
- **expo-symbols** (SF Symbols) for icons, **react-native-webview** (port preview),
  **expo-secure-store** (credentials), **expo-file-system** + **expo-sharing**
  (file download).

## Shared packages (reused, not forked)

`@coding-anywhere/protocol` (WS client + REST pair client + zod schemas — runs
unchanged on RN), `@coding-anywhere/markdown` (chat markdown core), `…/tokens`
(design tokens), `…/agents`, `…/utils`. These are consumed as **TypeScript source**
(their `main` is `./src/index.ts`); `metro.config.js` maps the NodeNext `.js`
import specifiers to `.ts` so Metro can resolve them.

## Layout

```
src/
├── app/                 # Expo Router routes
│   ├── _layout.tsx      #   root: auth gate (hydrate → redirect) + theme load
│   ├── sign-in.tsx      #   password / account-key / pair-code login
│   ├── (tabs)/          #   authed shell: bottom tabs
│   │   ├── _layout.tsx  #     Tabs + chat-store attach/refresh wiring
│   │   ├── index.tsx    #     对话 (sessions list)
│   │   ├── files.tsx    #     文件 (browse / view / download)
│   │   ├── diff.tsx     #     Diff (changes list + unified line diff)
│   │   ├── ports.tsx    #     端口 (list + WebView preview)
│   │   └── settings.tsx #     账户 (status, machine switcher, theme)
│   ├── chat/[id].tsx    #   chat detail (transcript + composer)
│   └── new-session.tsx  #   create session (agent + cwd)
├── components/          # Markdown.tsx (RN renderer), chat.tsx (bubbles/cards), ui.tsx, transcript.ts
├── store/               # zustand: connection, credentials, chat, files, diff, ports
└── lib/                 # theme.ts (color-scheme preference)
```

## Conventions & gotchas

- **Mobile-local stores (for now).** `src/store/*` are async ports of the web
  stores (`apps/web/src/store/*`), reusing only `@coding-anywhere/protocol`. They
  are intentionally duplicated until they get lifted into a shared `packages/state`
  (see repo plan). `chat.ts` / `transcript.ts` are near-verbatim copies (only the
  `./connection` import path changed); `connection.ts` / `credentials.ts` differ
  from web only in async SecureStore I/O.
- **Markdown renderer mirrors web 1:1.** `components/Markdown.tsx` walks the same
  shared mdast tree as `apps/web/src/components/Markdown.tsx`; keep the streaming
  segment invariant (O(live tail)) and the security posture (no raw HTML, no image
  fetch, only http(s)/mailto links). If you change one renderer, change both.
- **Theming.** Colors come from `@coding-anywhere/tokens` (inlined into
  `tailwind.config.js` for build-time classes). Dark mode is manual
  (`darkMode: 'class'`); read the active scheme with `useColorScheme` from
  **nativewind** (not react-native) so it respects the in-app toggle
  (`src/lib/theme.ts`). Default preference is `system`.
- **pnpm + NativeWind.** `react-native-css-interop` is a direct dependency because
  the babel `jsxImportSource: 'nativewind'` transform imports it from app files and
  pnpm doesn't hoist it. Don't remove it.
- **Icons.** Use `SymbolView` (SF Symbols). Invalid symbol names render blank, not
  crash — verify names exist.

## Commands

Run from the repo root (or `apps/mobile`):

- `pnpm --filter @coding-anywhere/mobile start` — Metro dev server.
- `pnpm --filter @coding-anywhere/mobile ios` — open in the iOS simulator (Expo Go).
- `pnpm --filter @coding-anywhere/mobile typecheck` — `tsc --noEmit`.
- New routes/changes regenerate typed-routes on the next Metro bundle; if
  `tsc` complains about an unknown route, start the dev server once to refresh
  `.expo/types`.

## Running against a local backend

Start the relay + a paired daemon (see repo-root `AGENTS.md`), then in the app's
sign-in screen use Relay URL `http://localhost:8443` — on the iOS **simulator**,
`localhost` reaches the host Mac. A physical device needs the Mac's LAN IP (or a
tunnel) and the relay reachable from it. The port-preview WebView loads the relay's
`/v1/proxy/<port>` URL with the access token in the query string.

## Status

- Done: auth/connection, chat (live Devin/cursor streaming), files browse/view/
  download, git diff, ports + WebView preview, dark mode.
- Deferred: RN markdown test pins (need `jest-expo` + `@testing-library/react-native`),
  in-app upload/edit, terminal (xterm via WebView), APNs push, `packages/state`
  extraction. Terminal and APNs also need relay/daemon work.
