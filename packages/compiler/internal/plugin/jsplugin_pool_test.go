package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A pooled VM must not carry capability state (injected head/CSS, emitted files)
// from one hook invocation into the next: krateCapabilities is per invocation.
func TestJSPluginPoolNoCapabilityLeak(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "leak-plugin",
  hooks: {
    BeforeBuild(ctx, options, krate) {
      krate.injectHead('<meta name="first" content="1">');
      return {};
    },
    AfterBuild(ctx, options, krate) {
      const r = krate.injectHead('<meta name="second" content="2">');
      return { files: [{ path: "second.html", content: r.headHTML }] };
    },
  },
};
`)
	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: []string{"index.tsx"}}

	// Run two different hooks back to back; the pool may reuse one VM.
	if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("BeforeBuild: %v", err)
	}
	if err := RunCommunityPlugins("AfterBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("AfterBuild: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "second.html"))
	if err != nil {
		t.Fatalf("reading second.html: %v", err)
	}
	if got, want := string(data), `<meta name="second" content="2">`; got != want {
		t.Fatalf("AfterBuild headHTML = %q, want %q (capability state leaked across invocations)", got, want)
	}
}

// BenchmarkJSPluginHook measures a repeated hook invocation: pooled VM reuse vs
// the previous fresh-VM-per-call model (KRATE_NO_PLUGIN_VM_POOL=1).
func BenchmarkJSPluginHook(b *testing.B) {
	root, outDir, cfg := writeTestPlugin(b, `
export default {
  name: "bench-plugin",
  hooks: {
    BeforeBuild(ctx, options, krate) {
      return { files: [{ path: "out.txt", content: String(ctx.pages.length) }] };
    },
  },
};
`)
	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: []string{"index.tsx", "about.tsx"}}
	run := func() {
		if err := runJSPluginHook("BeforeBuild", cfg, root, outDir, CommunityEnv{}, ctx); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("pooled", func(b *testing.B) {
		run()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			run()
		}
	})
	b.Run("fresh", func(b *testing.B) {
		b.Setenv("KRATE_NO_PLUGIN_VM_POOL", "1")
		run()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			run()
		}
	})
}
