package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

// toolApplyPatch applies a unified diff as one atomic transaction.
func (s *Service) toolApplyPatch(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	patch := argString(args, "patch")
	if patch == "" {
		return ErrorResult("provide patch (a unified diff)"), nil
	}
	apply := argBool(args, "apply", false)
	expected := argStringMap(args, "expected")
	expectedHash := func(rel string) string {
		if v, ok := expected[rel].(string); ok {
			return v
		}
		return ""
	}

	files, err := parseUnifiedPatch(patch)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	tx := newFileTransaction(s)
	for _, pf := range files {
		switch {
		case pf.createOnly():
			abs, rel, err := s.resolveEditTarget(pf.NewPath)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			if _, statErr := os.Stat(abs); statErr == nil {
				return ErrorResult("patch creates " + rel + " but it already exists"), nil
			}
			newText, err := applyFilePatch("", pf)
			if err != nil {
				return ErrorResult(rel + ": " + err.Error()), nil
			}
			if err := validateGoSource(rel, newText); err != nil {
				return ErrorResult(err.Error()), nil
			}
			if err := tx.stageWrite(abs, rel, newText, ""); err != nil {
				return ErrorResult(err.Error()), nil
			}
		case pf.deleteOnly():
			abs, rel, err := s.resolveEditTarget(pf.OldPath)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			if err := tx.stageDelete(abs, rel, expectedHash(rel)); err != nil {
				return ErrorResult(err.Error()), nil
			}
		case pf.rename():
			oldAbs, oldRel, err := s.resolveEditTarget(pf.OldPath)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			newAbs, newRel, err := s.resolveEditTarget(pf.NewPath)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			if _, statErr := os.Stat(newAbs); statErr == nil {
				return ErrorResult("patch moves to " + newRel + " but it already exists"), nil
			}
			data, readErr := os.ReadFile(oldAbs)
			if readErr != nil {
				return ErrorResult("reading " + oldRel + ": " + readErr.Error()), nil
			}
			if h := expectedHash(oldRel); h != "" && hashString(string(data)) != h {
				return ErrorResult(oldRel + " changed since it was read; refresh and retry"), nil
			}
			newText, err := applyFilePatch(string(data), pf)
			if err != nil {
				return ErrorResult(oldRel + ": " + err.Error()), nil
			}
			if err := validateGoSource(newRel, newText); err != nil {
				return ErrorResult(err.Error()), nil
			}
			if err := tx.stageWrite(newAbs, newRel, newText, ""); err != nil {
				return ErrorResult(err.Error()), nil
			}
			if err := tx.stageDelete(oldAbs, oldRel, hashString(string(data))); err != nil {
				return ErrorResult(err.Error()), nil
			}
		default:
			abs, rel, err := s.resolveEditTarget(pf.OldPath)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			data, readErr := os.ReadFile(abs)
			if readErr != nil {
				return ErrorResult("reading " + rel + ": " + readErr.Error()), nil
			}
			newText, err := applyFilePatch(string(data), pf)
			if err != nil {
				return ErrorResult(rel + ": " + err.Error()), nil
			}
			if err := validateGoSource(rel, newText); err != nil {
				return ErrorResult(err.Error()), nil
			}
			if err := tx.stageWrite(abs, rel, newText, expectedHash(rel)); err != nil {
				return ErrorResult(err.Error()), nil
			}
		}
	}
	return s.finishTransaction(tx, apply, "apply patch")
}

// toolBatchEdit applies structured file operations as one transaction.
func (s *Service) toolBatchEdit(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	raw, ok := args["edits"].([]any)
	if !ok || len(raw) == 0 {
		return ErrorResult("provide edits (a non-empty array)"), nil
	}
	apply := argBool(args, "apply", false)
	tx := newFileTransaction(s)
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return ErrorResult(fmt.Sprintf("edits[%d] must be an object", i)), nil
		}
		target := argString(m, "path")
		if target == "" {
			target = argString(m, "route")
		}
		if target == "" {
			return ErrorResult(fmt.Sprintf("edits[%d] needs path", i)), nil
		}
		expected := argString(m, "expectedSha256")
		op := argString(m, "op")
		to := argString(m, "to")
		if to == "" {
			to = argString(m, "moveTo")
		}
		if op == "move" && to == "" {
			return ErrorResult(fmt.Sprintf("edits[%d] move needs to/moveTo", i)), nil
		}
		if op == "delete" || argBool(m, "delete", false) {
			abs, rel, err := s.resolveEditTarget(target)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			if err := tx.stageDelete(abs, rel, expected); err != nil {
				return ErrorResult(err.Error()), nil
			}
			continue
		}
		if to != "" || op == "move" {
			fromAbs, fromRel, err := s.resolveEditTarget(target)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			toAbs, toRel, err := s.resolveEditTarget(to)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			if _, statErr := os.Stat(toAbs); statErr == nil {
				return ErrorResult("destination already exists: " + toRel), nil
			}
			data, readErr := os.ReadFile(fromAbs)
			if readErr != nil {
				return ErrorResult("reading " + fromRel + ": " + readErr.Error()), nil
			}
			if expected != "" && hashString(string(data)) != expected {
				return ErrorResult(fromRel + " changed since it was read; refresh and retry"), nil
			}
			if err := validateGoSource(toRel, string(data)); err != nil {
				return ErrorResult(err.Error()), nil
			}
			if err := tx.stageWrite(toAbs, toRel, string(data), ""); err != nil {
				return ErrorResult(err.Error()), nil
			}
			if err := tx.stageDelete(fromAbs, fromRel, hashString(string(data))); err != nil {
				return ErrorResult(err.Error()), nil
			}
			continue
		}
		abs, rel, err := s.resolveEditTarget(target)
		if err != nil {
			return ErrorResult(err.Error()), nil
		}
		var original string
		data, readErr := os.ReadFile(abs)
		if readErr == nil {
			original = string(data)
		} else if !os.IsNotExist(readErr) {
			return ErrorResult("reading " + rel + ": " + readErr.Error()), nil
		}
		content := argString(m, "content")
		find := argString(m, "find")
		replace := argString(m, "replace")
		var out string
		if content != "" {
			out = content
		} else if find != "" {
			matches := strings.Count(original, find)
			if matches == 0 {
				return ErrorResult("find text not found in " + rel), nil
			}
			if matches > 1 && !argBool(m, "replaceAll", false) {
				return ErrorResult(fmt.Sprintf("%q matches %d places in %s; pass replaceAll: true or narrow the find text", find, matches, rel)), nil
			}
			out = strings.ReplaceAll(original, find, replace)
		} else {
			return ErrorResult(fmt.Sprintf("edits[%d] needs content or find+replace", i)), nil
		}
		if strings.Contains(original, "\r\n") && out != original {
			out = strings.ReplaceAll(out, "\n", "\r\n")
		}
		if err := validateGoSource(rel, out); err != nil {
			return ErrorResult(err.Error()), nil
		}
		if err := tx.stageWrite(abs, rel, out, expected); err != nil {
			return ErrorResult(err.Error()), nil
		}
	}
	return s.finishTransaction(tx, apply, "batch edit")
}

func (s *Service) finishTransaction(tx *fileTransaction, apply bool, label string) (ToolResult, *rpcError) {
	diff := tx.diff()
	if !apply {
		return TextResult("Dry run (apply: true to " + label + ").\n\n" + diff), nil
	}
	if err := tx.commit(); err != nil {
		return ErrorResult(err.Error()), nil
	}
	return TextResult("Applied " + label + " (" + fmt.Sprint(tx.count()) + " file(s)).\n\n" + diff), nil
}

func (s *Service) toolUndo(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	force := argBool(args, "force", false)
	var id string
	var err error
	if tid := argString(args, "id"); tid != "" {
		id, err = s.undoByID(tid, force)
	} else {
		id, err = s.undoLatest(force)
	}
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	return TextResult("Undid transaction " + id), nil
}

func (s *Service) toolFormatFile(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	target := argString(args, "path")
	if target == "" {
		target = argString(args, "route")
	}
	if target == "" {
		return ErrorResult("provide path"), nil
	}
	apply := argBool(args, "apply", false)
	abs, rel, err := s.resolveEditTarget(target)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	data, readErr := os.ReadFile(abs)
	if readErr != nil {
		return ErrorResult("reading " + rel + ": " + readErr.Error()), nil
	}
	if !isEditableSourceExt(rel) {
		return ErrorResult("no formatter for " + rel + " (supported: .tsx, .ts, .jsx, .js)"), nil
	}
	prog, perrs, dropped := astprint.Parse(string(data))
	if dropped > 0 {
		return ErrorResult("refusing to format " + rel + ": it contains TypeScript syntax the AST would drop"), nil
	}
	if len(perrs) > 0 {
		return ErrorResult(fmt.Sprintf("refusing to format %s: it does not parse", rel)), nil
	}
	out := astprint.Print(prog)
	if strings.Contains(string(data), "\r\n") {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	if out == string(data) {
		return TextResult(rel + " is already formatted"), nil
	}
	tx := newFileTransaction(s)
	if err := tx.stageWrite(abs, rel, out, ""); err != nil {
		return ErrorResult(err.Error()), nil
	}
	return s.finishTransaction(tx, apply, "format")
}
