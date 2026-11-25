package build

import (
	"reflect"
	"testing"
)

// The own checks of the chunks of split batches run batched (runExactChunkBatch):
// every chunk's facts and text are the ones checking it alone gives.
func TestExactChunkBatchMatchesSeparateChecks(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, clientChunkSplitFiles())
	withPreact(t, root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	text := func(url string) string {
		b.clientChunkMu.Lock()
		defer b.clientChunkMu.Unlock()
		return b.clientChunkText[url]
	}
	names := []string{"A", "B", "C", "E", "F"}
	jobs := make([]*roundJob, len(names))
	for i, n := range names {
		jobs[i] = &roundJob{input: chunkReq(root, "src/components/"+n+".tsx")}
	}
	b.runExactChunkBatch(jobs)
	got := make([]*clientChunkResult, len(names))
	gotText := make([]string, len(names))
	for i, n := range names {
		res, _ := jobs[i].output.(*clientChunkResult)
		if res == nil || res.err != nil {
			t.Fatalf("%s: batched check: %+v", n, res)
		}
		got[i], gotText[i] = res, text(res.info.url)
	}
	for i, n := range names {
		want := b.checkClientChunkAlone(jobs[i].input.(clientChunkReq))
		if want.err != nil {
			t.Fatalf("%s: separate check: %v", n, want.err)
		}
		if got[i].info.url != want.info.url || got[i].info.stateful != want.info.stateful {
			t.Errorf("%s: batched url %q stateful %v, separate url %q stateful %v", n, got[i].info.url, got[i].info.stateful, want.info.url, want.info.stateful)
		}
		if !reflect.DeepEqual(got[i].info.inputs, want.info.inputs) {
			t.Errorf("%s: batched inputs %v, separate %v", n, got[i].info.inputs, want.info.inputs)
		}
		if w := text(want.info.url); gotText[i] != w {
			t.Errorf("%s: batched chunk text differs from the separate check's (%d vs %d bytes)", n, len(gotText[i]), len(w))
		}
	}
}
