package irtree_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func annotateAndBuildReact(t *testing.T, src string) *irtree.ComponentTree {
	t.Helper()
	prog := parseProg(t, src)
	bundler.RewriteReact(prog)
	ann := annotator.Annotate(prog, &config.Config{}, "test.tsx", src)
	return irtree.Build(prog, ann)
}

func TestStatelessRefComponentKeepsRefDeclAndMappedProps(t *testing.T) {
	src := `import { useRef } from "react";
const PREFIX = "/p/";
const ROWS = [
  { id: "one", label: "First" },
  { id: "two", label: "Second" },
  { id: "three", label: "Third" },
];

function TrackPanel({ className = "", children, ...rest }) {
  const panelRef = useRef<HTMLDivElement>(null);
  return (
    <div
      ref={panelRef}
      className={` + "`panel ${className}`" + `}
      onPointerMove={(e) => {
        const el = panelRef.current;
        if (!el) return;
        el.style.setProperty("--pos", ` + "`${e.clientX}px`" + `);
      }}
      {...rest}
    >
      {children}
    </div>
  );
}

function Row({ entry }) {
  const target = PREFIX + entry.id;
  return (
    <li>
      <TrackPanel className="row">
        <a href={target}>{entry.label}</a>
      </TrackPanel>
    </li>
  );
}

export default function Page() {
  return <ul>{ROWS.map((entry) => <Row entry={entry} />)}</ul>;
}`

	tree := annotateAndBuildReact(t, src)

	var panels []*irtree.ComponentNode
	var walk func([]irtree.SlotNode)
	walk = func(nodes []irtree.SlotNode) {
		for _, n := range nodes {
			cs, ok := n.(*irtree.ComponentSlot)
			if !ok || cs.Component == nil {
				continue
			}
			if cs.Component.Name == "TrackPanel" {
				panels = append(panels, cs.Component)
			}
			walk(cs.Component.Children)
			walk(cs.Component.ReturnSlots)
			walk(cs.Component.CallSiteSlots)
		}
	}
	walk(tree.Root.Children)

	if len(panels) != 3 {
		t.Fatalf("got %d TrackPanel instances, want 3", len(panels))
	}

	wantLinks := []struct{ href, label string }{
		{"/p/one", "First"},
		{"/p/two", "Second"},
		{"/p/three", "Third"},
	}
	for i, p := range panels {
		if p.Tier != irtree.TierClient {
			t.Errorf("panel %d tier = %v, want client", i, p.Tier)
		}
		if len(p.RefBindings) != 1 || p.RefBindings[0].Target != "panelRef" {
			t.Errorf("panel %d ref bindings = %+v, want panelRef", i, p.RefBindings)
		}
		declCount := 0
		for _, ev := range append(append([]string(nil), p.PreSignalVars...), p.ExtraVars...) {
			if strings.Contains(ev, "var panelRef=") {
				declCount++
			}
		}
		if declCount != 1 {
			t.Errorf("panel %d panelRef decl count = %d (pre=%v extra=%v), want 1", i, declCount, p.PreSignalVars, p.ExtraVars)
		}
		html := renderStaticHTML(p.CallSiteSlots)
		want := `<a href="` + wantLinks[i].href + `">` + wantLinks[i].label + `</a>`
		if html != want {
			t.Errorf("panel %d call-site HTML = %q, want %q", i, html, want)
		}
	}
}

func TestStatelessRefDirectChildKeepsRefDecl(t *testing.T) {
	src := `import { useRef } from "react";
function TrackPanel({ children }) {
  const panelRef = useRef<HTMLDivElement>(null);
  return <div ref={panelRef} onPointerMove={(e) => { if (panelRef.current) panelRef.current.dataset.x = String(e.clientX); }}>{children}</div>;
}
export default function Page() {
  return <main><TrackPanel><a href="/p/solo">Solo</a></TrackPanel></main>;
}`

	tree := annotateAndBuildReact(t, src)
	var panel *irtree.ComponentNode
	for _, n := range tree.Root.Children {
		if cs, ok := n.(*irtree.ComponentSlot); ok && cs.Component != nil && cs.Component.Name == "TrackPanel" {
			panel = cs.Component
		}
	}
	if panel == nil {
		t.Fatal("TrackPanel node not found")
	}
	if panel.Tier != irtree.TierClient {
		t.Fatalf("TrackPanel tier = %v, want client", panel.Tier)
	}
	declCount := 0
	for _, ev := range append(append([]string(nil), panel.PreSignalVars...), panel.ExtraVars...) {
		if strings.Contains(ev, "var panelRef=") {
			declCount++
		}
	}
	if declCount != 1 {
		t.Fatalf("panelRef decl count = %d (pre=%v extra=%v), want 1", declCount, panel.PreSignalVars, panel.ExtraVars)
	}
}
