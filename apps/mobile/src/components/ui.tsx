// Small NativeWind UI kit shared across screens. Keeps the per-screen markup terse
// and the look consistent with the web app's CodeWhale-style tokens.
import { type ReactNode } from 'react';
import {
  ActivityIndicator,
  Pressable,
  type PressableProps,
  Text,
  TextInput,
  type TextInputProps,
  View,
  type ViewProps,
} from 'react-native';

export function Card({ className, children, ...rest }: ViewProps & { className?: string }) {
  return (
    <View
      className={`rounded-lg border border-border bg-surface dark:border-border-dark dark:bg-surface-elevated-dark ${className ?? ''}`}
      {...rest}
    >
      {children}
    </View>
  );
}

export function ErrorBanner({ message }: { message?: string | null }) {
  if (!message) return null;
  return (
    <View className="rounded-md border border-danger/30 bg-danger/10 px-md py-sm">
      <Text className="text-sm text-danger">{message}</Text>
    </View>
  );
}

type ButtonProps = PressableProps & {
  title: string;
  variant?: 'primary' | 'default';
  loading?: boolean;
};

export function Button({
  title,
  variant = 'primary',
  loading = false,
  disabled,
  ...rest
}: ButtonProps) {
  const isPrimary = variant === 'primary';
  const isDisabled = disabled || loading;
  return (
    <Pressable
      disabled={isDisabled}
      className={[
        'h-12 flex-row items-center justify-center rounded-md px-lg',
        isPrimary
          ? 'bg-ink-900 dark:bg-ink-100'
          : 'border border-border bg-surface dark:border-border-dark dark:bg-surface-elevated-dark',
        isDisabled ? 'opacity-50' : 'active:opacity-80',
      ].join(' ')}
      {...rest}
    >
      {loading ? (
        <ActivityIndicator color={isPrimary ? '#ffffff' : '#6b7280'} />
      ) : (
        <Text
          className={`text-md font-semibold ${
            isPrimary ? 'text-white dark:text-ink-900' : 'text-text dark:text-text-dark'
          }`}
        >
          {title}
        </Text>
      )}
    </Pressable>
  );
}

type TextFieldProps = TextInputProps & {
  label: string;
  description?: ReactNode;
};

export function TextField({ label, description, className, ...rest }: TextFieldProps) {
  return (
    <View className="gap-xs">
      <Text className="text-sm font-semibold text-text dark:text-text-dark">{label}</Text>
      {typeof description === 'string' ? (
        <Text className="text-xs text-text-muted dark:text-text-muted-dark">{description}</Text>
      ) : (
        description
      )}
      <TextInput
        className={`h-12 rounded-md border border-border bg-surface px-md text-md text-text dark:border-border-dark dark:bg-surface-elevated-dark dark:text-text-dark ${className ?? ''}`}
        placeholderTextColor="#9aa1ac"
        {...rest}
      />
    </View>
  );
}

export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { value: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
}) {
  return (
    <View className="flex-row rounded-md border border-border bg-surface-muted p-1 dark:border-border-dark dark:bg-surface-muted-dark">
      {options.map((opt) => {
        const active = opt.value === value;
        return (
          <Pressable
            key={opt.value}
            onPress={() => onChange(opt.value)}
            className={`flex-1 items-center rounded-sm py-2 ${
              active ? 'bg-surface dark:bg-surface-elevated-dark' : ''
            }`}
          >
            <Text
              numberOfLines={1}
              className={`text-sm ${
                active
                  ? 'font-semibold text-text dark:text-text-dark'
                  : 'text-text-muted dark:text-text-muted-dark'
              }`}
            >
              {opt.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}
