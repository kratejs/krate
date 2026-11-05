package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// The hook context reaches the plugin as parsed JSON, not as JavaScript source:
// quotes, backslashes, line separators, script-closing text and a large body
// must all arrive intact.
func TestJSPluginHookContextRoundTrip(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "echo-plugin",
  hooks: {
    AfterPage(ctx) { return { html: ctx.html + "|end" }; },
  },
};
`)
	html := strings.Repeat(`<p class="a">say "hi" \ back\slash 'q' `+"  "+` </script><!-- ${x} `+"`tick`"+` emoji \U0001F600 \u0000 </p>`+"\n", 4000)
	ctx := &PageHookCtx{Page: "p.tsx", OutName: "p", HTML: html}
	if err := RunCommunityPlugins("AfterPage", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	if ctx.HTML != html+"|end" {
		t.Fatalf("html did not round-trip: got %d bytes, want %d", len(ctx.HTML), len(html)+4)
	}
}

type unserializableCtx struct{}

func (unserializableCtx) MarshalJSON() ([]byte, error) { return nil, errors.New("boom") }

// A plugin that does not implement a hook must not pay for serializing its
// context: the context is only requested once the hook is known to exist.
func TestJSPluginUnimplementedHookSkipsContext(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "partial-plugin",
  hooks: { AfterRender() { return {}; } },
};
`)
	if err := RunCommunityPlugins("AfterPage", []config.PluginConfig{cfg}, root, outDir, unserializableCtx{}); err != nil {
		t.Fatalf("unimplemented hook serialized its context: %v", err)
	}
	err := RunCommunityPlugins("AfterRender", []config.PluginConfig{cfg}, root, outDir, unserializableCtx{})
	if err == nil || !strings.Contains(err.Error(), "serializing hook context") {
		t.Fatalf("implemented hook with an unserializable context: err = %v, want a serializing error", err)
	}
}
