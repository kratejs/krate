package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A "use client" component on a server-rendered page renders on the server
// with getServerSnapshot, as React does: useSyncExternalStore(subscribe,
// () => "EyeDropper" in window, () => false) threw "window is not defined"
// because preact/compat ignores the third argument.
func TestClientBoundaryServerRenderUsesServerSnapshot(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	writeProjectFile(t, root, "src/components/Picker.tsx", `'use client';
import * as React from 'react';
import { useSyncExternalStore } from 'react';
const noop = () => () => {};
export default function Picker() {
  const pipette = React.useSyncExternalStore(noop, () => "EyeDropper" in window, () => false);
  const width = useSyncExternalStore(noop, () => window.innerWidth, () => -1);
  return <p id="picker">pipette:{String(pipette)} width:{width}</p>;
}
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import Picker from '../components/Picker';
export default async function Page() {
  const title = await Promise.resolve("picker");
  return <main><h1>{title}</h1><Picker /></main>;
}
`)
	t.Chdir(root)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getBody(t, base+"/")
	if status != 200 {
		t.Fatalf("status %d:\n%s", status, html)
	}
	if !strings.Contains(html, "pipette:false") || !strings.Contains(html, "width:-1") {
		t.Fatalf("the server render did not use getServerSnapshot:\n%s", html)
	}
}
