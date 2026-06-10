import { Button, Tooltip } from '@mantine/core';
import { useEffect, useRef, useState } from 'react';

/**
 * Copy `text` to the clipboard, working outside secure contexts too.
 *
 * `navigator.clipboard` only exists on HTTPS or localhost. The dev/LAN
 * deployment is plain HTTP on a LAN IP (e.g. http://192.168.x.x:5173), where
 * Mantine's CopyButton silently did nothing. Fall back to the legacy hidden
 * textarea + `document.execCommand('copy')` path there — it still works in
 * every browser as long as it runs inside a user gesture.
 */
export async function copyText(text: string): Promise<boolean> {
  if (window.isSecureContext && navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Permission denied etc. — fall through to the legacy path.
    }
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.top = '-1000px';
  document.body.appendChild(ta);
  ta.select();
  let ok = false;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  document.body.removeChild(ta);
  return ok;
}

/**
 * Drop-in replacement for the Mantine CopyButton+Tooltip+Button stack used in
 * the share/key modals, backed by {@link copyText}. Shows "已复制" on success
 * and an inline failure hint when both clipboard paths are unavailable.
 */
export function CopyTextButton({
  value,
  label,
  disabled,
}: {
  value: string;
  label: string;
  disabled?: boolean;
}) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const onClick = () => {
    void copyText(value).then((ok) => {
      setState(ok ? 'copied' : 'failed');
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setState('idle'), 2000);
    });
  };

  const tooltip =
    state === 'copied'
      ? '已复制'
      : state === 'failed'
        ? '复制失败 — 请手动选中文本复制'
        : '点击复制';
  return (
    <Tooltip label={tooltip} withArrow>
      <Button
        variant="default"
        color={state === 'failed' ? 'red' : undefined}
        onClick={onClick}
        disabled={disabled}
      >
        {state === 'copied' ? '已复制' : state === 'failed' ? '复制失败' : label}
      </Button>
    </Tooltip>
  );
}
