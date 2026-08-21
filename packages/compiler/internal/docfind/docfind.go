// Package docfind embeds Microsoft's docfind WASM document-search engine into
// krate and drives it entirely in-process — no subprocess and no temporary
// JSON files on disk.
//
// Two WASM modules are embedded:
//
//   - search.wasm — the browser-facing search module. An index is embedded into
//     it at docs-build time; the result is written to the output directory as
//     `docfind_bg.wasm` next to the hand-written `docfind.js` glue.
//   - builder.wasm — the build-time module that builds a search index from a
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

	once          sync.Once
	compiledBuild wazero.CompiledModule
	compileErr    error
)

func ensureCompiled(ctx context.Context, r wazero.Runtime) (wazero.CompiledModule, error) {
	once.Do(func() {
		compiledBuild, compileErr = r.CompileModule(ctx, builderWasm)
	})
	return compiledBuild, compileErr
}

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

	compiled, err := ensureCompiled(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("docfind: compiling builder module: %w", err)
	}
	mod, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("docfind-builder"))
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
