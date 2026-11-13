package native

import "github.com/kratejs/krate/packages/compiler/internal/config"

// MatchBrace returns the offset just past the `}` that closes the `{` at open,
// or -1. It skips strings, template literals and comments.
func MatchBrace(src string, open int) int { return matchBrace(src, open) }

// SkipQuoted returns the index of the closing quote of the string at i.
func SkipQuoted(src string, i int) int { return skipQuoted(src, i) }

// SkipTemplate returns the index of the closing backtick of the template at i.
func SkipTemplate(src string, i int) int { return skipTemplate(src, i) }

// ProjectFiles lists the source files placement and the target checks read:
// everything under src, the pages dir and the hostDirs.
func ProjectFiles(root string, cfg *config.Config) ([]string, error) { return projectFiles(root, cfg) }
