---
title: Advanced Usage
order: 1
sidebar: [{"title":"← Back to Getting Started","url":"/docs/getting-started/"},{"title":"Guides","children":[{"title":"Welcome to Advanced","url":"/docs/guides/"},{"title":"Advanced Usage","url":"/docs/guides/advanced/","active":true}]}]
---

# Advanced Usage

This guide covers advanced features of the Krate framework.

## Custom Plugins

Krate supports two types of plugins:

### Built-in Plugins

Registered in Go code via `init()` and `plugin.Register()`:

```go
func init() {
  plugin.Register(plugin.NewFunc("my-plugin", plugin.PhaseAfterBuild, func(ctx *plugin.Context) error {
    // Do something after all pages are built
    return nil
  }))
}
```

### Community Plugins

External Go binaries communicate via JSON over stdin/stdout:

```typescript
import myPlugin from "./plugins/my-plugin/index.mjs";

export default {
  plugins: [
    myPlugin({ option: "value" }),
  ],
};
```

### PluginV2 (Enhanced)

The new PluginV2 interface provides lifecycle hooks:

- `BeforeBuild` — before build starts
- `AfterParse` — after AST parsing
- `AfterMarkdownParse` — after markdown rendering
- `AfterRender` — after HTML rendering
- `GenerateRoutes` — generate virtual pages

## Partial Reloads

In dev mode, Krate tracks file dependencies and only rebuilds affected pages when a file changes. This makes development significantly faster for large projects.

## CSS Modules

CSS Modules are always on for `*.module.css` files. Import them in your components:

```tsx
import styles from "./Button.module.css";
```

Class names are automatically scoped with hashed suffixes.
