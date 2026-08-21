---
name: Bug report
about: Report a bug in Krate
title: ""
labels: bug
assignees: ""
---

## Description

A clear and concise description of the bug.

## Reproduction

Minimal steps or a minimal code sample to reproduce:

```tsx
// src/pages/index.tsx
export default function Page() {
  return <div>...</div>;
}
```

Command used (if relevant): `krate build` / `krate dev` / `go test ./...`

## Expected behavior

What you expected to happen.

## Actual behavior

What actually happened. Include error output, `⚠` build warnings, or generated HTML/JS excerpts.

## Environment

- OS: [e.g. Windows 11, macOS 14, Ubuntu 24.04]
- Krate version: [e.g. 0.0.1, commit hash]
- Go version (if building from source): `go version`
- Node/pnpm versions: `node --version` / `pnpm --version`

## Additional context

Anything else relevant.
