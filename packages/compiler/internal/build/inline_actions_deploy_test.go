package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The key that seals inline-action values is a secret: a deploy bundle never
// contains it, and the plan tells the user to provide KRATE_ACTION_SECRET.
func TestServerBundleOmitsInlineActionKey(t *testing.T) {
	proj, cfg := serverlessProject(t, "serverless")
	page := `export default async function Inline({ params }: any) {
  const { id } = await params;
  async function rename() { 'use server'; return id; }
  return <form action={rename}><button>go</button></form>;
}
`
	if err := os.WriteFile(filepath.Join(proj, "src", "pages", "inline.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	b := New(proj, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(proj, ".krate", "action-key"))
	if err != nil || len(strings.TrimSpace(string(key))) < minActionSecretLen {
		t.Fatalf("local key missing: %v", err)
	}
	plan, err := BuildServerBundle(proj, cfg, "node")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(plan.Bundle)
	if strings.Contains(string(data), strings.TrimSpace(string(key))) {
		t.Error("the deploy bundle contains the inline action key")
	}
	warned := false
	for _, w := range plan.Warnings {
		warned = warned || strings.Contains(w, "KRATE_ACTION_SECRET")
	}
	if !warned {
		t.Errorf("want a KRATE_ACTION_SECRET warning, got %v", plan.Warnings)
	}
}
