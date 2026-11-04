import Link from "next/link";
import LayoutCounter from "./components/LayoutCounter";

export const metadata = { title: "Next gallery" };

export default function RootLayout({ children, modal }: { children: React.ReactNode; modal: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <nav>
          <Link id="nav-home" href="/">Feed</Link> <Link id="nav-posts" href="/posts">Posts</Link> <Link id="nav-missing" href="/posts/missing" prefetch={false}>Missing</Link> <Link id="nav-crash" href="/crash">Crash</Link> <Link id="nav-boom" href="/boom" prefetch={false}>Boom</Link>
          <LayoutCounter />
        </nav>
        {children}
        {modal}
      </body>
    </html>
  );
}
