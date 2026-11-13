'use client';
import { useTheme } from 'next-themes';

export default function ThemeToggle() {
  const { theme, setTheme, resolvedTheme } = useTheme();
  return (
    <div>
      <button id="dark" onClick={() => setTheme('dark')}>dark</button>
      <p id="theme">{String(theme)}/{String(resolvedTheme)}</p>
    </div>
  );
}
