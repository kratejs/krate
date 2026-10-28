---
title: Features
order: 1
description: A tour of the features that ship with Krate beyond the core compiler.
sidebar:
  label: Features
  order: 2
  collapsible: true
  defaultOpen: true
---

# Features

A tour of the features that ship with Krate beyond the core compiler.

| Feature | Description |
|---------|-------------|
| [Built-in Components](/docs/features/built-in-components/) | `<Head>`, `<Script>`, `<Style>`, `<Link>`, `<Icon>`, `<Image>` |
| [Image Optimization](/docs/features/image-optimization/) | Compile-time WebP-first `<picture>` pipeline |
| [Data Fetching](/docs/features/data-fetching/) | Component tiers, ISR/SSR page data, `generateStaticParams`, `createResource`, API routes |
| [Async Server Components](/docs/features/server-components/) | `async` components that fetch per request, request APIs, `"use client"` boundaries |
| [API Routes](/docs/features/api-routes/) | TypeScript and Go endpoints in one namespace |
| [Go Components](/docs/features/go-components/) | Server and runtime components written in Go, importing any Go package |
| [Plugin System](/docs/features/plugins/) | Go plugin hooks + JS/TS community plugins via QuickJS, typed with `@krate/plugin` |
| [Search](/docs/features/search/) | Docs search bar: Pagefind (default, recommended), docfind WASM, or JSON |
| [Redirects & Rewrites](/docs/features/redirects-rewrites/) | URL manipulation from config |
| [Middleware](/docs/features/middleware/) | Request interception before page rendering |
| [Environment Variables](/docs/features/environment-variables/) | `.env` files, precedence, `process.env` in server contexts |
| [Analytics](/docs/features/analytics/) | Snippets via `<Head>`/`<Script>` + the SPA route-change hook |
| [HTTP Headers](/docs/features/http-headers/) | Middleware headers, automatic cache/security headers, ISR `Cache-Control` |
| [Typed Routes & Content](/docs/features/typed-routes/) | Generated `.d.ts` for routes, params, and content collections |
| [Quality Checks](/docs/features/quality-checks/) | Compiler-enforced a11y, SEO, and performance gates (`krate check`) |
| [MCP Server](/docs/features/mcp/) | Agent-native compiler surface over the Model Context Protocol (`krate mcp`) |

Also see the [component library](/docs/reference/component-library/), a
shadcn/ui-style set of components built on the runtime.
