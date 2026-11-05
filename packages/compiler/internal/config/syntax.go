package config

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// configSyntaxError reports a real syntax error in a config file as a
// diagnostic with file:line:col and a hint, or nil when the source parses. It
// runs only after evaluation failed, to tell "does not parse" (the user must
// fix the file) apart from "could not be evaluated" (the static fallback may
// still apply).
func configSyntaxError(path string, src []byte) error {
	loader := api.LoaderTS
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs":
		loader = api.LoaderJS
	case ".json":
		return jsonSyntaxError(path, src)
	}
	res := api.Transform(string(src), api.TransformOptions{
		Loader:     loader,
		Format:     api.FormatESModule,
		Sourcefile: path,
		LogLevel:   api.LogLevelSilent,
	})
	if len(res.Errors) == 0 {
		return nil
	}
	msg := res.Errors[0]
	d := diag.Diagnostic{File: path, Line: 1, Col: 1, Message: "syntax error in config: " + msg.Text, Hint: configSyntaxHint(path)}
	if loc := msg.Location; loc != nil {
		d.Line = loc.Line
		d.Col = loc.Column + 1 // esbuild columns are 0-based
		d.Source = strings.TrimRight(loc.LineText, "\r\n")
	}
	return d
}

func configSyntaxHint(path string) string {
	return "check " + filepath.Base(path) + " near this position for a missing comma, bracket, quote or stray text"
}

// jsonSyntaxError converts an encoding/json syntax error into a diagnostic.
func jsonSyntaxError(path string, src []byte) error {
	var v any
	err := json.Unmarshal(src, &v)
	var se *json.SyntaxError
	if !errors.As(err, &se) {
		return nil
	}
	off := int(se.Offset) - 1 // Offset counts bytes read, including the offender
	if off < 0 {
		off = 0
	}
	if off > len(src) {
		off = len(src)
	}
	line, col := 1, 1
	lineStart := 0
	for i := 0; i < off && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			col = 1
			lineStart = i + 1
			continue
		}
		col++
	}
	lineEnd := lineStart
	for lineEnd < len(src) && src[lineEnd] != '\n' && src[lineEnd] != '\r' {
		lineEnd++
	}
	return diag.Diagnostic{
		File:    path,
		Line:    line,
		Col:     col,
		Message: "invalid JSON in config: " + se.Error(),
		Hint:    "check " + filepath.Base(path) + " near this position for a missing comma, bracket or quote; JSON does not allow comments or trailing commas",
		Source:  string(src[lineStart:lineEnd]),
	}
}
