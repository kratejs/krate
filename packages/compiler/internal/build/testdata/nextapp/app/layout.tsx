import Link from "next/link";
import LayoutCounter from "./components/LayoutCounter";

export const metadata = { title: "Next gallery" };

export default function RootLayout({ children, modal }: { children: React.ReactNode; modal: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <nav>
          <Link id="nav-home" href="/">Feed</Link> <Link id="nav-posts" href="/posts">Posts</Link>
          <LayoutCounter />
        </nav>
        {children}
        {modal}
      </body>
    </html>
  );
}
