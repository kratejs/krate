const LAST_UPDATED = "October 2026";

const V1_BAR = [
  {
    title: "Compatibility is measured",
    body: "Every claim comes from a public matrix and a corpus of real React and Next.js apps. A feature is supported when it passes, not when it compiles.",
  },
  {
    title: "Migrations are reversible",
    body: "Every migration step produces a report, a diff, and a rollback path. Unsupported code is surfaced before files are written.",
  },
  {
    title: "Agents use the compiler graph",
    body: "MCP tools operate on routes, symbols, diagnostics, and staged edits rather than guessing from raw text.",
  },
  {
    title: "Runtime tradeoffs are explicit",
    body: "Preact is the only island runtime. React 19 support is implemented in the compiler and compat layer for source components, with known limits documented.",
  },
];

const REACT_PHASES = [
  {
    id: "R1",
    tone: "gate",
    status: "Complete",
    title: "Conformance foundation",
    body: "Build the corpus and the public matrix that gate every release.",
    exit: "Corpus includes a shadcn dashboard, the Radix suite, a Next.js app, class components, and forms; CI runs Chromium, Firefox, and WebKit plus the supported Preact versions.",
  },
  {
    id: "R2",
    tone: "gate",
    status: "Complete",
    title: "React 18 source parity",
    body: "Close the remaining semantic gaps for source components: dependency arrays, effect ordering, class lifecycle, error boundaries, events, forms, keys, refs, and Suspense.",
    exit: "No P0 or P1 divergence in the source-component corpus; failures have actionable diagnostics.",
  },
  {
    id: "R3",
    tone: "gate",
    status: "Complete",
    title: "React 19 source semantics",
    body: "Build the React 19 action hooks (useActionState, useOptimistic, useFormStatus) on top of the server-action transport, plus use, ref-as-prop, and async transitions.",
    exit: "The React 19 reference app passes the same browser conformance suite as React 18.",
  },
  {
    id: "R4",
    tone: "gate",
    status: "Complete",
    title: "Island and SSR parity",
    body: "Support local components inside islands, SSR island hydration, per-island chunks, island error boundaries, and a shared state bridge.",
    exit: "A page containing source components, compiled packages, and SSR renders consistently in production and development.",
  },
  {
    id: "R5",
    tone: "gate",
    status: "v1 gate",
    title: "Hardening and release",
    body: "The version matrix is CI-tested, performance budgets are enforced, diagnostic docs and safe codemods are gated, partial compatibility rows document their divergences, and pnpm check:release verifies the whole contract. The v1 gate closes at release.",
    exit: "The matrix is green, budgets are enforced, and migration paths have no silent failures.",
  },
];

const MCP_PHASES = [
  {
    id: "M1",
    tone: "gate",
    status: "Complete",
    title: "Protocol and transport",
    body: "stdio and Streamable HTTP with persistent sessions, bearer auth, structured results, pagination, progress, structured logging, resource subscriptions, cancellation, and SSE reconnect replay.",
    exit: "The in-repo protocol and HTTP conformance suite passes, including sessions, progress, subscriptions, cancellation, and reconnect.",
  },
  {
    id: "M2",
    tone: "gate",
    status: "Complete",
    title: "Security and operations",
    body: "writeRoots, project policy, localhost binding, bearer auth, redacted JSONL audit logs, secret redaction, HTTP rate limits, host/origin isolation, and /healthz plus /metrics observability are implemented.",
    exit: "Remote MCP can be offered without exposing project secrets or cross-tenant state.",
  },
  {
    id: "M3",
    tone: "gate",
    status: "Complete",
    title: "Safe editing",
    body: "Source editing, atomic multi-file patches, batch operations, undo, expected-hash conflicts, dry-run previews, writeRoots, diagnostic-linked codemods, and filtered apply_safe_fixes are implemented.",
    exit: "Every write is atomic or rolled back; concurrent edits are detected and explained.",
  },
  {
    id: "M4",
    tone: "gate",
    status: "Complete",
    title: "Workspace graph",
    body: "search_project and read_files span multi-root workspaces; structured diagnostics and site-graph impact are joined by an incremental semantic symbol index with declarations, imports, exports, JSX references, and workspace_symbols.",
    exit: "An agent can answer what breaks before editing without reading every file.",
  },
  {
    id: "M5",
    tone: "gate",
    status: "Complete",
    title: "Agent workflows",
    body: "Diagnostic repair, refactor, dependency audit, component scaffold, add-page, publish-content, fix-checks, and explore prompts; run_task for build/check/test/typecheck; and a prompt evaluation suite covering registration, required arguments, defaults, tool order, and safety constraints.",
    exit: "A complete fix, refactor, and check workflow is reviewable and repeatable.",
  },
];

const MIGRATION_PHASES = [
  {
    id: "N1",
    tone: "gate",
    status: "Complete",
    title: "Inventory and report",
    body: "Detect Next.js App Router and Pages Router routes, layouts, loading/error boundaries, API routes, middleware, config, NEXT_PUBLIC_* environment values, fonts, and images.",
    exit: "Dry run writes migration-report.json with routes, dependencies, manual items, warnings, and per-route status.",
  },
  {
    id: "N2",
    tone: "gate",
    status: "Complete",
    title: "Routing, shell, and config",
    body: "Move routes and layouts, rewrite links, images, head/metadata, navigation, headers, dynamic imports, public env, redirects, rewrites, headers, tsconfig, aliases, and package scripts. NEXT_PUBLIC_* values are inlined at migration time.",
    exit: "Routing, shell, and config migration is covered by unit tests; end-to-end reference-app validation remains N5.",
  },
  {
    id: "N3",
    tone: "gate",
    status: "Complete",
    title: "Components and app semantics",
    body: "next/cache (revalidatePath, revalidateTag, tagged fetch, unstable_cache), async server components, \"use client\" boundaries, template.tsx, route groups, parallel and intercepting routes, catch-all routes, per-segment not-found and error pages (a client error boundary per directory with reset), and a reworked SPA router with layout persistence are native Krate features the migrator maps onto. A real migrated App Router app runs in the browser conformance suite on Chromium, Firefox, and WebKit.",
    exit: "A migrated Next.js app with async data, revalidation, client islands, a template, and an intercepting modal passes browser conformance.",
  },
  {
    id: "N4",
    tone: "gate",
    status: "Complete",
    title: "Actions, RSC, and route handlers",
    body: "Server actions, form actions in server components, route-handler params, common next/server helpers rewritten onto the standard Request/Response API (there are no NextRequest/NextResponse classes), server-component error boundaries, out-of-order sibling Suspense streaming, and useParams/useSelectedLayoutSegment(s) are implemented.",
    exit: "Forms and mutations work without a client-side React runtime; unsupported RSC features are reported. A no-JS action post carries its returned state across the redirect in a flash cookie, but pre-rendered pages do not display it without script.",
  },
  {
    id: "N5",
    tone: "gate",
    status: "v1 gate",
    title: "Verification and rollback",
    body: "Verification is implemented: krate migrate nextjs --verify builds the migrated app, runs checks, diffs the route inventory and API targets, and keeps the rollback manifest. App Router, Pages Router, and client/dynamic reference apps are covered, and the complex app runs browser conformance. The v1 gate closes at release.",
    exit: "Three reference apps migrate with build and check green, zero silent drops, and a working rollback.",
  },
];

const FOUNDATIONS = [
  {
    title: "Public compatibility matrix",
    body: "One page listing every surface, its source/island/SSR status, and the exact known divergence. It becomes the support contract.",
  },
  {
    title: "Corpus and scoreboard",
    body: "Real applications run on every release. Regressions are release blockers, not issue reports.",
  },
  {
    title: "Server-rendered islands",
    body: "Compiled packages render their initial markup with Preact, hydrate in place, and keep local components, error boundaries, and the page signal bridge.",
  },
  {
    title: "Diagnostics and codemods",
    body: "Every unsupported pattern gets a stable diagnostic code and, where safe, a codemod. No generic unresolved-component warnings.",
  },
  {
    title: "Performance budgets",
    body: "Enforce island JavaScript, hydration time, and route output budgets in CI so compatibility work cannot silently regress the static-first story.",
  },
  {
    title: "Security and release policy",
    body: "CSP guidance, remote MCP authentication, migration supply-chain checks, a beta channel, semver, and a documented deprecation policy.",
  },
];

const NOT_PLANNED = [
  "A second React island runtime. Preact remains the only island runtime; React 19 support is compiler and compat work.",
  "Reimplementing React's reconciler for source components. Krate stays signal-based and reports unsupported semantics.",
  "Bit-for-bit Next.js parity. The migration targets the common App Router surface, not every experimental feature.",
  "Native desktop or mobile targets. Those remain separate plans.",
  "Requiring Node.js to build a Krate app.",
  "Silent best-effort migrations. Unsupported code is always reported.",
];

export default function Roadmap() {
  return (
    <article class="section roadmap">
      <Head>
        <title>Roadmap - Krate</title>
        <meta name="description" content="The public Krate roadmap: React and Preact compatibility, the MCP overhaul, and the Next.js migration CLI." />
      </Head>

      <h1 class="section-title">Roadmap</h1>
      <p class="section-lede">
        Krate is in public beta. This roadmap is outcome-based: scope and order can
        change, but the v1 gate does not. Every claim below is tied to a test, a
        matrix entry, or a migration report.
      </p>
      <div class="roadmap-meta">
        <span class="roadmap-status roadmap-status-beta">Public beta</span>
        <span>Last updated: {LAST_UPDATED}</span>
        <span>Target: v1.0</span>
      </div>
      <div class="hero-actions">
        <a class="btn btn-primary" href="https://github.com/kratejs/krate/milestones">Milestones</a>
        <a class="btn btn-secondary" href="/docs/guides/compatibility/">Compatibility matrix</a>
        <a class="btn btn-secondary" href="/docs/guides/react-compatibility/">React compatibility</a>
        <a class="btn btn-secondary" href="/docs/features/mcp/">MCP</a>
        <a class="btn btn-secondary" href="/docs/guides/migrating-from-nextjs/">Next.js migration</a>
      </div>

      <section class="roadmap-section" aria-labelledby="v1-bar">
        <h2 id="v1-bar" class="roadmap-title">The v1 bar</h2>
        <p class="roadmap-lede">
          Compatibility is a product contract. These four rules decide what ships.
        </p>
        <div class="features roadmap-cards">
          {V1_BAR.map((item) => (
            <div class="feature-card">
              <h3>{item.title}</h3>
              <p>{item.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section class="roadmap-track" aria-labelledby="react">
        <div class="roadmap-track-head">
          <div>
            <h2 id="react" class="roadmap-title">React and Preact compatibility</h2>
            <p class="roadmap-lede">
              React 18 parity for source components, React 19 source semantics,
              and production-grade compiled islands.
            </p>
          </div>
          <span class="roadmap-status roadmap-status-now">In progress</span>
        </div>

        <div class="roadmap-split">
          <div class="roadmap-panel">
            <h3>Today</h3>
            <p>
              Source components match React 18 and React 19 in a browser
              differential against React itself: effects, class lifecycles, error
              boundaries, events, forms, keys, refs, Suspense, use, and the action
              hooks. Radix primitives run as Preact islands, and controlled
              primitives are covered by a real Chromium, Firefox, and WebKit
              conformance harness.
            </p>
          </div>
          <div class="roadmap-panel">
            <h3>Target</h3>
            <p>
              A published matrix for source, islands, and SSR. React 19 support is
              implemented in the compiler and compat layer; Preact remains the only
              island runtime. Unsupported package semantics are diagnosed, never
              hidden.
            </p>
          </div>
        </div>

        <div class="roadmap-callout">
          <strong>Runtime policy.</strong> There will be no second React island
          runtime. React 19 support means source-level semantics plus a tested
          Preact compat surface, with documented divergences for compiled packages.
        </div>

        <div class="roadmap-callout">
          <strong>Live matrix.</strong> The source, island, and SSR status for
          every surface is published in the <a href="/docs/guides/compatibility/">compatibility matrix</a>,
          with its test references and known divergences.
        </div>
        <h3 class="roadmap-subtitle">Phases</h3>
        <ol class="roadmap-phases">
          {REACT_PHASES.map((phase) => (
            <li class="roadmap-phase">
              <div class="roadmap-phase-meta">
                <span class="roadmap-phase-id">{phase.id}</span>
                <span class={"roadmap-status roadmap-status-" + phase.tone}>{phase.status}</span>
              </div>
              <div class="roadmap-phase-body">
                <h3>{phase.title}</h3>
                <p>{phase.body}</p>
                <p class="roadmap-exit"><strong>Exit:</strong> {phase.exit}</p>
              </div>
            </li>
          ))}
        </ol>
      </section>

      <section class="roadmap-track" aria-labelledby="mcp">
        <div class="roadmap-track-head">
          <div>
            <h2 id="mcp" class="roadmap-title">MCP overhaul</h2>
            <p class="roadmap-lede">
              From a collection of tools to an agent workspace over the compiler
              graph.
            </p>
          </div>
          <span class="roadmap-status roadmap-status-gate">Complete</span>
        </div>

        <div class="roadmap-split">
          <div class="roadmap-panel">
            <h3>Today</h3>
            <p>
              <code>krate mcp</code> provides stdio and Streamable HTTP,
              structured results, transactions, undo, safe codemods,
              diagnostics, semantic workspace symbols, project search,
              pagination, progress, resource subscriptions, evaluated agent
              prompts, run_task, cancellation, a project policy, redacted audit
              logs, rate limits, and hosted health/metrics.
            </p>
          </div>
          <div class="roadmap-panel">
            <h3>Target</h3>
            <p>
              A hardened MCP workspace with semantic graph operations, automatic
              diagnostic fixes and codemods, stronger remote operations, and
              reviewable agent workflows.
            </p>
          </div>
        </div>

        <div class="roadmap-split">
          <div class="roadmap-panel">
            <h3>New tools</h3>
            <ul class="roadmap-list">
              <li><code>get_diagnostics</code> with stable codes and fixes</li>
              <li><code>apply_patch</code>, <code>batch_edit</code>, and <code>undo</code></li>
              <li><code>search_project</code>, <code>read_files</code>, and <code>impact</code></li>
              <li><code>run_task</code> for build, check, test, and typecheck</li>
            </ul>
          </div>
          <div class="roadmap-panel">
            <h3>Acceptance</h3>
            <ul class="roadmap-list">
              <li>In-repo protocol and HTTP conformance suite</li>
              <li>Cancellation, reconnect, and SSE replay tests</li>
              <li>No stdout contamination in stdio mode</li>
              <li>Authenticated HTTP transport</li>
              <li>Structured results and pagination</li>
            </ul>
          </div>
        </div>

        <ol class="roadmap-phases">
          {MCP_PHASES.map((phase) => (
            <li class="roadmap-phase">
              <div class="roadmap-phase-meta">
                <span class="roadmap-phase-id">{phase.id}</span>
                <span class={"roadmap-status roadmap-status-" + phase.tone}>{phase.status}</span>
              </div>
              <div class="roadmap-phase-body">
                <h3>{phase.title}</h3>
                <p>{phase.body}</p>
                <p class="roadmap-exit"><strong>Exit:</strong> {phase.exit}</p>
              </div>
            </li>
          ))}
        </ol>
      </section>

      <section class="roadmap-track" aria-labelledby="migration">
        <div class="roadmap-track-head">
          <div>
            <h2 id="migration" class="roadmap-title">Migration CLI</h2>
            <p class="roadmap-lede">
              <code>krate migrate nextjs</code> turns a Next.js project
              into a reviewable Krate migration, not a best-effort rewrite.
            </p>
          </div>
          <span class="roadmap-status roadmap-status-now">Beta</span>
        </div>

        <div class="roadmap-panel">
          <h3>Command shape</h3>
          <pre class="roadmap-code"><code>{`krate migrate nextjs --report migration-report.json
krate migrate nextjs --json
krate migrate nextjs --apply --verify --force --update-package
krate migrate rollback .krate/migrate/nextjs-<timestamp>`}</code></pre>
          <p class="roadmap-note">
            Dry run is the default. Existing targets require --force, every
            apply is backed up, and the report lists routes, dependencies, and
            manual work before anything is written.
          </p>
        </div>

        <div class="roadmap-split">
          <div class="roadmap-panel">
            <h3>What moves</h3>
            <ul class="roadmap-list">
              <li>App/Pages Router routes, layouts, loading, error, API, and middleware</li>
              <li>Links, images, head/metadata, navigation, headers, and dynamic imports</li>
              <li>tsconfig, path aliases, public env, redirects, rewrites, headers, and images</li>
              <li>Package scripts and Krate dependencies</li>
              <li>App-local relative import graphs</li>
            </ul>
          </div>
          <div class="roadmap-panel">
            <h3>What is guaranteed</h3>
            <ul class="roadmap-list">
              <li>A JSON report before any file is written</li>
              <li>Every unsupported construct listed with a diagnostic</li>
              <li>Backups and rollback for applied migrations</li>
              <li>Route inventory with dependencies and per-route status</li>
              <li>No silent drops and no overwrite without a flag</li>
            </ul>
          </div>
        </div>

        <h3 class="roadmap-subtitle">Phases</h3>
        <ol class="roadmap-phases">
          {MIGRATION_PHASES.map((phase) => (
            <li class="roadmap-phase">
              <div class="roadmap-phase-meta">
                <span class="roadmap-phase-id">{phase.id}</span>
                <span class={"roadmap-status roadmap-status-" + phase.tone}>{phase.status}</span>
              </div>
              <div class="roadmap-phase-body">
                <h3>{phase.title}</h3>
                <p>{phase.body}</p>
                <p class="roadmap-exit"><strong>Exit:</strong> {phase.exit}</p>
              </div>
            </li>
          ))}
        </ol>
      </section>

      <section class="roadmap-section" aria-labelledby="serverless">
        <h2 id="serverless" class="roadmap-title">Serverless and edge status</h2>
        <p class="roadmap-lede">
          What runs where today, stated plainly. The same portable server
          handler serves every target below; it is tested against <code>krate start</code>
          request for request on Node, and the Cloudflare worker has been run
          in a local workerd. Vercel and Netlify output is validated against
          the platform file formats and by calling the handlers directly, not
          by a live deployment.
        </p>
        <ul class="roadmap-list">
          <li>Node (<code>krate deploy --target node</code>): SSR, ISR, streaming, API routes, actions, middleware. No Go binary needed.</li>
          <li>Vercel, Netlify, Cloudflare: the same features through one function or worker; caches are per instance.</li>
          <li>Not on serverless: Go API routes, Go components, a custom API sidecar, community plugins with serve hooks. <code>krate deploy</code> refuses them and points at Docker.</li>
          <li>Planned: a shared cache store for ISR and revalidation across instances (the handler already takes an <code>isrStore</code>).</li>
        </ul>
      </section>

      <section class="roadmap-section" aria-labelledby="foundations">
        <h2 id="foundations" class="roadmap-title">What else has to be true</h2>
        <p class="roadmap-lede">
          These are not side quests. They are the conditions that make the three
          tracks credible.
        </p>
        <div class="features roadmap-cards">
          {FOUNDATIONS.map((item) => (
            <div class="feature-card">
              <h3>{item.title}</h3>
              <p>{item.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section class="roadmap-section roadmap-not-planned" aria-labelledby="not-planned">
        <h2 id="not-planned" class="roadmap-title">Not planned for v1</h2>
        <ul class="roadmap-list">
          {NOT_PLANNED.map((item) => <li>{item}</li>)}
        </ul>
      </section>

      <section class="cta-band">
        <h2>Follow the work</h2>
        <p>
          Milestones are the source of truth. Compatibility claims and migration
          reports will link back to the tests that prove them.
        </p>
        <div class="hero-actions">
          <a class="btn btn-primary" href="https://github.com/kratejs/krate/milestones">GitHub milestones</a>
          <a class="btn btn-secondary" href="/docs/guides/react-compatibility/">Compatibility guide</a>
          <a class="btn btn-secondary" href="/docs/guides/migrating-from-nextjs/">Migration guide</a>
        </div>
      </section>
    </article>
  );
}