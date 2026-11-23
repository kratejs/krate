package build

import (
	"fmt"
	"strings"
	"testing"
)

// builtinDriverFiles is a client component that reaches a database driver
// through the root of a package: editor -> markdown -> @scope/community ->
// fakedb, whose many files import Node built-ins.
func builtinDriverFiles(driverFiles int) map[string]string {
	files := map[string]string{
		"src/pages/index.tsx": `import Editor from "../components/editor";
export default async function Home() { return <main><Editor /></main>; }
`,
		"src/components/editor.tsx": `"use client";
import { useState } from "react";
import { render } from "./markdown";
export default function Editor() {
  const [t] = useState("x");
  return <div>{render(t)}</div>;
}
`,
		"src/components/markdown.tsx": `import { slug } from "@scope/community";
export function render(t: string) { return slug(t); }
`,
		"node_modules/@scope/community/package.json": `{"name":"@scope/community","version":"1.0.0","main":"index.js"}`,
		"node_modules/@scope/community/index.js": `import { MongoClient } from "fakedb";
export const client = new MongoClient();
export const slug = (s) => String(s).toLowerCase();
`,
		"node_modules/fakedb/package.json": `{"name":"fakedb","version":"1.0.0","main":"index.js"}`,
	}
	var imports, uses strings.Builder
	for i := 0; i < driverFiles; i++ {
		files[fmt.Sprintf("node_modules/fakedb/lib/part%d.js", i)] = fmt.Sprintf("import net from \"node:net\";\nexport const part%d = () => net.createConnection;\n", i)
		fmt.Fprintf(&imports, "import { part%d } from \"./lib/part%d.js\";\n", i, i)
		fmt.Fprintf(&uses, "part%d, ", i)
	}
	files["node_modules/fakedb/index.js"] = imports.String() + "export class MongoClient { constructor() { this.parts = [" + uses.String() + "]; } }\n"
	return files
}

// A client component that drags a Node-only package in reports one error for
// the island, not one per driver file, and names the import chain.
func TestClientChunkNodeBuiltinsReportOnceWithImportChain(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, builtinDriverFiles(40))
	withPreact(t, root)
	err := b.BuildAll()
	if err == nil {
		t.Fatal("a client chunk that imports Node built-ins must fail the build")
	}
	got := strings.ReplaceAll(err.Error(), `\`, "/")
	for _, want := range []string{
		"KR8009",
		`"node:net"`,
		"40 imports in 40 files",
		"import chain: src/components/editor.tsx -> src/components/markdown.tsx -> @scope/community (package root) -> fakedb",
		"browser-safe subpath",
		"src/components/markdown.tsx:1:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("error lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "Could not resolve"); n != 1 {
		t.Errorf("%d \"Could not resolve\" lines, want 1:\n%s", n, got)
	}
	if strings.Contains(got, "part7.js") {
		t.Errorf("a driver file is listed without verbose:\n%s", got)
	}
}

// With verbose on, the whole list follows the summary.
func TestClientChunkNodeBuiltinsVerboseListsEveryImport(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, builtinDriverFiles(12))
	withPreact(t, root)
	b.Verbose = true
	err := b.BuildAll()
	if err == nil {
		t.Fatal("expected a build error")
	}
	got := strings.ReplaceAll(err.Error(), `\`, "/")
	if !strings.Contains(got, "import chain:") {
		t.Errorf("the summary is missing:\n%s", got)
	}
	if n := strings.Count(got, "Could not resolve"); n != 13 {
		t.Errorf("%d \"Could not resolve\" lines, want 13 (summary and 12 imports):\n%s", n, got)
	}
}

// Project code that imports a built-in itself is named directly.
func TestClientChunkNodeBuiltinInProjectCode(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `import Editor from "../components/editor";
export default async function Home() { return <main><Editor /></main>; }
`,
		"src/components/editor.tsx": `"use client";
import { readFileSync } from "node:fs";
export default function Editor() { return <pre>{String(readFileSync)}</pre>; }
`,
	})
	withPreact(t, root)
	err := b.BuildAll()
	if err == nil {
		t.Fatal("expected a build error")
	}
	got := strings.ReplaceAll(err.Error(), `\`, "/")
	for _, want := range []string{"KR8009", `"node:fs"`, "import chain: src/components/editor.tsx", "src/components/editor.tsx:2:"} {
		if !strings.Contains(got, want) {
			t.Errorf("error lacks %q:\n%s", want, got)
		}
	}
}
