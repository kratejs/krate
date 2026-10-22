package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const boundaryClassSrc = `
import React, { Component } from 'react';
class ErrorBoundary extends Component {
  state = { error: null };
  static getDerivedStateFromError(error) { return { error: error.message }; }
  componentDidCatch(error, info) { console.log(error.message); }
  render() {
    if (this.state.error) return <p class="fallback">Failed: {this.state.error}</p>;
    return this.props.children;
  }
}
function Broken(props) {
  if (!props.data) throw new Error('no data for ' + props.label);
  return <p class="ok">{props.data}</p>;
}
`

// TestBoundaryBuildTimeFallback: a descendant that throws while the page is
// built makes its boundary render the getDerivedStateFromError fallback, and
// the runtime receives the error for componentDidCatch. A sibling boundary
// whose child renders fine is unaffected.
func TestBoundaryBuildTimeFallback(t *testing.T) {
	html, js := buildReactPage(t, boundaryClassSrc+`
export default function Page() {
  return <main>
    <ErrorBoundary><Broken label="x" /></ErrorBoundary>
    <ErrorBoundary><Broken label="y" data="fine" /></ErrorBoundary>
  </main>;
}`)
	for _, want := range []string{`<p class="fallback">Failed: <!--k:4-->no data for x<!--/k:4--></p>`, `<p class="ok">fine</p>`} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "__children__") {
		t.Errorf("children placeholder leaked:\n%s", html)
	}
	if !strings.Contains(js, "buildError:{name:'Error',message:'no data for x'}") {
		t.Errorf("hydration should pass the build error to the runtime:\n%s", js)
	}
	if strings.Count(js, "buildError:") != 1 {
		t.Errorf("only the failing boundary carries a build error:\n%s", js)
	}
}

// TestBuildTimeThrowWithoutBoundaryFails: with no boundary the build fails
// with the coded KR3010 diagnostic at the throw.
func TestBuildTimeThrowWithoutBoundaryFails(t *testing.T) {
	root := t.TempDir()
	pages := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pages, 0755); err != nil {
		t.Fatal(err)
	}
	src := boundaryClassSrc + `
export default function Page() { return <main><Broken label="z" /></main>; }`
	if err := os.WriteFile(filepath.Join(pages, "index.tsx"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = pages
	cfg.OutDir = filepath.Join(root, "dist")
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("expected the build to fail")
	}
	if msg := err.Error(); !strings.Contains(msg, "[KR3010]") || !strings.Contains(msg, "no data for z") || !strings.Contains(msg, "13:20: [KR3010]") {
		t.Errorf("error = %s", msg)
	}
}
