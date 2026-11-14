import { ThemeProvider } from 'next-themes';

export default function Layout({ children }: { children: any }) {
  return <ThemeProvider attribute="class" defaultTheme="light" enableSystem={false}>{children}</ThemeProvider>;
}
