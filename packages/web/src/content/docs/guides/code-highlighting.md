---
title: Code Highlighting
description: How Krate highlights code at build time, at request time and in the browser, in light and dark.
order: 19
---

# Code Highlighting

Krate highlights code with [chroma](https://github.com/alecthomas/chroma). Which
part of the system does it depends on when the code is known:

| The code is known | Who highlights it | JavaScript sent to the browser |
|-------------------|-------------------|--------------------------------|
| At build time (a literal, a template string, a Markdown fence) | chroma, in the compiler | None |
| At request time (an SSR or ISR page) | The same lexers, in the Node renderer | None |
| In the browser (a signal, fetched data) | The same lexers, loaded on demand | The tokenizer plus that language |

All three write the same markup: one `<span class="line"><span class="cl">` per line
and one classed span per token, so one stylesheet colours them all and a block
looks the same whichever way it was made. The browser and request-time paths run
chroma's own lexer rules, translated to JavaScript when the site is built, so
they match chroma token for token (see [How it stays exact](#how-it-stays-exact)).

## Light and dark

`markdown.codeTheme` chooses the colours:

```ts
markdown: {
  codeTheme: { light: "github", dark: "krate-dark" }, // the default
}
```

`krate-dark` is `github-dark`'s syntax colours on the neutral surface Krate's
cards use, and `github-dark` is still available by name. A plain string,
`codeTheme: "monokai"`, is one theme for both modes. The
stylesheet follows the page: the system preference, or a `data-theme="light"` or
`data-theme="dark"` attribute on `<html>`, including when it changes at run time.
It applies to the `Code` component, fenced Markdown blocks and the docs themes.
Any [chroma style](https://xyproto.github.io/splash/docs/) name works; an unknown
one warns (`KR7150`). See [Theming Components](/docs/guides/theming/) for the
`--krate-code-bg` and `--krate-code-text` overrides.

## Code that changes at run time

```tsx
const [source, setSource] = createSignal("const a = 1;");
return <Code lang="ts">{source()}</Code>;
```

The text is written as plain text first. When the signal changes the new text
shows at once, and after a short quiet moment (120 ms) it is highlighted; text that
has not changed is left alone. The tokenizer and the language's tables are a
separate chunk the build writes only for the languages a page uses (a literal
`lang` is found automatically), loaded on first use. A page without such code
loads none of it, and a `<Code>` whose text is known at build time sends no
JavaScript for highlighting.

If `lang` is not a literal, or you highlight with your own code, list the languages
to load:

```ts
// krate.config.ts
highlight: { languages: ["ts", "bash"] },
```

```ts
import { krateHighlight } from "@krate/runtime";

const html = await krateHighlight(code, "ts"); // inner HTML of a highlighted block, or null
```

`krateHighlight` resolves to `null` when the language is not available on the page
or the text is over the limits below; show it as plain text then. The same lexers
are available as `highlight(code, lang, lines?)` from `@krate/runtime/highlight`
for code that runs where the tables are already loaded.

## Request time

On SSR and ISR pages the Node renderer highlights `<Code>` and `<SyntaxHighlight>`
whose text only exists per request. The build writes the tables for the
languages the server pages use (and `highlight.languages`) to
`.krate/highlight.json`, and the renderer loads them when it starts. Pages
rendered by a serverless deploy target have no renderer process to load them and
write plain text.

## Languages

Build-time highlighting supports every chroma language. The request-time and
browser highlighters ship the languages that are checked against chroma on real
code: TypeScript (and TSX), JavaScript, JSX, JSON, CSS, HTML, Bash, Go, Python,
Rust, YAML, Markdown, TOML, XML, C and plain text. Code in another language, in
the browser or at request time, is shown as plain text with a warning (`KR7151`);
use a listed language, or make the code known at build time.

## Limits

The browser and request-time highlighters never trade exactness for speed: when a
snippet cannot be highlighted within limits, it is shown as plain text.

- Text over 64 KB, or with a line over 2,000 characters, is not highlighted.
- Highlighting stops after 400 ms and the text stays plain.
- A language that is not loaded is not highlighted.

Some lexer rules take time that grows with the square of a line's length, which is
why long lines are refused; no input can hold a page for longer than the time
limit.

## How it stays exact

The compiler turns the chroma lexers a project needs into small tables (rule lists
with each regular expression translated to JavaScript), from the lexers inside the
binary, so they always match its chroma version. The translation reproduces the
regular expression engine chroma uses (a .NET-compatible one, including its
quirks) and the tokenizer reproduces chroma's lexer state machine and HTML
formatter. The test suite compares the two on real code for every shipped
language, on files from this repository, its dependencies and the Go standard
library, plus truncated, CRLF and newline-less copies and line-highlight specs.
Any difference fails the build, including after a chroma upgrade.
