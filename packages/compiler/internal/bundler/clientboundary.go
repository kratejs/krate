package bundler

import "strings"

// HasUseClientDirective reports whether a module starts with the `"use client"`
// directive (a React client boundary). Only the directive prologue counts: the
// literal must come before any other statement, after optional comments,
// a hashbang, and other directives such as "use strict".
func HasUseClientDirective(source string) bool {
	s := source
	if strings.HasPrefix(s, "#!") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		} else {
			return false
		}
	}
	for {
		s = strings.TrimLeft(s, " \t\r\n\uFEFF")
		switch {
		case strings.HasPrefix(s, "//"):
			nl := strings.IndexByte(s, '\n')
			if nl < 0 {
				return false
			}
			s = s[nl+1:]
			continue
		case strings.HasPrefix(s, "/*"):
			end := strings.Index(s[2:], "*/")
			if end < 0 {
				return false
			}
			s = s[end+4:]
			continue
		}
		if len(s) == 0 || (s[0] != '"' && s[0] != '\'') {
			return false
		}
		q := s[0]
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return false
		}
		lit := s[1 : end+1]
		if lit == "use client" {
			return true
		}
		// Another directive ("use strict"): keep scanning the prologue.
		s = strings.TrimLeft(s[end+2:], " \t")
		s = strings.TrimPrefix(s, ";")
	}
}
