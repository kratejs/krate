package irtree

import "testing"

func TestLocalCodeModuleCoversAliases(t *testing.T) {
	for src, want := range map[string]bool{
		`"./a"`:            true,
		`"../a.ts"`:        true,
		`"@/lib/a"`:        true,
		`"~/lib/a.tsx"`:    true,
		`"@/styles/a.css"`: false,
		`"@scope/pkg"`:     false,
		`"react"`:          false,
	} {
		if got := localCodeModule(src); got != want {
			t.Errorf("localCodeModule(%s) = %v, want %v", src, got, want)
		}
	}
}
