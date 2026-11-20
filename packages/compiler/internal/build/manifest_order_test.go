package build

import "testing"

// Segment pages finish in any order; the manifest lists them by route.
func TestManifestSegmentPagesSorted(t *testing.T) {
	t.Parallel()
	seg := func(out string) *PageResult {
		return &PageResult{OutName: out, SourcePath: out + ".tsx", IsErrorPage: true, Mode: RenderSSR}
	}
	m := BuildManifest([]*PageResult{seg("zeta/__404.html"), seg("alpha/__404.html"), seg("mid/__404.html")}, "", "")
	if len(m.SegmentPages) != 3 {
		t.Fatalf("segment pages: %+v", m.SegmentPages)
	}
	for i := 1; i < len(m.SegmentPages); i++ {
		if m.SegmentPages[i-1].Route > m.SegmentPages[i].Route {
			t.Fatalf("not sorted: %+v", m.SegmentPages)
		}
	}
}
