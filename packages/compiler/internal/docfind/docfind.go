// Package docfind embeds Microsoft's docfind WASM document-search engine into
// krate and drives it entirely in-process - no subprocess and no temporary
// JSON files on disk.
// Two WASM modules are embedded:
//   - search.wasm - the browser-facing search module. An index is embedded into
//     it at docs-build time; the result is written to the output directory as
//     `docfind_bg.wasm` next to the hand-written `docfind.js` glue.
//   - builder.wasm - the build-time module that builds a search index from a
//     JSON document array and embeds it into `search.wasm` (passed as a
//     template), producing the final module.
//
// The WASM modules are compiled from the vendored Rust sources under
// `third_party/docfind` (see `scripts/build-docfind.mjs`). Everything runs via
// github.com/tetratelabs/wazero, a pure-Go WebAssembly runtime.
package docfind

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed embedded/docfind.js
var DocfindJS []byte

//go:embed embedded/search.wasm
var searchWasm []byte

//go:embed embedded/builder.wasm
var builderWasm []byte

// Document is one entry in the search index. The JSON field names match
// docfind's `Document` schema (serde camelCase).
type Document struct {
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Href     string   `json:"href"`
	Body     string   `json:"body"`
	Keywords []string `json:"keywords,omitempty"`
}

// DocfindJSName is the filename (with directory) that DocfindJS should be
// written to relative to the output root.
const DocfindJSName = "docfind.js"

// WASMName is the filename of the search module produced by Build.
const WASMName = "docfind_bg.wasm"

var (
	mu sync.Mutex
)

// Build returns the final `docfind_bg.wasm` module with an index built from
// `documents` embedded into it. It runs the vendored docfind builder in-process
// via wazero; documents are passed through WASM memory (no temp files). The
// returned bytes are ready to serve to the browser alongside DocfindJS.
func Build(ctx context.Context, documents []Document) ([]byte, error) {
	if len(documents) == 0 {
		return nil, errors.New("docfind: no documents to index")
	}

	docsJSON, err := json.Marshal(documents)
	if err != nil {
		return nil, fmt.Errorf("docfind: encoding documents: %w", err)
	}

	mu.Lock()
	defer mu.Unlock()

	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(512))
	defer r.Close(ctx)

	mod, err := r.InstantiateWithConfig(ctx, builderWasm, wazero.NewModuleConfig().WithName("docfind-builder"))
	if err != nil {
		return nil, fmt.Errorf("docfind: instantiating builder module: %w", err)
	}

	alloc := mod.ExportedFunction("docfind_alloc")
	build := mod.ExportedFunction("docfind_build")
	mem := mod.Memory()
	if mem == nil {
		return nil, errors.New("docfind: builder module has no memory")
	}

	writeInput := func(data []byte) (uint64, uint64, error) {
		if len(data) == 0 {
			return 0, 0, nil
		}
		ptr, err := alloc.Call(ctx, uint64(len(data)))
		if err != nil {
			return 0, 0, err
		}
		if !mem.Write(uint32(ptr[0]), data) {
			return 0, 0, errors.New("docfind: writing input out of bounds")
		}
		return ptr[0], uint64(len(data)), nil
	}

	docsPtr, docsLen, err := writeInput(docsJSON)
	if err != nil {
		return nil, fmt.Errorf("docfind: writing documents into WASM memory: %w", err)
	}
	tplPtr, tplLen, err := writeInput(searchWasm)
	if err != nil {
		return nil, fmt.Errorf("docfind: writing template into WASM memory: %w", err)
	}

	// 8 bytes: [out_ptr u32][out_len u32]
	outPtr, err := alloc.Call(ctx, 8)
	if err != nil {
		return nil, fmt.Errorf("docfind: allocating output buffer: %w", err)
	}
	outBase := uint32(outPtr[0])

	results, err := build.Call(ctx, docsPtr, docsLen, tplPtr, tplLen, uint64(outBase), uint64(outBase+4))
	if err != nil {
		return nil, fmt.Errorf("docfind: building index: %w", err)
	}
	if len(results) == 0 || results[0] != 0 {
		return nil, errors.New("docfind: building index failed (invalid documents or template)")
	}

	resPtr, ok := mem.ReadUint32Le(outBase)
	if !ok {
		return nil, errors.New("docfind: reading output pointer")
	}
	resLen, ok := mem.ReadUint32Le(outBase + 4)
	if !ok {
		return nil, errors.New("docfind: reading output length")
	}
	if resPtr == 0 || resLen == 0 {
		return nil, errors.New("docfind: builder returned an empty module")
	}

	wasm, ok := mem.Read(resPtr, resLen)
	if !ok {
		return nil, errors.New("docfind: reading built module")
	}
	out := make([]byte, len(wasm))
	copy(out, wasm)

	return out, nil
}

// SearchResult is one ranked hit returned by Search.
type SearchResult struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	Href     string `json:"href"`
	Body     string `json:"body"`
}

// searchState caches a built index and a live search module. The framework
// docs are static for the lifetime of a process, so the index is built once
// on first use.
type searchState struct {
	once    sync.Once
	runtime wazero.Runtime
	mod     api.Module
	err     error
}

var search searchState

// Search runs a ranked query against an index built from `documents`. The
// index is built lazily on the first call and cached for the process; later
// calls reuse it and ignore `documents`. Queries are serialized because a
// single WASM instance is shared.
func Search(ctx context.Context, documents []Document, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 8
	}
	search.once.Do(func() {
		search.runtime, search.mod, search.err = buildSearchModule(ctx, documents)
	})
	if search.err != nil {
		return nil, search.err
	}

	mu.Lock()
	defer mu.Unlock()

	alloc := search.mod.ExportedFunction("docfind_alloc")
	free := search.mod.ExportedFunction("docfind_free")
	doSearch := search.mod.ExportedFunction("docfind_search")
	mem := search.mod.Memory()
	if alloc == nil || doSearch == nil || mem == nil {
		return nil, errors.New("docfind: search module missing exports")
	}

	qBytes := []byte(query)
	qPtr, err := alloc.Call(ctx, uint64(len(qBytes)))
	if err != nil {
		return nil, fmt.Errorf("docfind: allocating query: %w", err)
	}
	qPtrVal := uint32(qPtr[0])

	// A zero-length query has nothing to write; keep the pointer valid.
	if len(qBytes) > 0 {
		if !mem.Write(qPtrVal, qBytes) {
			return nil, errors.New("docfind: writing query out of bounds")
		}
	}
	if free != nil {
		defer func() { _, _ = free.Call(ctx, qPtr[0], uint64(len(qBytes))) }()
	}

	outPtr, err := alloc.Call(ctx, 8)
	if err != nil {
		return nil, fmt.Errorf("docfind: allocating output buffer: %w", err)
	}
	outBase := uint32(outPtr[0])

	res, err := doSearch.Call(ctx, qPtr[0], uint64(len(qBytes)), uint64(maxResults), uint64(outBase), uint64(outBase+4))
	if err != nil {
		return nil, fmt.Errorf("docfind: search call: %w", err)
	}
	if len(res) == 0 || res[0] != 0 {
		return nil, errors.New("docfind: search failed")
	}

	resPtr, ok := mem.ReadUint32Le(outBase)
	if !ok {
		return nil, errors.New("docfind: reading result pointer")
	}
	resLen, ok := mem.ReadUint32Le(outBase + 4)
	if !ok {
		return nil, errors.New("docfind: reading result length")
	}
	if resPtr == 0 || resLen == 0 {
		return []SearchResult{}, nil
	}
	payload, ok := mem.Read(resPtr, resLen)
	if !ok {
		return nil, errors.New("docfind: reading result payload")
	}
	var out []SearchResult
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("docfind: decoding results: %w", err)
	}
	return out, nil
}

// buildSearchModule builds an index from documents and returns a live module
// ready to answer queries.
func buildSearchModule(ctx context.Context, documents []Document) (wazero.Runtime, api.Module, error) {
	built, err := Build(ctx, documents)
	if err != nil {
		return nil, nil, err
	}
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(512))
	compiled, err := r.CompileModule(ctx, built)
	if err != nil {
		r.Close(ctx)
		return nil, nil, fmt.Errorf("docfind: compiling search module: %w", err)
	}
	mod, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("docfind-search"))
	if err != nil {
		r.Close(ctx)
		return nil, nil, fmt.Errorf("docfind: instantiating search module: %w", err)
	}
	return r, mod, nil
}

// ValidateDocuments returns an error if any document is unusable (empty href or
// title/body), with the offending hrefs listed.
func ValidateDocuments(documents []Document) error {
	var bad []string
	for _, d := range documents {
		if strings.TrimSpace(d.Href) == "" {
			bad = append(bad, "(empty href)")
			continue
		}
		if strings.TrimSpace(d.Title) == "" && strings.TrimSpace(d.Body) == "" {
			bad = append(bad, d.Href)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("docfind: %d document(s) have an empty href or no content: %s",
			len(bad), strings.Join(bad, ", "))
	}
	return nil
}
