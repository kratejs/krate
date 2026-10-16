package gohost

import (
	"strings"
	"testing"
)

func TestSplitGoMod(t *testing.T) {
	content := "module example.com/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/x/y v1.2.3\n)\n\nreplace github.com/a/b => ../b\n"
	ver, directives := SplitGoMod(content)
	if ver != "1.23" {
		t.Fatalf("version = %q, want 1.23", ver)
	}
	if strings.Contains(directives, "module ") || strings.Contains(directives, "go 1.23") {
		t.Fatalf("module/go clauses leaked into directives:\n%s", directives)
	}
	if !strings.Contains(directives, "github.com/x/y v1.2.3") {
		t.Fatalf("require directive lost:\n%s", directives)
	}
}

func TestRebaseReplacePaths(t *testing.T) {
	in := "require github.com/x/y v1.2.3\nreplace github.com/a/b => ../b\nreplace example.com/c => ./vendor/c\nreplace example.com/abs => /abs/path"
	out := RebaseReplacePaths(in)
	if !strings.Contains(out, "replace github.com/a/b => ../../../b") {
		t.Fatalf("../b not rebased:\n%s", out)
	}
	if !strings.Contains(out, "replace example.com/c => ../../vendor/c") {
		t.Fatalf("./vendor/c not rebased:\n%s", out)
	}
	if !strings.Contains(out, "replace example.com/abs => /abs/path") {
		t.Fatalf("absolute replace path must be untouched:\n%s", out)
	}
}
