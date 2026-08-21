package bundler

import (
	"testing"
)

func TestHasDirective(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		directive string
		want     bool
	}{
		{
			name:      "line comment @server",
			source:    "// @server\nexport default function Foo() {}",
			directive: "@server",
			want:      true,
		},
		{
			name:      "line comment @runtime",
			source:    "// @runtime\nexport default function Bar() {}",
			directive: "@runtime",
			want:      true,
		},
		{
			name:      "line comment with description",
			source:    "// @server My server component\nexport default function Foo() {}",
			directive: "@server",
			want:      true,
		},
		{
			name:      "block comment @server",
			source:    "/* @server */\nexport default function Foo() {}",
			directive: "@server",
			want:      true,
		},
		{
			name:      "block comment with stars",
			source:    "/** @server */\nexport default function Foo() {}",
			directive: "@server",
			want:      true,
		},
		{
			name:      "skips blank lines",
			source:    "\n\n// @server\nexport default function Foo() {}",
			directive: "@server",
			want:      true,
		},
		{
			name:      "wrong directive",
			source:    "// @server\nexport default function Foo() {}",
			directive: "@runtime",
			want:      false,
		},
		{
			name:      "no directive",
			source:    "export default function Foo() {}",
			directive: "@server",
			want:      false,
		},
		{
			name:      "directive not first",
			source:    "import { foo } from 'bar';\n// @server\nexport default function Foo() {}",
			directive: "@server",
			want:      false,
		},
		{
			name:      "case insensitive",
			source:    "// @Server\nexport default function Foo() {}",
			directive: "@server",
			want:      true,
		},
		{
			name:      "import before directive stops scan",
			source:    "import React from 'react';\n// @server\nexport default function Foo() {}",
			directive: "@server",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasDirective(tt.source, tt.directive)
			if got != tt.want {
				t.Errorf("HasDirective(%q, %q) = %v, want %v", tt.source, tt.directive, got, tt.want)
			}
		})
	}
}

func TestHasServerDirective(t *testing.T) {
	if !HasServerDirective("// @server\nexport default function Foo() {}") {
		t.Error("expected HasServerDirective to return true")
	}
	if HasServerDirective("// @runtime\nexport default function Foo() {}") {
		t.Error("expected HasServerDirective to return false for @runtime")
	}
}

func TestHasRuntimeDirective(t *testing.T) {
	if !HasRuntimeDirective("// @runtime\nexport default function Foo() {}") {
		t.Error("expected HasRuntimeDirective to return true")
	}
	if HasRuntimeDirective("// @server\nexport default function Foo() {}") {
		t.Error("expected HasRuntimeDirective to return false for @server")
	}
}

func TestIsServerComponentFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"components/Foo.server.tsx", true},
		{"components/Foo.server.ts", true},
		{"components/Foo.server.jsx", true},
		{"components/Foo.server.js", true},
		{"components/Foo.tsx", false},
		{"components/Foo.runtime.tsx", false},
	}
	for _, tt := range tests {
		if got := IsServerComponentFile(tt.path); got != tt.want {
			t.Errorf("IsServerComponentFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIsRuntimeComponentFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"components/Foo.runtime.tsx", true},
		{"components/Foo.runtime.ts", true},
		{"components/Foo.runtime.jsx", true},
		{"components/Foo.runtime.js", true},
		{"components/Foo.tsx", false},
		{"components/Foo.server.tsx", false},
	}
	for _, tt := range tests {
		if got := IsRuntimeComponentFile(tt.path); got != tt.want {
			t.Errorf("IsRuntimeComponentFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestClassifyComponent(t *testing.T) {
	tests := []struct {
		name              string
		source            string
		filePath          string
		serverComponents  []string
		runtimeComponents []string
		serverDirs        []string
		runtimeDirs       []string
		want              ComponentClass
	}{
		{
			name:   "default is client",
			source: "export default function Foo() {}",
			filePath: "components/Foo.tsx",
			want:   ComponentClassClient,
		},
		{
			name:   "@server directive",
			source: "// @server\nexport default function Foo() {}",
			filePath: "components/Foo.tsx",
			want:   ComponentClassServer,
		},
		{
			name:   "@runtime directive",
			source: "// @runtime\nexport default function Bar() {}",
			filePath: "components/Bar.tsx",
			want:   ComponentClassRuntime,
		},
		{
			name:   "@static directive",
			source: "// @static\nexport default function Static() {}",
			filePath: "components/Static.tsx",
			want:   ComponentClassStatic,
		},
		{
			name:   "*.server.tsx convention",
			source: "export default function Foo() {}",
			filePath: "components/Foo.server.tsx",
			want:   ComponentClassServer,
		},
		{
			name:   "*.runtime.tsx convention",
			source: "export default function Bar() {}",
			filePath: "components/Bar.runtime.tsx",
			want:   ComponentClassRuntime,
		},
		{
			name:   "*.static.tsx convention",
			source: "export default function Static() {}",
			filePath: "components/Static.static.tsx",
			want:   ComponentClassStatic,
		},
		{
			name:              "config serverComponents list",
			source:            "export default function DataTable() {}",
			filePath:          "components/DataTable.tsx",
			serverComponents:  []string{"DataTable"},
			runtimeComponents: nil,
			want:              ComponentClassServer,
		},
		{
			name:              "config runtimeComponents list",
			source:            "export default function AuthCheck() {}",
			filePath:          "components/AuthCheck.tsx",
			serverComponents:  nil,
			runtimeComponents: []string{"AuthCheck"},
			want:              ComponentClassRuntime,
		},
		{
			name:     "serverDirs match",
			source:   "export default function Foo() {}",
			filePath: "src/components/server/Foo.tsx",
			serverDirs: []string{"src/components/server"},
			want:     ComponentClassServer,
		},
		{
			name:      "runtimeDirs match",
			source:    "export default function Bar() {}",
			filePath:  "src/components/runtime/Bar.tsx",
			runtimeDirs: []string{"src/components/runtime"},
			want:      ComponentClassRuntime,
		},
		{
			name:   "directive takes priority over config",
			source: "// @server\nexport default function Foo() {}",
			filePath: "components/Foo.tsx",
			serverComponents: nil,
			runtimeComponents: []string{"Foo"},
			want:   ComponentClassServer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyComponent(tt.source, tt.filePath, tt.serverComponents, tt.runtimeComponents, tt.serverDirs, tt.runtimeDirs)
			if got != tt.want {
				t.Errorf("ClassifyComponent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractComponentName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"components/Foo.tsx", "Foo"},
		{"/abs/path/Bar.server.tsx", "Bar"},
		{"components/Baz.runtime.ts", "Baz"},
		{"JustName.jsx", "JustName"},
	}
	for _, tt := range tests {
		if got := extractComponentName(tt.path); got != tt.want {
			t.Errorf("extractComponentName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
