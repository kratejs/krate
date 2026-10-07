---
title: Diagrams
description: Render Mermaid diagrams from fenced code blocks.
order: 9
---

# Diagrams

With `markdown.mermaid: true`, fenced `mermaid` blocks render as diagrams:

```mermaid
graph TD
  A[Source TSX] --> B[Go compiler]
  B --> C[Static HTML]
  B --> D[Hydration JS]
```