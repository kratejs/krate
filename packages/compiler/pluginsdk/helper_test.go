package plug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResultEmitAndInjectHelpers(t *testing.T) {
	var r Result

	r.EmitFile("a.txt", "hello")
	if len(r.Files) != 1 || r.Files[0].Path != "a.txt" || r.Files[0].Content != "hello" {
		t.Errorf("EmitFile = %+v", r.Files)
	}

	r.InjectHead("<meta>")
	r.InjectHead("<script>")
	if r.HeadHTML == nil || *r.HeadHTML != "<meta><script>" {
		t.Errorf("InjectHead = %v", r.HeadHTML)
	}

	r.InjectCSS(".a{}")
	r.InjectCSS(".b{}")
	if r.RawCSS == nil || *r.RawCSS != ".a{}\n.b{}" {
		t.Errorf("InjectCSS = %v", r.RawCSS)
	}
}

func TestPathHelpers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveFile(root, "./a.txt")
	if err != nil {
		t.Fatalf("ResolveFile: %v", err)
	}
	if want := filepath.Join(root, "a.txt"); got != want {
		t.Errorf("ResolveFile = %q, want %q", got, want)
	}

	content, err := ReadFile(root, "./a.txt")
	if err != nil || content != "hi" {
		t.Fatalf("ReadFile = %q (err %v)", content, err)
	}

	if _, err := ReadFile(root, "../escape.txt"); err == nil {
		t.Fatal("expected traversal error")
	}

	if err := WriteFileToRoot(root, "public/b.txt", []byte("b")); err != nil {
		t.Fatalf("WriteFileToRoot: %v", err)
	}
	if err := WriteFileToRoot(root, "../escape.txt", []byte("no")); err == nil {
		t.Fatal("expected traversal error from WriteFileToRoot")
	}
}

// TestDispatchFoldsInjectHelpers verifies Result.InjectHead/InjectCSS emissions
// are folded into the concrete context fields the host applies.
func TestDispatchFoldsInjectHelpers(t *testing.T) {
	srv := &pluginServer{hooks: Hooks{
		AfterRender: func(ctx *RenderArgs) error {
			ctx.InjectHead(`<meta name="injected">`)
			ctx.InjectCSS(".injected{}")
			return nil
		},
	}}

	args, _ := json.Marshal(map[string]interface{}{
		"page":     "index.tsx",
		"html":     "<p>x</p>",
		"headHTML": "<title>t</title>",
		"hasJS":    false,
		"rawCSS":   ".base{}",
	})
	var out json.RawMessage
	if err := srv.Dispatch(DispatchRequest{Kind: "build", Hook: "AfterRender", Args: args}, &out); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var res Result
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	if res.HeadHTML == nil || *res.HeadHTML != `<title>t</title><meta name="injected">` {
		t.Errorf("HeadHTML = %v", res.HeadHTML)
	}
	if res.RawCSS == nil || *res.RawCSS != ".base{}\n.injected{}" {
		t.Errorf("RawCSS = %v", res.RawCSS)
	}
}

// TestDispatchEmitFile verifies Result.EmitFile reaches the output envelope.
func TestDispatchEmitFile(t *testing.T) {
	srv := &pluginServer{hooks: Hooks{
		BeforeBuild: func(ctx *BuildArgs) error {
			ctx.EmitFile("x.txt", "y")
			return nil
		},
	}}

	args, _ := json.Marshal(BuildArgs{Root: "R"})
	var out json.RawMessage
	if err := srv.Dispatch(DispatchRequest{Kind: "build", Hook: "BeforeBuild", Args: args}, &out); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var res Result
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "x.txt" || res.Files[0].Content != "y" {
		t.Errorf("Files = %+v", res.Files)
	}
}

// TestDispatchKrateInfoFields verifies the shared metadata keys are decoded into
// KrateInfo on non-build contexts.
func TestDispatchKrateInfoFields(t *testing.T) {
	var seen KrateInfo
	srv := &pluginServer{hooks: Hooks{
		AfterRender: func(ctx *RenderArgs) error {
			seen = ctx.KrateInfo
			return nil
		},
	}}

	args, _ := json.Marshal(map[string]interface{}{
		"page":        "index.tsx",
		"html":        "<p>x</p>",
		"headHTML":    "",
		"rawCSS":      "",
		"projectRoot": "/proj",
		"outDir":      "/proj/dist",
		"pagesDir":    "/proj/src/pages",
		"version":     "1.2.3",
		"devMode":     true,
		"pages":       []string{"a.tsx", "b.tsx"},
	})
	var out json.RawMessage
	if err := srv.Dispatch(DispatchRequest{Kind: "build", Hook: "AfterRender", Args: args}, &out); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if seen.ProjectRoot != "/proj" || seen.OutDir != "/proj/dist" || seen.PagesDir != "/proj/src/pages" {
		t.Errorf("KrateInfo = %+v", seen)
	}
	if seen.Version != "1.2.3" || !seen.DevMode || len(seen.Pages) != 2 {
		t.Errorf("KrateInfo = %+v", seen)
	}
}
