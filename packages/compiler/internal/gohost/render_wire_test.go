package gohost

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/regionwire"
)

// The generated render package answers binary frames to a client that asks for
// them and NDJSON to one that does not (an older Krate server). The test
// compiles the generated sources and runs Handle in both modes.
func TestGeneratedHandleNegotiatesFrameFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles generated code")
	}
	dir := t.TempDir()
	src, err := RenderSource()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":    "module example.com/render\n\ngo 1.21\n",
		"render.go": src,
		"wire.go":   regionwire.HostSource("render"),
		"handle_test.go": `package render

import (
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

const body = ` + "`" + `{"regions":[{"id":"a","name":"Ok","props":"{}"},{"id":"b","name":"Bad","props":"{}"}]}` + "`" + `

func init() {
	Register("Ok", func(p, r string) (string, Vary, error) {
		return "<b>1 & 2 < 3</b>", Vary{Query: []string{"q"}}, nil
	})
	Register("Bad", func(p, r string) (string, Vary, error) { return "", Vary{}, io.ErrUnexpectedEOF })
}

func serve(accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/__krate/gorender", strings.NewReader(body))
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	Handle(w, req)
	return w
}

func TestNDJSONWithoutAccept(t *testing.T) {
	w := serve("")
	if ct := w.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("content type %q", ct)
	}
	if !strings.Contains(w.Body.String(), "<b>1 & 2 < 3</b>") || !strings.Contains(w.Body.String(), "unexpected EOF") {
		t.Fatalf("body %s", w.Body.String())
	}
}

func TestBinaryWithAccept(t *testing.T) {
	w := serve(ContentType)
	if ct := w.Header().Get("Content-Type"); ct != ContentType {
		t.Fatalf("content type %q", ct)
	}
	b := w.Body.Bytes()
	if !bytes.Contains(b, []byte("<b>1 & 2 < 3</b>")) || !bytes.Contains(b, []byte("unexpected EOF")) {
		t.Fatalf("body %q", b)
	}
}
` + "",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated render package: %v\n%s", err, out)
	}
}
