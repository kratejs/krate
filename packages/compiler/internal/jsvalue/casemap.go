package jsvalue

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// jsUpper and jsLower implement String.prototype.toUpperCase/toLowerCase
// with the full, locale-independent Unicode mappings (so "ß" becomes "SS"
// and a word-final capital sigma lowercases to "ς"), which strings.ToUpper's
// simple one-to-one mappings do not.
func jsUpper(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	return cases.Upper(language.Und).String(s)
}

func jsLower(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	return cases.Lower(language.Und).String(s)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
