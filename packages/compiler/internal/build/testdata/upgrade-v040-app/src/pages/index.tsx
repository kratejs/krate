import Counter from "@/components/Counter";

export default function Home() {
  return (
    <>
      <Head>
        <title>__PROJECT_DISPLAY_NAME__</title>
        <meta
          name="description"
          content="A Krate app — signal-based reactivity, compiled to static HTML."
        />
      </Head>

      <section class="hero">
        <div class="hero-badge">Signals · SSG · Go-native</div>
        <h1 class="hero-title">You just built a Krate app.</h1>
        <p class="hero-subtitle">
          This page was server-rendered to static HTML at build time. The
          counter below is hydrated with a tiny signal-based runtime — no React,
          no bundler subprocess.
        </p>
        <div class="hero-actions">
          <a class="btn btn-primary" href="/about">Learn more</a>
          <a
            class="btn btn-secondary"
            href="https://krate.js.org/docs/getting-started/"
          >
            Read the docs
          </a>
        </div>
      </section>

      <section class="demo">
        <div class="demo-card">
          <h2>Try the counter</h2>
          <Counter />
        </div>
      </section>
    </>
  );
}
