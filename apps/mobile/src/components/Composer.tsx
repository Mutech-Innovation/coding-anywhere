// Shared chat composer used by the chat screen, the workspaces list, and the
// per-workspace screen. Three sizes, all driven by focus + a drag handle:
//   collapsed  – a rounded pill (placeholder only) when idle/blurred & empty
//   expanded   – a card with a multiline editor (on focus)
//   full       – a tall, near-fullscreen editor (drag the handle up)
// The "+" opens the Context sheet (attachments); a model dropdown sits next to
// it. No voice input (intentional). Purely presentational: the parent owns
// draft/attachments and decides what onSend does. Mode (Plan/Ask/…) lives in the
// chat header, not here.
import {
  type AvailableCommand,
  type ChatConfigOption,
  type ChatUsage,
  type ChatAttachment,
} from '@coding-anywhere/protocol';
import { colors } from '@coding-anywhere/tokens';
import { SymbolView } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { useEffect, useRef, useState } from 'react';
import {
  Image,
  Keyboard,
  LayoutAnimation,
  Modal,
  PanResponder,
  Platform,
  Pressable,
  ScrollView,
  Text,
  TextInput,
  UIManager,
  useWindowDimensions,
  View,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { ContextSheet } from './ContextSheet';

if (Platform.OS === 'android' && UIManager.setLayoutAnimationEnabledExperimental) {
  UIManager.setLayoutAnimationEnabledExperimental(true);
}

// Tracks whether the on-screen keyboard is actually visible. We can't infer this
// from input focus alone: with a hardware keyboard (e.g. the simulator) the
// input is focused but no on-screen keyboard appears, so the composer must keep
// its full safe-area bottom padding to clear the home indicator.
function useKeyboardShown(): boolean {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const showEvt = Platform.OS === 'ios' ? 'keyboardWillShow' : 'keyboardDidShow';
    const hideEvt = Platform.OS === 'ios' ? 'keyboardWillHide' : 'keyboardDidHide';
    const show = Keyboard.addListener(showEvt, () => setShown(true));
    const hide = Keyboard.addListener(hideEvt, () => setShown(false));
    return () => {
      show.remove();
      hide.remove();
    };
  }, []);
  return shown;
}

type Mode = 'collapsed' | 'expanded' | 'full';

function choiceValue(c: { value?: unknown }): string {
  return typeof c.value === 'string' ? c.value : String(c.value ?? '');
}
function choiceLabel(c: { value?: unknown; label?: string; name?: string }): string {
  return c.label ?? c.name ?? choiceValue(c);
}

export interface ComposerProps {
  draft: string;
  onChangeDraft: (v: string) => void;
  onSend: () => void;
  busy?: boolean;
  thinking?: boolean;
  onCancel?: () => void;
  placeholder?: string;
  /** Shown in the expanded/full header (e.g. the workspace name). */
  headerLabel?: string;
  attachments?: ChatAttachment[];
  onAttachmentsChange?: (next: ChatAttachment[]) => void;
  modelOption?: ChatConfigOption | null;
  onModelChange?: (value: string) => void;
  usage?: ChatUsage | null;
  commands?: AvailableCommand[];
  autoFocus?: boolean;
}

export function Composer({
  draft,
  onChangeDraft,
  onSend,
  busy = false,
  thinking = false,
  onCancel,
  placeholder = 'Plan, ask, build…',
  headerLabel,
  attachments = [],
  onAttachmentsChange,
  modelOption = null,
  onModelChange,
  usage,
  commands = [],
  autoFocus = false,
}: ComposerProps) {
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const insets = useSafeAreaInsets();
  const { height: winH, width: winW } = useWindowDimensions();

  const [mode, setMode] = useState<Mode>(autoFocus ? 'expanded' : 'collapsed');
  const kbShown = useKeyboardShown();
  const [contextOpen, setContextOpen] = useState(false);
  const [modelOpen, setModelOpen] = useState(false);
  const [modelAnchor, setModelAnchor] = useState<{ x: number; y: number } | null>(null);
  const inputRef = useRef<TextInput | null>(null);
  const pillRef = useRef<View>(null);
  const modeRef = useRef<Mode>(mode);
  modeRef.current = mode;

  const canSend = !!draft.trim() || attachments.length > 0;

  function animate() {
    LayoutAnimation.configureNext(LayoutAnimation.create(180, 'easeInEaseOut', 'opacity'));
  }

  function go(next: Mode) {
    animate();
    setMode(next);
    if (next === 'collapsed') inputRef.current?.blur();
    else setTimeout(() => inputRef.current?.focus(), 0);
  }

  // Adding context from the collapsed pill grows the composer so the new
  // thumbnails are visible.
  function handleAttachments(next: ChatAttachment[]) {
    onAttachmentsChange?.(next);
    if (next.length > 0 && modeRef.current === 'collapsed') go('expanded');
  }

  useEffect(() => {
    if (autoFocus) setTimeout(() => inputRef.current?.focus(), 100);
  }, [autoFocus]);

  // Drag the grab handle: up → fullscreen, down → shrink one step.
  const pan = useRef(
    PanResponder.create({
      onMoveShouldSetPanResponder: (_e, g) => Math.abs(g.dy) > 8,
      onPanResponderRelease: (_e, g) => {
        if (g.dy < -24) go('full');
        else if (g.dy > 24) go(modeRef.current === 'full' ? 'expanded' : 'collapsed');
      },
    }),
  ).current;

  const slashMatch = /^\/(\w*)$/.exec(draft);
  const suggestions =
    slashMatch && commands.length > 0
      ? commands.filter((c) => c.name.startsWith(slashMatch[1]!)).slice(0, 5)
      : [];

  const modelChoices = modelOption?.options ?? [];
  const currentModel =
    typeof modelOption?.currentValue === 'string' ? modelOption.currentValue : undefined;
  const currentModelLabel = modelChoices.find((c) => choiceValue(c) === currentModel) ?? null;

  // With the keyboard up, KeyboardAvoidingView already lifts us above it, so a
  // small gap is enough. Otherwise keep the safe-area inset so the bottom row
  // clears the home indicator (hardware-keyboard / blurred cases).
  const bottomPad = kbShown ? 8 : Math.max(insets.bottom, 12);
  const editorHeight = mode === 'full' ? Math.round(winH * 0.5) : 110;

  function handleSend() {
    if (!canSend || busy) return;
    onSend();
  }

  // Open the model dropdown anchored just above the pill (measure its on-screen
  // position so the menu pops up from the pill rather than the screen bottom).
  function openModelMenu() {
    const node = pillRef.current;
    if (!node) {
      setModelOpen(true);
      return;
    }
    node.measureInWindow((x, y) => {
      setModelAnchor({ x, y });
      setModelOpen(true);
    });
  }

  // ---- Collapsed pill -----------------------------------------------------
  if (mode === 'collapsed') {
    return (
      <View
        className="bg-surface px-md pt-sm dark:bg-surface-dark"
        style={{ paddingBottom: bottomPad }}
      >
        <View className="flex-row items-center gap-sm rounded-full border border-border bg-surface-muted px-xs py-1 dark:border-border-dark dark:bg-surface-muted-dark">
          <PlusButton
            tint={t.textMuted}
            count={attachments.length}
            onPress={() => setContextOpen(true)}
          />
          <Pressable className="flex-1 py-1.5" onPress={() => go('expanded')}>
            <Text
              className={
                draft
                  ? 'text-md text-text dark:text-text-dark'
                  : 'text-md text-text-muted dark:text-text-muted-dark'
              }
              numberOfLines={1}
            >
              {draft || placeholder}
            </Text>
          </Pressable>
          {canSend ? (
            <SendButton
              scheme={scheme}
              thinking={thinking}
              onSend={handleSend}
              onCancel={onCancel}
              small
            />
          ) : null}
        </View>
        <ContextSheet
          visible={contextOpen}
          onClose={() => setContextOpen(false)}
          attachments={attachments}
          onAttachmentsChange={handleAttachments}
        />
      </View>
    );
  }

  // ---- Expanded / full card ----------------------------------------------
  return (
    <View
      className="bg-surface px-md pt-1 dark:bg-surface-dark"
      style={{ paddingBottom: bottomPad }}
    >
      <View className="rounded-xl border border-border bg-surface-muted dark:border-border-dark dark:bg-surface-muted-dark">
        {/* Grab handle */}
        <View {...pan.panHandlers} className="items-center py-1.5">
          <Pressable
            hitSlop={12}
            onPress={() => go(mode === 'full' ? 'expanded' : 'full')}
            className="px-8 py-1"
          >
            <View className="h-1 w-10 rounded-full bg-ink-300 dark:bg-ink-600" />
          </Pressable>
        </View>

        {headerLabel ? (
          <View className="flex-row items-center gap-xs px-md pb-1">
            <Text className="text-sm font-semibold text-text dark:text-text-dark" numberOfLines={1}>
              {headerLabel}
            </Text>
          </View>
        ) : null}

        {/* Slash suggestions */}
        {suggestions.length > 0 ? (
          <View className="mx-md mb-sm overflow-hidden rounded-md border border-border dark:border-border-dark">
            {suggestions.map((c) => (
              <Pressable
                key={c.name}
                onPress={() => onChangeDraft(`/${c.name} `)}
                className="border-b border-border bg-surface px-md py-sm active:opacity-70 dark:border-border-dark dark:bg-surface-elevated-dark"
              >
                <Text className="text-sm font-semibold text-text dark:text-text-dark">
                  /{c.name}
                </Text>
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

        {/* Editor */}
        <TextInput
          ref={inputRef}
          className="px-md text-md text-text dark:text-text-dark"
          style={{ height: editorHeight, textAlignVertical: 'top' }}
          placeholder={placeholder}
          placeholderTextColor={t.textMuted}
          value={draft}
          onChangeText={onChangeDraft}
          onBlur={() => {
            // Don't shrink while the context sheet is up — it just blurred us.
            if (!draft.trim() && attachments.length === 0 && !contextOpen) {
              go('collapsed');
            }
          }}
          multiline
        />

        {/* Attachment thumbnails */}
        {attachments.length > 0 ? (
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerClassName="gap-sm px-md pb-sm"
          >
            {attachments.map((att, i) => {
              const uri =
                att.type === 'image' && att.data
                  ? `data:${att.mime_type ?? 'image/jpeg'};base64,${att.data}`
                  : null;
              return (
                <View key={i} className="relative">
                  <View className="h-14 w-14 items-center justify-center overflow-hidden rounded-md border border-border bg-surface dark:border-border-dark dark:bg-surface-elevated-dark">
                    {uri ? (
                      <Image
                        source={{ uri }}
                        style={{ width: 56, height: 56 }}
                        resizeMode="cover"
                      />
                    ) : (
                      <SymbolView name="doc.fill" size={18} tintColor={t.textMuted} />
                    )}
                  </View>
                  <Pressable
                    onPress={() => onAttachmentsChange?.(attachments.filter((_, j) => j !== i))}
                    hitSlop={8}
                    className="absolute -right-1.5 -top-1.5 h-5 w-5 items-center justify-center rounded-full bg-ink-900 dark:bg-ink-100"
                  >
                    <SymbolView
                      name="xmark"
                      size={9}
                      tintColor={scheme === 'dark' ? '#0e0e10' : '#ffffff'}
                    />
                  </Pressable>
                </View>
              );
            })}
          </ScrollView>
        ) : null}

        {/* Bottom row */}
        <View className="flex-row items-center gap-sm px-sm pb-sm pt-1">
          <PlusButton
            tint={t.textMuted}
            count={attachments.length}
            onPress={() => setContextOpen(true)}
          />
          {modelOption ? (
            <Pressable
              ref={pillRef}
              onPress={openModelMenu}
              className="flex-row items-center gap-xs rounded-full border border-border px-sm py-1 active:opacity-70 dark:border-border-dark"
            >
              <Text className="text-xs text-text-muted dark:text-text-muted-dark" numberOfLines={1}>
                {currentModelLabel ? choiceLabel(currentModelLabel) : 'Auto'}
              </Text>
              <SymbolView name="chevron.up" size={9} tintColor={t.textMuted} />
            </Pressable>
          ) : null}
          {usage ? (
            <Text className="text-xs text-text-muted dark:text-text-muted-dark">
              {usage.used}/{usage.size}
            </Text>
          ) : null}
          <View className="flex-1" />
          <SendButton
            scheme={scheme}
            thinking={thinking}
            onSend={handleSend}
            onCancel={onCancel}
            disabled={!canSend || busy}
          />
        </View>
      </View>

      <ContextSheet
        visible={contextOpen}
        onClose={() => setContextOpen(false)}
        attachments={attachments}
        onAttachmentsChange={handleAttachments}
      />

      {/* Model dropdown — floats just above the pill */}
      <Modal
        visible={modelOpen}
        transparent
        animationType="fade"
        onRequestClose={() => setModelOpen(false)}
      >
        <Pressable className="flex-1" onPress={() => setModelOpen(false)}>
          {modelAnchor ? (
            <View
              style={{
                position: 'absolute',
                left: Math.max(12, Math.min(modelAnchor.x, winW - 224)),
                bottom: Math.max(12, winH - modelAnchor.y + 8),
                width: 212,
                maxHeight: 300,
              }}
              className="overflow-hidden rounded-xl border border-border bg-surface shadow-lg dark:border-border-dark dark:bg-surface-elevated-dark"
            >
              <ScrollView>
                {modelChoices.map((c) => {
                  const v = choiceValue(c);
                  const active = v === currentModel;
                  return (
                    <Pressable
                      key={v}
                      onPress={() => {
                        onModelChange?.(v);
                        setModelOpen(false);
                      }}
                      className="flex-row items-center justify-between gap-sm border-b border-border px-md py-sm active:opacity-70 dark:border-border-dark"
                    >
                      <Text
                        className={
                          active
                            ? 'flex-1 text-sm font-semibold text-accent dark:text-accent-dark'
                            : 'flex-1 text-sm text-text dark:text-text-dark'
                        }
                        numberOfLines={1}
                      >
                        {choiceLabel(c)}
                      </Text>
                      {active ? (
                        <SymbolView name="checkmark" size={13} tintColor={t.accent} />
                      ) : null}
                    </Pressable>
                  );
                })}
              </ScrollView>
            </View>
          ) : null}
        </Pressable>
      </Modal>
    </View>
  );
}

function PlusButton({
  tint,
  count,
  onPress,
}: {
  tint: string;
  count: number;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      className="relative h-9 w-9 items-center justify-center rounded-full bg-surface active:opacity-70 dark:bg-surface-elevated-dark"
    >
      <SymbolView name="plus" size={16} tintColor={tint} />
      {count > 0 ? (
        <View className="absolute -right-0.5 -top-0.5 h-4 w-4 items-center justify-center rounded-full bg-accent">
          <Text className="text-[10px] font-bold text-white">{count}</Text>
        </View>
      ) : null}
    </Pressable>
  );
}

function SendButton({
  scheme,
  thinking,
  onSend,
  onCancel,
  disabled,
  small,
}: {
  scheme: string | null | undefined;
  thinking: boolean;
  onSend: () => void;
  onCancel?: () => void;
  disabled?: boolean;
  small?: boolean;
}) {
  const size = small ? 'h-8 w-8' : 'h-9 w-9';
  if (thinking) {
    return (
      <Pressable
        onPress={onCancel}
        className={`${size} items-center justify-center rounded-full bg-danger active:opacity-80`}
      >
        <SymbolView name="stop.fill" size={14} tintColor="#ffffff" />
      </Pressable>
    );
  }
  return (
    <Pressable
      onPress={onSend}
      disabled={disabled}
      className={`${size} items-center justify-center rounded-full bg-ink-900 dark:bg-ink-100 ${
        disabled ? 'opacity-40' : 'active:opacity-80'
      }`}
    >
      <SymbolView name="arrow.up" size={16} tintColor={scheme === 'dark' ? '#0e0e10' : '#ffffff'} />
    </Pressable>
  );
}
