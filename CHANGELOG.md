# Changelog

Krate uses [GitHub Releases](https://github.com/kratejs/krate/releases) for the
detailed changelog. This file records the release train at a glance.

## Unreleased

- Fixed: a markdown autolink such as `<javascript:alert(1)>` or `<data:text/html,x>`
  was emitted as a link. Autolinks go through the same scheme check as `[x](url)`
  links now and render as text. The icon sanitizer also drops SMIL `set`/`animate`
  that target `href`, `<style>` that imports or fetches external files,
  external `<image>`, `<iframe>`, `<object>` and `<embed>`.
- Fixed: `<Image width={1000}>` (also 2000, 3000, 10000) failed with `KR8803`.
  Images are now sized to what you ask for: `width={100}` ships 100w and 200w
  variants and `sizes="100px"` instead of six 640-2000w files. JPEGs with an EXIF
  orientation (phone photos) are rotated before resizing, so the variants and their
  dimensions match what the browser shows, and GIFs work (animated ones are served
  as they are).
- Fixed: page-level and imported `.md`/`.mdx` ignored the `markdown` config (`math`,
  `mermaid`, `gfm`, `admonitions`, `headingAnchors`, `codeTheme`).
- Fixed: a markdown list, quote or table directly inside a multi-line JSX element
  in MDX swallowed the closing tag and failed the build with an error pointing into
  generated code. A parse failure in compiled markdown now says its position refers
  to the generated code.
- Relative images in markdown, MDX and content collections (`![x](./pic.png)`) are
  copied to `/assets/` with a hashed name and linked there, under `basePath`.
- Content: enum fields are typed as unions, fields with a `default` are no longer
  optional, `date` fields must hold an ISO date (a validation error otherwise),
  hidden files such as editor lock files are skipped, and two entries with the same
  slug (`a.md` and `a.mdx`) are an error. Frontmatter folds multi-line quoted
  strings, and a `+++` (TOML) or unterminated block warns instead of silently
  rendering as text.
- Markdown code: fences with `{1,3-5}` highlight those lines on pages as well as in
  collections, titled fences keep their header inside asides, `mdx`/`astro`/`prisma`
  are highlighted, and short code blocks and tables no longer add a focus stop and a
  landmark each (only those likely to scroll do).
- `<Icon>` sizes to the text (`1em`) like Iconify's own SVGs, keeps a solid icon's
  `fill` instead of outlining it, and maps `size={n}` to width and height.
- Tailwind: `shadow-xl/20`, `drop-shadow-xl/50`, `inset-shadow-*/n`, `text-shadow-*/n`,
  `bg-size-[...]` and `bg-position-[...]` are generated.
- Fixed: `Tabs`, `Accordion` and other zero-JS components built from a `.map()` over a
  build-time array silently showed every panel and ignored `defaultValue`. Their
  values are collected from the unrolled array now; a value that cannot be read at
  build time warns with `KR8820`. A trigger's `class` may be computed.
- Components: `OTPField` edits the visible digits (Backspace, paste, autofill) and
  submits under `name`; the searchable `Select` and `Command` are keyboard operable
  comboboxes with an active descendant; `NativeSelect` shows its placeholder;
  `ContextMenu` opens from the keyboard (ContextMenu key, Shift+F10) with menu roles
  and focus management; `Tooltip` and `HoverCard` are linked with `aria-describedby`,
  close on Escape, can be hovered, and take `asChild` for a real button or link;
  `Pagination` links use `param` and keep the other query parameters; `Checkbox` and
  `Switch` are native inputs with `name`, `value`, `defaultChecked`, `disabled` and
  `id`; every component takes `class`/`className`; modal `Dialog`, `AlertDialog`
  and `Sheet` dim the page, make it inert, lock scrolling and close on an outside
  click; `Form.Field` links its description and error to the control and marks it
  `aria-invalid`; `Card` titles are headings; `Slider` takes `id` and `disabled`.
  Focus rings use one colour and no longer rely on a 15% glow.
- Fixed: docs search snippets showed `&quot;` and `&#39;`.
- Tier decorators: `@server`, `@static`, `@runtime` and `@client` can go directly
  on a function, class or const. On a file's default export or only exported
  component they classify the whole file, like a `// @server` comment; elsewhere
  they classify one component. Misplaced ones fail with `KR7115`.
- `krate typecheck` runs `krate types` and `tsc --noEmit` without the errors `tsc`
  reports for tier decorators, and `@krate/ts-plugin` does the same in editors.
  Scaffolds use both.
- Fixed: modules on a page shared one namespace, so two files with a private
  `up`, `Row` or `LABEL`, or two default exports named `Card`, showed one module's
  value for both, in static HTML and in client code. They are now kept apart. A
  page that imports one name from two modules gets `KR7116`.
- Decorators work in `.js` and `.jsx` files, not only TypeScript.
- A decorated class component now always warns (`KR3011`), including member
  decorators and legacy decorators, which used to report an unresolved component
  or drop the decorator silently.
- Fixed: an import from an alias like `@server/lib` near the top of a file changed
  the tier of its components.
- `@krate/core` no longer publishes a stray Linux compiler binary, and the npm
  wrapper picks the platform package when installed from a registry. In
  `0.5.0-beta.1` a clean npm or pnpm install failed on Linux with
  `spawn .../bin/krate EACCES`.
- Build failures print the diagnostic hint. A broken tsconfig path alias now
  shows the directory its targets resolved against.
- `krate version` reports the native binary instead of the npm wrapper, and warns
  when the two differ.
- Backslashes in tsconfig `baseUrl` and `paths` are normalized, so a config
  written on Windows resolves on Linux and macOS.
- `krate deploy` runs server features on more hosts. A portable server handler
  (`@krate/runtime/server-handler`) is bundled with the build data into one
  module for `--target node` (new: `dist/.krate/server/server.mjs`, no Go
  binary), and for `vercel` (Build Output API v3), `netlify` (Functions) and
  `cloudflare` (Workers with static assets) instead of refusing projects with
  SSR/ISR/API routes/actions/middleware. A request matrix checks the node target
  against `krate start`. Go API routes, Go components and sidecars are refused
  with a hint to use Docker. Migration note: caches are per instance on the
  serverless targets, so `revalidatePath`/`revalidateTag` reach only the
  instance that handled the request (reported under `skipped`).
- Per-segment not-found and error pages: `404.tsx`/`not-found.tsx` and
  `error.tsx`/`500.tsx` in any pages directory. The nearest not-found page
  answers unmatched URLs, closed dynamic params and `notFound()` (404, inside
  the segment's layouts); the nearest error page answers uncaught server errors
  (500, sanitized message plus a digest, never cached) and is a client error
  boundary around the segment with `reset()`. Two files for one role, or a
  segment page in a `@slot`, are build errors (`KR9001`, `KR9002`). The Next.js
  migrator maps nested `not-found.tsx`, `error.tsx` and `global-error.tsx` to
  native files instead of manual items or a generated class-boundary layout.
  Behavior change: a page that renders inside an error boundary no longer
  disables layout persistence (the runtime now tears down class-component owners
  by route scope).
- Tailwind output is layered like v4 (`@layer theme, base, components,
  utilities`). Migration note: unlayered CSS beats Tailwind utilities, so a
  global reset such as `* { margin: 0; padding: 0 }` must move into
  `@layer base { ... }` or it will override `p-*`/`m-*` utilities.

## 0.5.0-alpha.4

- Next.js migration maps App Router routing, server components, client
  boundaries, cache/revalidation, and framework features.
- Docs and marketing site updated to the new SVG logo and alpha.4 packages.
- Server, MCP, migration, and runtime hardening fixes.
- Serverless targets now serve per-segment not-found and error pages (nearest
  page, inside layouts, `no-store`, sanitized error with a digest, never cached
  by ISR) and run middleware for prerendered pages: with middleware, Vercel and
  Netlify route page requests to the function (assets stay on the CDN) instead
  of bypassing it, and the deploy warning is gone. The error digest is the same
  value on every host (portable sha256 in the render core).
