// Package searchassets embeds the docs search UI files (search.js + search.css)
// that the docs plugin writes into the output directory at build time.
package searchassets

import _ "embed"

//go:embed search.js
var SearchJS []byte

//go:embed search.css
var SearchCSS []byte
