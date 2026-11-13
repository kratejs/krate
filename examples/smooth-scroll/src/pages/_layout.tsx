import './global.css';

export default function Layout(children) {
  return (
    <div>
      <nav class="nav">
        <span class="nav-title">Lenis smooth scroll</span>
        <Link href="/">Home</Link>
        <Link href="/about">About</Link>
        <Link href="/#snap">Snap</Link>
      </nav>
      <main>{children}</main>
      <div class="hud" id="hud">
        <div>lenis: <strong id="hud-state">loading</strong></div>
        <div>scroll: <strong id="hud-scroll">0</strong></div>
        <div>velocity: <strong id="hud-velocity">0</strong></div>
      </div>
      <script defer src="/hud.js"></script>
    </div>
  );
}
