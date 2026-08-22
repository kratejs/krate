import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'Benchmark',
  description: 'Next.js benchmark fixture',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
