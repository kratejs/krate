package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/check"
	"github.com/kratejs/krate/packages/compiler/internal/fix"
)

const (
	defaultSearchLimit = 100
	maxSearchLimit     = 500
	maxSearchFileBytes = 2 * 1024 * 1024
)

type projectMatch struct {
	Path         string   `json:"path"`
	Root         string   `json:"root,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	ImportedFrom string   `json:"importedFrom,omitempty"`
	Line         int      `json:"line"`
	Col          int      `json:"col"`
	Text         string   `json:"text"`
	Context      []string `json:"context,omitempty"`
}

func (s *Service) toolSearchProject(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	reportProgress(ctx, 0, 1, "searching project")
	query, rerr := requireString(args, "query")
	if rerr != nil {
		return ToolResult{}, rerr
	}
	offset, err := decodeCursor(argString(args, "cursor"))
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	hits, truncated, err := s.searchProject(query, searchOptions{
		Regex:         argBool(args, "regex", false),
		CaseSensitive: argBool(args, "caseSensitive", false),
		Glob:          argString(args, "glob"),
		Root:          argString(args, "root"),
		Context:       argInt(args, "context", 2),
		Limit:         argInt(args, "limit", defaultSearchLimit),
		Skip:          offset,
	})
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	reportProgress(ctx, 1, 1, "search complete")
	next := ""
	if truncated {
		next = encodeCursor(offset + len(hits))
	}
	return JSONResult(map[string]any{
		"query":      query,
		"matches":    hits,
		"count":      len(hits),
		"truncated":  truncated,
		"nextCursor": next,
	})
}

func (s *Service) toolFindReferences(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	symbol, rerr := requireString(args, "symbol")
	if rerr != nil {
		return ToolResult{}, rerr
	}
	root := argString(args, "root")
	glob := argString(args, "glob")
	includeDefinition := argBool(args, "includeDefinition", true)
	limit := argInt(args, "limit", defaultSearchLimit)
	contextLines := argInt(args, "context", 1)
	s.refreshSymbolIndex()
	occurrences, indexStats := s.index.snapshot()
	var hits []projectMatch
	truncated := false
	for _, occ := range occurrences {
		if occ.Name != symbol {
			continue
		}
		if root != "" && occ.Root != root {
			continue
		}
		if glob != "" && !globMatch(glob, occ.Path) {
			continue
		}
		if !includeDefinition && occ.Kind == "definition" {
			continue
		}
		if len(hits) >= limit {
			truncated = true
			break
		}
		match := s.symbolMatch(occ, contextLines)
		hits = append(hits, match)
	}
	definitions := make([]projectMatch, 0)
	for _, occ := range occurrences {
		if occ.Kind != "definition" || occ.Name != symbol {
			continue
		}
		if root != "" && occ.Root != root {
			continue
		}
		if glob != "" && !globMatch(glob, occ.Path) {
			continue
		}
		definitions = append(definitions, s.symbolMatch(occ, 0))
	}
	return JSONResult(map[string]any{
		"symbol":      symbol,
		"references":  hits,
		"definitions": definitions,
		"count":       len(hits),
		"truncated":   truncated,
		"index":       indexStats,
	})
}

func (s *Service) refreshSymbolIndex() symbolIndexStats {
	return s.index.refresh(s.workspaceRoots())
}

func (s *Service) symbolMatch(occ symbolOccurrence, contextLines int) projectMatch {
	match := projectMatch{
		Path: indexPathLabel(occ.Root, occ.Path), Root: occ.Root, Kind: occ.Kind,
		ImportedFrom: occ.Source, Line: occ.Line, Col: occ.Col,
	}
	abs := filepath.Join(s.workspacePath(occ.Root), filepath.FromSlash(occ.Path))
	data, err := os.ReadFile(abs)
	if err != nil {
		return match
	}
	lines := splitSearchLines(string(data))
	if occ.Line > 0 && occ.Line <= len(lines) {
		match.Text = lines[occ.Line-1]
		start := occ.Line - 1 - contextLines
		if start < 0 {
			start = 0
		}
		end := occ.Line + contextLines
		if end > len(lines) {
			end = len(lines)
		}
		match.Context = append([]string(nil), lines[start:end]...)
	}
	return match
}

func (s *Service) workspacePath(root string) string {
	if wr, ok := s.findWorkspaceRoot(root); ok {
		return wr.Path
	}
	return s.root
}

type searchOptions struct {
	Regex         bool
	CaseSensitive bool
	Glob          string
	Root          string
	Context       int
	Limit         int
	Skip          int
}

func (s *Service) searchProject(query string, opts searchOptions) ([]projectMatch, bool, error) {
	if opts.Limit <= 0 {
		opts.Limit = defaultSearchLimit
	}
	if opts.Limit > maxSearchLimit {
		opts.Limit = maxSearchLimit
	}
	if opts.Skip < 0 {
		opts.Skip = 0
	}
	if opts.Context < 0 {
		opts.Context = 0
	}
	if opts.Context > 10 {
		opts.Context = 10
	}
	pattern := query
	if !opts.Regex {
		pattern = regexp.QuoteMeta(query)
	}
	if !opts.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false, fmt.Errorf("invalid search pattern: %w", err)
	}

	var matches []projectMatch
	truncated := false
	roots := s.workspaceRoots()
	for _, root := range roots {
		if opts.Root != "" && root.Name != opts.Root {
			continue
		}
		if truncated {
			break
		}
		walkErr := filepath.WalkDir(root.Path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != root.Path && listIgnoreDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(root.Path, p)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if s.policy != nil && s.policy.denied(rel) {
				return nil
			}
			if opts.Glob != "" && !globMatch(opts.Glob, rel) {
				return nil
			}
			info, statErr := d.Info()
			if statErr != nil || info.Size() > maxSearchFileBytes {
				return nil
			}
			data, readErr := os.ReadFile(p)
			if readErr != nil || !utf8.Valid(data) || isBinary(data) {
				return nil
			}
			lines := splitSearchLines(string(data))
			for i, line := range lines {
				for _, loc := range re.FindAllStringIndex(line, -1) {
					if len(matches) >= opts.Skip+opts.Limit {
						truncated = true
						return filepath.SkipAll
					}
					start := i - opts.Context
					if start < 0 {
						start = 0
					}
					end := i + opts.Context + 1
					if end > len(lines) {
						end = len(lines)
					}
					ctxLines := append([]string(nil), lines[start:end]...)
					matches = append(matches, projectMatch{
						Path: indexPathLabel(root.Name, rel), Root: root.Name,
						Line: i + 1, Col: loc[0] + 1, Text: line, Context: ctxLines,
					})
				}
			}
			return nil
		})
		if walkErr != nil {
			return nil, false, walkErr
		}
	}
	if opts.Skip >= len(matches) {
		return []projectMatch{}, truncated, nil
	}
	end := opts.Skip + opts.Limit
	if end > len(matches) {
		end = len(matches)
	}
	return matches[opts.Skip:end], truncated, nil
}

func splitSearchLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (s *Service) toolReadFiles(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	var paths []string
	if raw, ok := args["paths"].([]any); ok {
		for _, v := range raw {
			if p, ok := v.(string); ok && p != "" {
				paths = append(paths, p)
			}
		}
	}
	if p := argString(args, "path"); p != "" {
		paths = append(paths, p)
	}
	if rootName := argString(args, "root"); rootName != "" {
		for i, p := range paths {
			if !strings.Contains(p, ":") {
				paths[i] = rootName + ":" + p
			}
		}
	}
	if len(paths) == 0 {
		return ErrorResult("provide path or paths"), nil
	}
	if len(paths) > 100 {
		paths = paths[:100]
	}
	type fileView struct {
		Path      string `json:"path"`
		Root      string `json:"root,omitempty"`
		Language  string `json:"language,omitempty"`
		Bytes     int    `json:"bytes,omitempty"`
		Truncated bool   `json:"truncated,omitempty"`
		Content   string `json:"content,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	views := make([]fileView, 0, len(paths))
	const perFile = 256 * 1024
	for _, target := range paths {
		abs, rel, rootName, err := s.resolveWorkspacePath(target)
		if err != nil {
			views = append(views, fileView{Path: target, Error: err.Error()})
			continue
		}
		label := indexPathLabel(rootName, rel)
		if s.policy != nil && s.policy.denied(rel) {
			views = append(views, fileView{Path: label, Root: rootName, Error: "denied by policy"})
			continue
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			views = append(views, fileView{Path: label, Root: rootName, Error: readErr.Error()})
			continue
		}
		if isBinary(data) {
			views = append(views, fileView{Path: label, Root: rootName, Bytes: len(data), Error: "binary file"})
			continue
		}
		content := string(data)
		truncated := false
		if len(content) > perFile {
			content = content[:perFile]
			truncated = true
		}
		views = append(views, fileView{
			Path: label, Root: rootName, Language: languageForPath(rel), Bytes: len(data), Truncated: truncated, Content: content,
		})
	}
	return JSONResult(map[string]any{"files": views, "count": len(views)})
}

type diagnosticView struct {
	ID       string       `json:"id"`
	Code     string       `json:"code"`
	Severity string       `json:"severity"`
	File     string       `json:"file,omitempty"`
	Line     int          `json:"line,omitempty"`
	Col      int          `json:"col,omitempty"`
	Route    string       `json:"route,omitempty"`
	Message  string       `json:"message"`
	Hint     string       `json:"hint,omitempty"`
	Fix      *codemodView `json:"fix,omitempty"`
}

type codemodView struct {
	Code string `json:"code"`
	Tool string `json:"tool"`
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

func (s *Service) toolGetDiagnostics(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	reportProgress(ctx, 0, 1, "building")
	out, buildErr := s.runBuild(ctx)
	reportProgress(ctx, 0.7, 1, "collecting diagnostics")
	views := []diagnosticView{}
	seen := map[string]bool{}
	add := func(v diagnosticView) {
		key := v.Code + "\x00" + v.File + "\x00" + fmt.Sprint(v.Line) + ":" + fmt.Sprint(v.Col) + "\x00" + v.Message
		if seen[key] {
			return
		}
		seen[key] = true
		views = append(views, v)
	}
	for _, d := range build.DiagnosticsFromError(buildErr) {
		file := d.File
		if rel, err := filepath.Rel(s.root, file); err == nil {
			file = filepath.ToSlash(rel)
		}
		code := string(d.Code)
		if code == "" {
			code = "compile"
		}
		add(diagnosticView{
			ID:   code + ":" + file + ":" + fmt.Sprint(d.Line) + ":" + fmt.Sprint(d.Col),
			Code: code, Severity: "error", File: file, Line: d.Line, Col: d.Col, Message: d.Message, Hint: d.Hint,
		})
	}

	checkEnabled := argBool(args, "check", true)
	routeFilter := argString(args, "route")
	if checkEnabled && buildErr == nil {
		var findings []check.Finding
		var cfg check.Config
		chk := newBuilder(Options{Root: s.root, Cfg: s.cfg, Env: s.env, Verbose: s.verbose})
		chk.SetContext(ctx)
		s.runMu.Lock()
		_, err := captureStdout(func() error {
			var e error
			findings, cfg, e = chk.CheckSite(true)
			return e
		})
		s.runMu.Unlock()
		if err != nil {
			add(diagnosticView{ID: "check:error", Code: "check", Severity: "error", Message: "running checks: " + err.Error()})
		}
		_ = cfg
		for _, f := range findings {
			if routeFilter != "" && f.Route != routeFilter {
				continue
			}
			file := filepath.ToSlash(f.File)
			add(diagnosticView{
				ID:   f.Rule + ":" + file + ":" + fmt.Sprint(f.Line) + ":" + fmt.Sprint(f.Col),
				Code: f.Rule, Severity: f.Severity.String(), File: file, Line: f.Line, Col: f.Col, Route: f.Route,
				Message: f.Message, Hint: f.Hint,
			})
		}
	}
	codemods := []codemodView{}
	if _, changes, _, fixErr := fix.Plan(s.root); fixErr == nil {
		byKey := map[string]codemodView{}
		for _, change := range changes {
			codemod := codemodView{
				Code: string(change.Code), Tool: "apply_safe_fixes",
				File: change.File, Line: change.Line, Col: change.Col,
				From: change.From, To: change.To,
			}
			codemods = append(codemods, codemod)
			byKey[codemodKey(codemod.Code, codemod.File, codemod.Line, codemod.Col)] = codemod
		}
		for i := range views {
			key := codemodKey(views[i].Code, views[i].File, views[i].Line, views[i].Col)
			if codemod, ok := byKey[key]; ok {
				views[i].Fix = &codemod
			}
		}
	}
	reportProgress(ctx, 1, 1, "diagnostics complete")
	return JSONResult(map[string]any{
		"ok":          buildErr == nil,
		"diagnostics": views,
		"count":       len(views),
		"codemods":    codemods,
		"output":      strings.TrimSpace(out),
	})
}

func codemodKey(code, file string, line, col int) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d", code, normalizeRel(file), line, col)
}

func (s *Service) toolApplySafeFixes(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	codes := argStringList(args, "codes")
	pathFilters := argStringList(args, "paths")
	files, changes, skipped, err := fix.PlanWithCodes(s.root, codes)
	if err != nil {
		return ErrorResult("planning safe fixes: " + err.Error()), nil
	}
	apply := argBool(args, "apply", false)
	tx := newFileTransaction(s)
	rels := make([]string, 0, len(files))
	for rel := range files {
		if len(pathFilters) > 0 && !matchesPathFilters(pathFilters, rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	selected := map[string]bool{}
	for _, rel := range rels {
		selected[rel] = true
		if s.policy != nil && !s.policy.writable(rel) {
			skipped = append(skipped, fix.Skipped{File: rel, Reason: s.policy.deniedReason(rel)})
			continue
		}
		abs, _, terr := s.resolveProjectPath(rel)
		if terr != nil {
			return ErrorResult(terr.Error()), nil
		}
		current, rerr := os.ReadFile(abs)
		if rerr != nil {
			return ErrorResult("reading " + rel + ": " + rerr.Error()), nil
		}
		if err := tx.stageWrite(abs, rel, files[rel], hashString(string(current))); err != nil {
			return ErrorResult(err.Error()), nil
		}
	}
	appliedChanges := make([]fix.Change, 0, len(changes))
	for _, change := range changes {
		if selected[change.File] {
			appliedChanges = append(appliedChanges, change)
		}
	}
	if tx.count() == 0 {
		if len(appliedChanges) == 0 {
			return TextResult("No safe fixes available."), nil
		}
		var b strings.Builder
		b.WriteString("No writable safe fixes available.")
		for _, sk := range skipped {
			fmt.Fprintf(&b, "\n  %s: %s", sk.File, sk.Reason)
		}
		return TextResult(b.String()), nil
	}
	res, rerr := s.finishTransaction(tx, apply, "safe fixes")
	if rerr != nil {
		return res, rerr
	}
	var b strings.Builder
	if len(appliedChanges) > 0 {
		b.WriteString("\nCodemods:\n")
		for _, change := range appliedChanges {
			fmt.Fprintf(&b, "  %s %s:%d:%d %q -> %q\n", change.Code, change.File, change.Line, change.Col, change.From, change.To)
		}
	}
	if len(skipped) > 0 {
		b.WriteString("\nSkipped files:\n")
		for _, sk := range skipped {
			fmt.Fprintf(&b, "  %s: %s\n", sk.File, sk.Reason)
		}
	}
	if b.Len() > 0 {
		res.Content = append(res.Content, TextContent(b.String()))
	}
	return res, nil
}

func matchesPathFilters(patterns []string, rel string) bool {
	rel = normalizeRel(rel)
	for _, pattern := range patterns {
		pattern = normalizeRel(pattern)
		if pattern == "" {
			continue
		}
		if globMatch(pattern, rel) {
			return true
		}
	}
	return false
}

func (s *Service) toolApplyFix(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	target := argString(args, "path")
	if target == "" {
		target = argString(args, "route")
	}
	find := argString(args, "find")
	replace := argString(args, "replace")
	if target == "" || find == "" {
		return ErrorResult("provide path/route and find/replace for the fix"), nil
	}
	res, rerr := s.toolEditFile(ctx, map[string]any{
		"route": target, "find": find, "replace": replace,
		"expectedSha256": argString(args, "expectedSha256"),
		"apply":          argBool(args, "apply", false),
	})
	if rerr != nil {
		return res, rerr
	}
	return res, nil
}

func (s *Service) toolImpact(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	target := argString(args, "path")
	route := argString(args, "route")
	if target == "" && route == "" {
		return ErrorResult("provide path or route"), nil
	}
	graph, out, rerr := s.assembleSiteGraph(ctx)
	if rerr != nil {
		return ErrorResult(rerr.Message + "\n\n" + out), nil
	}
	normalized := normalizeRel(target)
	type result struct {
		Path         string   `json:"path,omitempty"`
		Route        string   `json:"route,omitempty"`
		Dependents   []string `json:"dependents,omitempty"`
		Dependencies []string `json:"dependencies,omitempty"`
	}
	var res result
	if route != "" {
		want := normalizeRel(strings.TrimPrefix(route, "/"))
		for _, r := range graph.Routes {
			if r.Route == route || strings.TrimPrefix(r.Route, "/") == want {
				res.Route = r.Route
				res.Dependencies = r.Dependencies
				res.Dependents = r.Dependents
				break
			}
		}
		if res.Route == "" {
			return ErrorResult("route not found: " + route), nil
		}
	} else {
		res.Path = normalized
		res.Dependents = graph.Files[normalized]
	}
	return JSONResult(res)
}

func (s *Service) toolListChanges(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	offset, err := decodeCursor(argString(args, "cursor"))
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	limit := argInt(args, "limit", 10)
	if limit <= 0 {
		limit = 10
	}
	items, err := s.transactionHistory(offset + limit + 1)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	total := len(items)
	next := ""
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := items[offset:end]
	if end < total {
		next = encodeCursor(end)
	}
	return JSONResult(map[string]any{"transactions": page, "count": len(page), "nextCursor": next})
}

func (s *Service) transactionHistory(limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	entries, err := os.ReadDir(s.transactionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]any{}, nil
		}
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	out := make([]map[string]any, 0, limit)
	for _, name := range dirs {
		if len(out) >= limit {
			break
		}
		data, err := os.ReadFile(filepath.Join(s.transactionsDir(), name, "transaction.json"))
		if err != nil {
			continue
		}
		var meta txMeta
		if json.Unmarshal(data, &meta) != nil {
			continue
		}
		files := make([]string, 0, len(meta.Files))
		for _, f := range meta.Files {
			files = append(files, f.Path)
		}
		out = append(out, map[string]any{
			"id": meta.ID, "created": meta.Created, "undone": meta.Undone, "files": files,
		})
	}
	return out, nil
}
