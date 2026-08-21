package build

// escapeHTMLShimJS is the shared HTML-escaping helper embedded in the server
// JSX shims (jsxShimCode / ssrPageShimCode). Escaping is the XSS boundary for
// server-side rendering, so it must live in exactly one place — the two shims
// concatenate this instead of each defining their own copy.
const escapeHTMLShimJS = `function __escapeHtml(v){return String(v).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');}
`
