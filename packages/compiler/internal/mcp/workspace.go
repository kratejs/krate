package mcp

import (
	"cmp"
	"context"
	"sort"
	"strings"
)

type workspaceSymbolView struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Path     string `json:"path"` // root:path for secondary roots
	Root     string `json:"root,omitempty"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Exported bool   `json:"exported,omitempty"`
	Text     string `json:"text,omitempty"`
}

type workspaceRootView struct {
	Name    string `json:"name,omitempty"`
	Path    string `json:"path"`
	Primary bool   `json:"primary,omitempty"`
}

func (s *Service) toolWorkspaceSymbols(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	reportProgress(ctx, 0, 1, "indexing workspace")
	s.refreshSymbolIndex()
	occurrences, stats := s.index.snapshot()
	query := strings.TrimSpace(argString(args, "query"))
	kind := strings.ToLower(strings.TrimSpace(argString(args, "kind")))
	root := argString(args, "root")
	glob := argString(args, "glob")
	offset, err := decodeCursor(argString(args, "cursor"))
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	limit := argInt(args, "limit", 200)
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	type symbolEntry struct {
		view workspaceSymbolView
		occ  symbolOccurrence
	}
	var symbols []symbolEntry
	for _, occ := range occurrences {
		if occ.Kind != "definition" {
			continue
		}
		if kind != "" && !strings.EqualFold(occ.SymbolKind, kind) {
			continue
		}
		if root != "" && occ.Root != root {
			continue
		}
		if glob != "" && !globMatch(glob, occ.Path) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(occ.Name), strings.ToLower(query)) {
			continue
		}
		symbols = append(symbols, symbolEntry{
			view: workspaceSymbolView{
				Name: occ.Name, Kind: occ.SymbolKind, Path: indexPathLabel(occ.Root, occ.Path),
				Root: occ.Root, Line: occ.Line, Col: occ.Col, Exported: occ.Exported,
			},
			occ: occ,
		})
	}
	sort.SliceStable(symbols, func(i, j int) bool {
		a, b := symbols[i].view, symbols[j].view
		if query != "" {
			ra, rb := symbolMatchRank(a.Name, query), symbolMatchRank(b.Name, query)
			if ra != rb {
				return ra < rb
			}
		}
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c < 0
		}
		if c := cmp.Compare(a.Path, b.Path); c != 0 {
			return c < 0
		}
		return a.Line < b.Line
	})
	total := len(symbols)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := make([]workspaceSymbolView, 0, end-offset)
	for _, entry := range symbols[offset:end] {
		entry.view.Text = s.symbolMatch(entry.occ, 0).Text
		page = append(page, entry.view)
	}
	next := ""
	if end < total {
		next = encodeCursor(end)
	}
	roots := make([]workspaceRootView, 0, len(s.roots))
	for i, r := range s.roots {
		roots = append(roots, workspaceRootView{Name: r.Name, Path: r.Path, Primary: i == 0})
	}
	reportProgress(ctx, 1, 1, "workspace index ready")
	return JSONResult(map[string]any{
		"symbols":    page,
		"count":      len(page),
		"total":      total,
		"truncated":  end < total,
		"nextCursor": next,
		"roots":      roots,
		"index":      stats,
	})
}

func symbolMatchRank(name, query string) int {
	name = strings.ToLower(name)
	query = strings.ToLower(query)
	switch {
	case name == query:
		return 0
	case strings.HasPrefix(name, query):
		return 1
	default:
		return 2
	}
}
