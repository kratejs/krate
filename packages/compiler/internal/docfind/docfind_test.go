package docfind

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestBuildProducesSearchableWASM(t *testing.T) {
	docs := []Document{
		{Title: "Getting Started", Category: "docs", Href: "/docs/getting-started", Body: "This guide will help you get started with krate."},
		{Title: "Configuration", Category: "docs", Href: "/docs/configuration", Body: "Configure krate with krate.config.ts."},
		{Title: "Signals", Category: "runtime", Href: "/docs/runtime/signals", Body: "createSignal returns a getter and a setter pair."},
	}

	built, err := Build(context.Background(), docs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(built) == 0 {
		t.Fatal("Build returned empty wasm")
	}
	if len(built) < len(searchWasm) {
		t.Fatalf("built module smaller than template: got %d, template %d", len(built), len(searchWasm))
	}

	// Now instantiate the BUILT module and run docfind_search against it.
	results := searchWASM(t, built, "config")
	found := false
	for _, r := range results {
		if r["href"] == "/docs/configuration" {
			found = true
		}
	}
	if !found {
		t.Fatalf("'config' did not match /docs/configuration: %s", mustJSON(results))
	}

	results = searchWASM(t, built, "signal")
	found = false
	for _, r := range results {
		if r["href"] == "/docs/runtime/signals" {
			found = true
		}
	}
	if !found {
		t.Fatalf("'signal' did not match /docs/runtime/signals: %s", mustJSON(results))
	}

	results = searchWASM(t, built, "zzz-not-in-index")
	if len(results) != 0 {
		t.Fatalf("expected no results for gibberish, got %s", mustJSON(results))
	}
}

func TestBuildEmptyDocuments(t *testing.T) {
	_, err := Build(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty documents")
	}
}

func TestValidateDocuments(t *testing.T) {
	if err := ValidateDocuments([]Document{{Title: "a", Href: "/a", Body: "b"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateDocuments([]Document{{Title: "", Href: "", Body: ""}}); err == nil {
		t.Fatal("expected error for empty document")
	}
}

func searchWASM(t *testing.T, module []byte, query string) []map[string]string {
	t.Helper()
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(512))
	defer r.Close(ctx)
	compiled, err := r.CompileModule(ctx, module)
	if err != nil {
		t.Fatalf("compiling built module: %v", err)
	}
	mod, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("docfind-search"))
	if err != nil {
		t.Fatalf("instantiating built module: %v", err)
	}

	alloc := mod.ExportedFunction("docfind_alloc")
	search := mod.ExportedFunction("docfind_search")
	mem := mod.Memory()

	// Write query into wasm memory.
	qBytes := []byte(query)
	ptr, err := alloc.Call(ctx, uint64(len(qBytes)))
	if err != nil {
		t.Fatalf("alloc: %v", err)
	}
	if !mem.Write(uint32(ptr[0]), qBytes) {
		t.Fatal("write query out of bounds")
	}

	outPtr, err := alloc.Call(ctx, 8)
	if err != nil {
		t.Fatalf("alloc out: %v", err)
	}
	outBase := uint32(outPtr[0])

	res, err := search.Call(ctx, ptr[0], uint64(len(qBytes)), 10, uint64(outBase), uint64(outBase+4))
	if err != nil {
		t.Fatalf("search call: %v", err)
	}
	if len(res) == 0 || res[0] != 0 {
		t.Fatalf("search returned nonzero result: %v", res)
	}

	resPtr, ok := mem.ReadUint32Le(outBase)
	if !ok {
		t.Fatal("read result ptr")
	}
	resLen, ok := mem.ReadUint32Le(outBase + 4)
	if !ok {
		t.Fatal("read result len")
	}
	if resPtr == 0 || resLen == 0 {
		return nil
	}
	payload, ok := mem.Read(resPtr, resLen)
	if !ok {
		t.Fatal("read result payload")
	}
	var out []map[string]string
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	return out
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestEmbeddedAssetsPresent(t *testing.T) {
	if len(DocfindJS) == 0 {
		t.Fatal("docfind.js glue is empty")
	}
	if !strings.Contains(string(DocfindJS), "docfind_search") {
		t.Fatal("docfind.js glue does not reference docfind_search")
	}
	if len(searchWasm) == 0 || len(builderWasm) == 0 {
		t.Fatal("embedded wasm modules missing")
	}
}
