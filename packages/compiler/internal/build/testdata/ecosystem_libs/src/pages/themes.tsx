import { ThemeProvider, useTheme } from 'next-themes';
function Toggle() {
  const { theme, setTheme, resolvedTheme } = useTheme();
  return <div><button id="dark" onClick={() => setTheme('dark')}>dark</button><button id="light" onClick={() => setTheme('light')}>light</button><p id="theme">{String(theme)}/{String(resolvedTheme)}</p></div>;
}
export default function Page() {
  return <ThemeProvider attribute="class" defaultTheme="light" enableSystem={false}><main><Toggle /></main></ThemeProvider>;
}
