'use client';
import { useTheme } from 'next-themes';

export default function ThemeToggle({ suffix = '' }: { suffix?: string }) {
  const { theme, setTheme, resolvedTheme } = useTheme();
  return (
    <div>
      <button id={'dark' + suffix} onClick={() => setTheme('dark')}>dark</button>
      <p id={'theme' + suffix}>{String(theme)}/{String(resolvedTheme)}</p>
    </div>
  );
}
