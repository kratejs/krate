export default function HydrationWithoutReact() {
  return (
    <article class="section prose">
      <Head>
        <title>Hydration without React — Krate</title>
        <meta name="description" content="How Krate binds signals to the DOM with a tiny client runtime." />
      </Head>
      <p>
        <a href="/blog/">← All posts</a>
      </p>
      <h1 class="section-title" style="text-align:left">Hydration without React</h1>
      <p class="section-lede" style="text-align:left">2026-02-10</p>

      <p>
        Krate does not ship a virtual DOM. Instead, the compiler emits hydration markers
        that tell the runtime exactly which DOM nodes a signal drives.
      </p>

      <h2>Bindings, not diffs</h2>
      <p>
        A signal that feeds a text node becomes a direct write. A signal that feeds an
        attribute becomes an attribute effect. There is no tree to reconcile, so
        hydration is predictable and cheap.
      </p>

      <h2>Delegated events</h2>
      <p>
        Most events are handled through a single delegated listener; events that do not
        bubble (like <code>focus</code> and <code>change</code>) get direct listeners.
        Form inputs bind through a dedicated property path.
      </p>
    </article>
  );
}
