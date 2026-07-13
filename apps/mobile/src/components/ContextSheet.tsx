// Bottom "Context" sheet opened from the composer's "+" button. Lets the user
// attach photos / files (sent to the agent as ChatAttachment). Mirrors the
// reference design (IMG_4896): selected items strip + Add list. (Mode lives in
// the chat header; the model picker is the dropdown next to the "+".)
import { bytesToBase64, type ChatAttachment } from '@coding-anywhere/protocol';
import { colors } from '@coding-anywhere/tokens';
import * as DocumentPicker from 'expo-document-picker';
import { File } from 'expo-file-system';
import * as ImagePicker from 'expo-image-picker';
import { SymbolView } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { Alert, Image, Modal, Pressable, ScrollView, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

const MAX_BYTES = 10 * 1024 * 1024;

function attachmentThumbUri(att: ChatAttachment): string | null {
  return att.type === 'image' && att.data
    ? `data:${att.mime_type ?? 'image/jpeg'};base64,${att.data}`
    : null;
}

export function ContextSheet({
  visible,
  onClose,
  attachments,
  onAttachmentsChange,
}: {
  visible: boolean;
  onClose: () => void;
  attachments: ChatAttachment[];
  onAttachmentsChange: (next: ChatAttachment[]) => void;
}) {
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const insets = useSafeAreaInsets();

  async function pickPhotos() {
    const perm = await ImagePicker.requestMediaLibraryPermissionsAsync();
    if (!perm.granted) {
      Alert.alert('需要相册权限', '请在系统设置里允许访问相册后重试。');
      return;
    }
    const res = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ['images'],
      allowsMultipleSelection: true,
      base64: true,
      quality: 0.8,
    });
    if (res.canceled) return;
    const additions: ChatAttachment[] = [];
    for (const asset of res.assets) {
      if (!asset.base64) continue;
      if ((asset.fileSize ?? 0) > MAX_BYTES) {
        Alert.alert('文件过大', `${asset.fileName ?? '图片'} 超过 10MB 限制。`);
        continue;
      }
      additions.push({
        type: 'image',
        name: asset.fileName ?? `image-${Date.now()}.jpg`,
        data: asset.base64,
        mime_type: asset.mimeType ?? 'image/jpeg',
      });
    }
    if (additions.length) onAttachmentsChange([...attachments, ...additions]);
  }

  async function pickFiles() {
    const res = await DocumentPicker.getDocumentAsync({
      multiple: true,
      copyToCacheDirectory: true,
    });
    if (res.canceled) return;
    const additions: ChatAttachment[] = [];
    for (const asset of res.assets) {
      if ((asset.size ?? 0) > MAX_BYTES) {
        Alert.alert('文件过大', `${asset.name} 超过 10MB 限制。`);
        continue;
      }
      try {
        const bytes = await new File(asset.uri).bytes();
        const isImage = (asset.mimeType ?? '').startsWith('image/');
        additions.push({
          type: isImage ? 'image' : 'file',
          name: asset.name,
          data: bytesToBase64(bytes),
          mime_type: asset.mimeType ?? 'application/octet-stream',
        });
      } catch {
        Alert.alert('读取失败', `无法读取 ${asset.name}。`);
      }
    }
    if (additions.length) onAttachmentsChange([...attachments, ...additions]);
  }

  function removeAt(index: number) {
    onAttachmentsChange(attachments.filter((_, i) => i !== index));
  }

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <Pressable className="flex-1 justify-end bg-black/40" onPress={onClose}>
        <Pressable
          onPress={(e) => e.stopPropagation()}
          className="rounded-t-xl bg-surface dark:bg-surface-elevated-dark"
          style={{ paddingBottom: Math.max(insets.bottom, 12) }}
        >
          <View className="items-center pt-sm">
            <View className="h-1 w-10 rounded-full bg-ink-300 dark:bg-ink-600" />
          </View>

          {/* Header */}
          <View className="flex-row items-center px-lg py-md">
            <Pressable
              onPress={onClose}
              className="h-8 w-8 items-center justify-center rounded-full bg-surface-muted active:opacity-70 dark:bg-surface-muted-dark"
            >
              <SymbolView name="xmark" size={14} tintColor={t.text} />
            </Pressable>
            <Text className="flex-1 text-center text-md font-bold text-text dark:text-text-dark">
              上下文
            </Text>
            <View className="w-8" />
          </View>

          <ScrollView contentContainerClassName="pb-sm" keyboardShouldPersistTaps="handled">
            {/* Selected attachments strip */}
            {attachments.length > 0 ? (
              <ScrollView
                horizontal
                showsHorizontalScrollIndicator={false}
                contentContainerClassName="gap-sm px-lg pb-md"
              >
                {attachments.map((att, i) => {
                  const uri = attachmentThumbUri(att);
                  return (
                    <View key={i} className="relative">
                      <View className="h-20 w-20 items-center justify-center overflow-hidden rounded-lg border border-border bg-surface-muted dark:border-border-dark dark:bg-surface-muted-dark">
                        {uri ? (
                          <Image
                            source={{ uri }}
                            style={{ width: 80, height: 80 }}
                            resizeMode="cover"
                          />
                        ) : (
                          <View className="items-center gap-1 p-1">
                            <SymbolView name="doc.fill" size={22} tintColor={t.textMuted} />
                            <Text
                              className="text-[10px] text-text-muted dark:text-text-muted-dark"
                              numberOfLines={1}
                            >
                              {att.name}
                            </Text>
                          </View>
                        )}
                      </View>
                      <Pressable
                        onPress={() => removeAt(i)}
                        hitSlop={8}
                        className="absolute -right-1.5 -top-1.5 h-5 w-5 items-center justify-center rounded-full bg-ink-900 dark:bg-ink-100"
                      >
                        <SymbolView
                          name="xmark"
                          size={10}
                          tintColor={scheme === 'dark' ? '#0e0e10' : '#ffffff'}
                        />
                      </Pressable>
                    </View>
                  );
                })}
              </ScrollView>
            ) : null}

            {/* Add */}
            <Text className="px-lg pb-1 text-xs text-text-muted dark:text-text-muted-dark">
              添加
            </Text>
            <SheetRow icon="photo" label="照片" tint={t.text} onPress={() => void pickPhotos()} />
            <SheetRow icon="doc" label="文件" tint={t.text} onPress={() => void pickFiles()} />
          </ScrollView>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

function SheetRow({
  icon,
  label,
  tint,
  onPress,
  trailing,
}: {
  icon: Parameters<typeof SymbolView>[0]['name'];
  label: string;
  tint: string;
  onPress: () => void;
  trailing?: React.ReactNode;
}) {
  return (
    <Pressable
      onPress={onPress}
      className="mx-lg flex-row items-center gap-md border-b border-border py-md active:opacity-70 dark:border-border-dark"
    >
      <SymbolView name={icon} size={20} tintColor={tint} />
      <Text className="flex-1 text-md text-text dark:text-text-dark">{label}</Text>
      {trailing}
    </Pressable>
  );
}
