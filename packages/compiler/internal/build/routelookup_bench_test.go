package build

import (
	"fmt"
	"testing"
)

func benchRoutePages(n int) []ManifestPage {
	pages := make([]ManifestPage, 0, n)
	for i := 0; len(pages) < n-1; i++ {
		pages = append(pages,
			ManifestPage{Route: fmt.Sprintf("sec%d/page", i), Source: fmt.Sprintf("s%d.tsx", i), Mode: "ssr"},
			ManifestPage{Route: fmt.Sprintf("sec%d/[id]", i), Source: fmt.Sprintf("d%d.tsx", i), Mode: "ssr"},
		)
	}
	pages = pages[:n-1]
	// Sorts after every other first segment, so a hit is the last candidate.
	return append(pages, ManifestPage{Route: "~/[...all]", Source: "last.tsx", Mode: "ssr"})
}

func BenchmarkRouteLookup(b *testing.B) {
	for _, n := range []int{20, 200, 1000} {
		s := &SSRServer{manifest: &ServerManifest{Pages: benchRoutePages(n)}}
		b.Run(fmt.Sprintf("routes=%d/last", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if p, _ := s.FindPageForRoute("/~/a/b/c"); p == nil {
					b.Fatal("expected a match")
				}
			}
		})
		b.Run(fmt.Sprintf("routes=%d/miss", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if p, _ := s.FindPageForRoute("/nope/x/y"); p != nil {
					b.Fatal("unexpected match")
				}
			}
		})
	}
}
