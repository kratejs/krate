import "@krate/components/code";
import sources from "../demos/sources.json";
import { demos } from "../demos/registry";
import "../demos/kit.css";
import "../styles/demos.css";

// Static building blocks for a /demos/<slug>/ page. They are kept separate (and
// the live demo is never passed to them as children) so that only the demo
// itself hydrates; none of the source text or registry data reaches the browser.

export function DemoHead(props: { slug: string }) {
  const demo = demos.find((d) => d.slug === props.slug)!;
  const zero = demo.js === "zero";
  return (
    <>
      <Head>
        <title>{demo.title + " - live demo"}</title>
        <meta name="description" content={demo.blurb} />
      </Head>
      <nav class="demo-crumbs" aria-label="Breadcrumb">
        <a href="/demos/"><Icon name="lucide:arrow-left" width="14" height="14" /> All demos</a>
      </nav>
      <header class="demo-head" data-view-transition={"demo-" + demo.slug}>
        <h1>{demo.title}</h1>
        <p class="demo-blurb">{demo.blurb}</p>
        <div class="demo-badges">
          <span class={zero ? "demo-chip demo-chip-zero" : "demo-chip"}>
            {zero ? "0 bytes of page JavaScript" : "Hydrated island"}
          </span>
          {demo.tags.map((t) => <span class="demo-chip demo-chip-tag">{t}</span>)}
        </div>
      </header>
    </>
  );
}

export function DemoNotes(props: { slug: string }) {
  const demo = demos.find((d) => d.slug === props.slug)!;
  return (
    <aside class="demo-notes">
      <h2>What this shows</h2>
      <p>{demo.shows}</p>
      <a class="btn btn-primary" href={"/playground/#example=demo-" + demo.slug}>
        <Icon name="lucide:play" width="16" height="16" /> Open in playground
      </a>
    </aside>
  );
}

export function DemoSource(props: { slug: string }) {
  return (
    <section class="demo-source" aria-label="Source">
      <h2>Source</h2>
      {(sources as Record<string, { name: string; lang: string; code: string }[]>)[props.slug].map((s) => (
        <details class="demo-file" open={s.lang !== "css"}>
          <summary>{s.name}</summary>
          <div class="krate-code-block">
            <SyntaxHighlight lang={s.lang}>{s.code}</SyntaxHighlight>
          </div>
        </details>
      ))}
    </section>
  );
}

export function DemoPager(props: { slug: string }) {
  const index = demos.findIndex((d) => d.slug === props.slug);
  const prev = index > 0 ? demos[index - 1] : null;
  const next = index < demos.length - 1 ? demos[index + 1] : null;
  return (
    <nav class="demo-pager" aria-label="More demos">
      {prev ? <a href={"/demos/" + prev.slug + "/"} rel="prev"><span>Previous</span>{prev.title}</a> : <span />}
      {next ? <a href={"/demos/" + next.slug + "/"} rel="next" class="demo-pager-next"><span>Next</span>{next.title}</a> : <span />}
    </nav>
  );
}
