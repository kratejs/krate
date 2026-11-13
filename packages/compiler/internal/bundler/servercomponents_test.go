package bundler

import (
	"testing"
)

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
			name:     "default is client",
			source:   "export default function Foo() {}",
			filePath: "components/Foo.tsx",
			want:     ComponentClassClient,
		},
		{
			name:     "@server directive",
			source:   "// @server\nexport default function Foo() {}",
			filePath: "components/Foo.tsx",
			want:     ComponentClassServer,
		},
		{
			name:     "@runtime directive",
			source:   "// @runtime\nexport default function Bar() {}",
			filePath: "components/Bar.tsx",
			want:     ComponentClassRuntime,
		},
		{
			name:     "@static directive",
			source:   "// @static\nexport default function Static() {}",
			filePath: "components/Static.tsx",
			want:     ComponentClassStatic,
		},
		{
			name:     "*.server.tsx convention",
			source:   "export default function Foo() {}",
			filePath: "components/Foo.server.tsx",
			want:     ComponentClassServer,
		},
		{
			name:     "*.runtime.tsx convention",
			source:   "export default function Bar() {}",
			filePath: "components/Bar.runtime.tsx",
			want:     ComponentClassRuntime,
		},
		{
			name:     "*.static.tsx convention",
			source:   "export default function Static() {}",
			filePath: "components/Static.static.tsx",
			want:     ComponentClassStatic,
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
			name:       "serverDirs match",
			source:     "export default function Foo() {}",
			filePath:   "src/components/server/Foo.tsx",
			serverDirs: []string{"src/components/server"},
			want:       ComponentClassServer,
		},
		{
			name:        "runtimeDirs match",
			source:      "export default function Bar() {}",
			filePath:    "src/components/runtime/Bar.tsx",
			runtimeDirs: []string{"src/components/runtime"},
			want:        ComponentClassRuntime,
		},
		{
			name:              "directive takes priority over config",
			source:            "// @server\nexport default function Foo() {}",
			filePath:          "components/Foo.tsx",
			serverComponents:  nil,
			runtimeComponents: []string{"Foo"},
			want:              ComponentClassServer,
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
