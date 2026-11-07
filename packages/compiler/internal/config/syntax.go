package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// configSyntaxError reports a real syntax error in a config file as a
// diagnostic with file:line:col and a hint, or nil when the source parses. It
// runs only after evaluation failed, to tell "does not parse" (the user must
// fix the file) apart from "could not be evaluated" (the static fallback may
// still apply).
func configSyntaxError(path string, src []byte) error {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return jsonSyntaxError(path, src)
	}
	d, ok := parseFailure(path, src)
	if !ok {
		return nil
	}
	d.Message = "syntax error in config: " + d.Message
	return d
}

// parseFailure parses one file on its own and reports the first syntax error.
func parseFailure(path string, src []byte) (diag.Diagnostic, bool) {
	loader := api.LoaderTS
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs":
		loader = api.LoaderJS
	case ".jsx":
		loader = api.LoaderJSX
	case ".tsx":
		loader = api.LoaderTSX
	}
	res := api.Transform(string(src), api.TransformOptions{
		Loader:     loader,
		Format:     api.FormatESModule,
		Sourcefile: path,
		LogLevel:   api.LogLevelSilent,
	})
	if len(res.Errors) == 0 {
		return diag.Diagnostic{}, false
	}
	msg := res.Errors[0]
	d := diag.Diagnostic{File: path, Line: 1, Col: 1, Message: msg.Text, Hint: configSyntaxHint(path)}
	if loc := msg.Location; loc != nil {
		d.Line = loc.Line
		d.Col = loc.Column + 1 // esbuild columns are 0-based
		d.Source = strings.TrimRight(loc.LineText, "\r\n")
	}
	return d, true
}

// importedSyntaxError turns a bundling failure into a diagnostic when the cause
// is a syntax error in a local file the config imports. Other bundling failures
// (a package that is not installed, an unresolvable path) return nil so callers
// keep their own message. Each failing file is re-parsed alone, so a file that
// parses fine is never blamed.
func importedSyntaxError(configPath string, err error) error {
	var be *jseval.BundleError
	if !errors.As(err, &be) {
		return nil
	}
	seen := map[string]bool{}
	for _, m := range be.Messages {
		if m.File == "" || seen[m.File] || filepath.Clean(m.File) == filepath.Clean(configPath) {
			continue
		}
		seen[m.File] = true
		src, rerr := os.ReadFile(m.File)
		if rerr != nil {
			continue
		}
		if strings.EqualFold(filepath.Ext(m.File), ".json") {
			if jerr := jsonSyntaxError(m.File, src); jerr != nil {
				return jerr
			}
			continue
		}
		if d, ok := parseFailure(m.File, src); ok {
			d.Message = "syntax error in " + filepath.Base(m.File) + ", imported by the config: " + d.Message
			return d
		}
	}
	return nil
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
