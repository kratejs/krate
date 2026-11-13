package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInlineServerActionStaticPage covers the build-time path: an inline
// action that closes over nothing is an ordinary action reference, so a
// synchronous page renders its form with the action URL like an imported
// action. One that captures a value in a synchronous component, or any inline
// action in a "use client" module, is KR7001.
func TestInlineServerActionStaticPage(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/account.tsx": `export default function Account() {
  return <main>
    <form action={async () => { 'use server'; return { out: true }; }}><button>Sign out</button></form>
    <form action={signOutAll}><button>All</button></form>
  </main>;
  async function signOutAll() { 'use server'; return { all: true }; }
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	page, err := os.ReadFile(filepath.Join(root, "dist", "account", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(page), "action=/__krate/action/a"); n != 2 {
		t.Fatalf("want two native action forms, got %d:\n%s", n, page)
	}
	if b.actions == nil || len(b.actions.Entries) != 2 {
		t.Fatalf("manifest: %+v", b.actions)
	}

	for name, files := range map[string]map[string]string{
		"captures in a sync component": {"src/pages/p.tsx": `export default function P({ id }: { id: string }) {
  return <form action={async () => { 'use server'; return id; }} />;
}`},
		"use client": {"src/pages/p.tsx": `"use client";
export default function P() {
  return <form action={async () => { 'use server'; }} />;
}`},
	} {
		b, _ := scProject(t, files)
		err := b.BuildAll()
		if err == nil || !strings.Contains(err.Error(), "KR7001") {
			t.Errorf("%s: want KR7001, got %v", name, err)
		}
	}
}
