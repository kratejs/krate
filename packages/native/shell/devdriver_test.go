package shell

import (
	"errors"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/sdk"
)

type viewCtl struct {
	fakeCtl
	views   []sdk.ViewInfo
	queried []string
	emitted []string
}

func (c *viewCtl) QueryView(view, js string) (string, error) {
	c.queried = append(c.queried, view+"|"+js)
	if view == "gone" {
		return "", errors.New(`no view "gone"`)
	}
	return `"` + view + `"`, nil
}
func (c *viewCtl) Views() []sdk.ViewInfo { return c.views }
func (c *viewCtl) Emit(area, payload string) error {
	c.emitted = append(c.emitted, area+"|"+payload)
	if area == "bad" {
		return errors.New("cannot emit")
	}
	return nil
}

func TestServeControlDriverCommands(t *testing.T) {
	c := &viewCtl{views: []sdk.ViewInfo{{ID: "main", Window: "main", Primary: true}, {ID: "v1", Window: "main"}}}
	var out strings.Builder
	ServeControl(strings.NewReader(strings.Join([]string{
		"views",
		"query-in v1 document.title",
		"query-in gone 1",
		"query-in v1",
		"query-in",
		`emit tray {"kind":"click","id":""}`,
		"emit bad {}",
		"emit",
	}, "\n")+"\n"), &out, c)
	got := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{
		`ok [{"id":"main","window":"main","primary":true},{"id":"v1","window":"main","primary":false}]`,
		`ok "v1"`,
		`error no view "gone"`,
		`error query-in needs a view id and a JavaScript expression`,
		`error query-in needs a view id and a JavaScript expression`,
		`ok`,
		`error cannot emit`,
		`error emit needs an area (tray, menu or shortcut) and a JSON payload`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines:\n%s", len(got), out.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
	if c.queried[0] != "v1|document.title" {
		t.Errorf("queried %v", c.queried)
	}
	if c.emitted[0] != `tray|{"kind":"click","id":""}` {
		t.Errorf("emitted %v", c.emitted)
	}
}

func TestServeControlDriverCommandsUnsupported(t *testing.T) {
	var out strings.Builder
	ServeControl(strings.NewReader("views\nquery-in main 1\nemit tray {}\n"), &out, &fakeCtl{})
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if !strings.HasPrefix(l, "error ") || !strings.Contains(l, "not supported here") {
			t.Errorf("got %q", l)
		}
	}
}

func TestJSArg(t *testing.T) {
	for in, want := range map[string]string{
		"1+1":                                "1+1",
		`@json "(() => {\n  return 1\n})()"`: "(() => {\n  return 1\n})()",
	} {
		got, err := jsArg(in)
		if err != nil || got != want {
			t.Errorf("jsArg(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{`@json nope`, `@json "  "`, `@json 5`} {
		if _, err := jsArg(in); err == nil {
			t.Errorf("jsArg(%q) succeeded", in)
		}
	}
	c := &viewCtl{}
	var out strings.Builder
	ServeControl(strings.NewReader("query-in v1 @json \"a\\nb\"\n"), &out, c)
	if len(c.queried) != 1 || c.queried[0] != "v1|a\nb" {
		t.Errorf("queried %q", c.queried)
	}
}
