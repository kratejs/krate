package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestAssembleSiteGraph(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": `export default function Page() { return <h1>Hi</h1>; }`,
		"src/pages/about.tsx": `export default function About() { return <h1>About</h1>; }`,
	})

	graph, _, rerr := svc.assembleSiteGraph(context.Background())
	if rerr != nil {
		t.Fatalf("assembleSiteGraph: %v", rerr)
	}
	if len(graph.Routes) < 2 {
		t.Fatalf("expected at least 2 routes, got %d", len(graph.Routes))
	}

	var about *graphNode
	for i := range graph.Routes {
		if graph.Routes[i].Route == "/about" {
			about = &graph.Routes[i]
		}
	}
	if about == nil {
		t.Fatalf("no /about node in graph: %+v", graph.Routes)
	}
	if about.Source != "src/pages/about.tsx" {
		t.Errorf("source = %q", about.Source)
	}
	if !about.Built {
		t.Error("about node should be built")
	}
	foundDep := false
	for _, d := range about.Dependencies {
		if d == "src/pages/about.tsx" {
			foundDep = true
		}
	}
	if !foundDep {
		t.Errorf("dependencies missing page file: %v", about.Dependencies)
	}
	if len(about.Outputs) == 0 {
		t.Errorf("no output files recorded for /about")
	}
	if len(graph.Files["src/pages/about.tsx"]) == 0 {
		t.Errorf("files map missing dependents for the page file")
	}
}

func TestExplainTool(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": `export default function Page() { return <h1>Hi</h1>; }`,
	})
	res, rerr := svc.toolExplain(context.Background(), map[string]any{"route": "/"})
	if rerr != nil {
		t.Fatalf("toolExplain rpc error: %v", rerr)
	}
	text := ""
	for _, c := range res.Content {
		text += c.Text
	}
	if !strings.Contains(text, `"/"`) || !strings.Contains(text, "src/pages/index.tsx") {
		t.Fatalf("explain output unexpected: %s", text)
	}
}

func TestExplainToolMissingRoute(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": `export default function Page() { return <h1>Hi</h1>; }`,
	})
	res, rerr := svc.toolExplain(context.Background(), map[string]any{"route": "/nope"})
	if rerr != nil {
		t.Fatalf("unexpected rpc error: %v", rerr)
	}
	text := ""
	for _, c := range res.Content {
		text += c.Text
	}
	if !strings.Contains(text, "not found") {
		t.Fatalf("expected not-found message, got: %s", text)
	}
}
