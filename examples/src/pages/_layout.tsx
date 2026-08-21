import './global.css';
import { Link } from '@krate/runtime';

export default function Layout(children) {
  return (
    <div class="layout">
      <nav>
        <Link href="/" prefetch={false}>Home</Link>
        <Link href="/about">About</Link>
        <Link href="/blog">Blog</Link>
      </nav>
      <main>{children}</main>
      <footer>Krate &copy; 2026</footer>
    </div>
  );
}
