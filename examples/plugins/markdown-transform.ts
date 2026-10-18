import { defineMarkdownPlugin } from "@krate/plugin";

// Demonstrates `markdown.plugins`: this runs on the rendered HTML of Markdown
// pages and docs pages (AfterMarkdownParse). The marker element is visible in
// the built output.
export default defineMarkdownPlugin((html) => {
  return html + '\n<div data-markdown-plugin="ran"></div>';
});
