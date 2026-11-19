// Package jsscan finds the code of a JavaScript or TypeScript source: it
// tells code from comments, strings, regex literals and template text.
package jsscan

import "strings"

// Mask marks, for each byte of a JavaScript/TypeScript source, whether it
// is code: false inside comments, string literals, regex literals and the
// text parts of template literals. The expressions of a `${...}` substitution
// are code.
func Mask(src string) []bool {
	mask := make([]bool, len(src))
	type frame struct {
		template bool
		braces   int
	}
	stack := []frame{{}}
	var prev byte // last significant byte seen in code
	for i := 0; i < len(src); {
		top := &stack[len(stack)-1]
		c := src[i]
		if top.template {
			switch {
			case c == '\\':
				i += 2
			case c == '`':
				stack = stack[:len(stack)-1]
				i++
				prev = '`'
			case c == '$' && i+1 < len(src) && src[i+1] == '{':
				stack = append(stack, frame{})
				i += 2
			default:
				i++
			}
			continue
		}
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
			} else {
				i += end + 4
			}
		case c == '\'' || c == '"':
			i++
			for i < len(src) && src[i] != c && src[i] != '\n' {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			i++
			prev = c
		case c == '`':
			stack = append(stack, frame{template: true})
			i++
		case c == '/' && regexAllowedAfter(prev):
			i = skipRegex(src, i)
			prev = '/'
		case c == '}' && top.braces == 0 && len(stack) > 1:
			stack = stack[:len(stack)-1]
			i++
		default:
			if c == '{' {
				top.braces++
			} else if c == '}' && top.braces > 0 {
				top.braces--
			}
			mask[i] = true
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				prev = c
			}
			i++
		}
	}
	return mask
}

// regexAllowedAfter reports whether a `/` after the byte prev starts a regex
// literal rather than a division.
func regexAllowedAfter(prev byte) bool {
	return prev == 0 || strings.IndexByte("(,=:[!&|?{};+-*%<>~^", prev) >= 0
}

func skipRegex(src string, i int) int {
	i++
	inClass := false
	for i < len(src) && src[i] != '\n' {
		switch {
		case src[i] == '\\':
			i++
		case src[i] == '[':
			inClass = true
		case src[i] == ']':
			inClass = false
		case src[i] == '/' && !inClass:
			i++
			for i < len(src) && IsIdentChar(src[i]) {
				i++
			}
			return i
		}
		i++
	}
	return i
}

// IsIdentChar reports whether c can be part of an ASCII identifier.
func IsIdentChar(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// CodeOnly returns src with everything that is not code replaced by spaces.
func CodeOnly(src string) string {
	mask := Mask(src)
	b := []byte(src)
	for i := range b {
		if !mask[i] && b[i] != '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}

// Pairs returns, for the code bracket at each index of src, the index of its
// partner, and -1 for every other byte (and for an unbalanced bracket). mask
// is Mask(src).
func Pairs(src string, mask []bool) []int {
	out := make([]int, len(src))
	for i := range out {
		out[i] = -1
	}
	var stack []int
	for i := 0; i < len(src); i++ {
		if !mask[i] {
			continue
		}
		switch src[i] {
		case '(', '[', '{':
			stack = append(stack, i)
		case ')', ']', '}':
			if n := len(stack); n > 0 {
				out[i], out[stack[n-1]] = stack[n-1], i
				stack = stack[:n-1]
			}
		}
	}
	return out
}
