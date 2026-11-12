package caps

import (
	"strings"
	"testing"
)

func TestNormalizeTitleBar(t *testing.T) {
	for in, want := range map[string]string{"": "native", "native": "native", "hidden": "hidden", "none": "none"} {
		got, ok := NormalizeTitleBar(in)
		if !ok || got != want {
			t.Errorf("NormalizeTitleBar(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := NormalizeTitleBar("frameless"); ok {
		t.Error("frameless accepted")
	}
}

func TestParseColor(t *testing.T) {
	cases := map[string]RGBA{
		"#fff":      {255, 255, 255, 255},
		"#102030":   {0x10, 0x20, 0x30, 255},
		"#10203040": {0x10, 0x20, 0x30, 0x40},
		"#abc":      {0xaa, 0xbb, 0xcc, 255},
		"#00000000": {0, 0, 0, 0},
		"#FF8000":   {255, 128, 0, 255},
	}
	for in, want := range cases {
		got, err := ParseColor(in)
		if err != nil || got != want {
			t.Errorf("ParseColor(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "fff", "#ff", "#ggg", "#12345", "#1234567", "red"} {
		if _, err := ParseColor(bad); err == nil {
			t.Errorf("ParseColor(%q) accepted", bad)
		}
	}
}

func TestOverlayNormalize(t *testing.T) {
	o, err := TitleBarOverlay{Color: "#101010", SymbolColor: "#fff"}.Normalize()
	if err != nil || o.Height != DefaultOverlayHeight {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := (TitleBarOverlay{Height: -1}).Normalize(); err == nil {
		t.Error("negative height accepted")
	}
	if _, err := (TitleBarOverlay{Height: 500}).Normalize(); err == nil {
		t.Error("huge height accepted")
	}
	if _, err := (TitleBarOverlay{Color: "blue"}).Normalize(); err == nil || !strings.Contains(err.Error(), "titleBarOverlay.color") {
		t.Errorf("bad color error %v", err)
	}
}

func TestBackdropAndVibrancyTables(t *testing.T) {
	want := map[string]int{"none": 1, "mica": 2, "acrylic": 3, "micaAlt": 4}
	for n, v := range want {
		if got, ok := BackdropType(n); !ok || got != v {
			t.Errorf("BackdropType(%q) = %d, %v", n, got, ok)
		}
	}
	if v, ok := BackdropType(""); !ok || v != 1 {
		t.Errorf("empty backdrop = %d, %v", v, ok)
	}
	if _, ok := BackdropType("glass"); ok {
		t.Error("glass accepted")
	}
	if got := strings.Join(BackdropNames(), ","); got != "acrylic,mica,micaAlt,none" {
		t.Errorf("names %s", got)
	}
	if v, ok := VibrancyMaterial("sidebar"); !ok || v != 7 {
		t.Errorf("sidebar = %d, %v", v, ok)
	}
	if v, ok := VibrancyMaterial("hudWindow"); !ok || v != 13 {
		t.Errorf("hudWindow = %d, %v", v, ok)
	}
	if _, ok := VibrancyMaterial("jelly"); ok {
		t.Error("jelly accepted")
	}
	if len(VibrancyNames()) != 14 {
		t.Errorf("%d materials", len(VibrancyNames()))
	}
}

func TestTabbingAndPolicy(t *testing.T) {
	for in, want := range map[string]int{"": 0, "auto": 0, "preferred": 1, "disallowed": 2} {
		if got, ok := TabbingModeValue(in); !ok || got != want {
			t.Errorf("tabbing %q = %d, %v", in, got, ok)
		}
	}
	if _, ok := TabbingModeValue("many"); ok {
		t.Error("bad tabbing mode accepted")
	}
	for in, want := range map[string]int{"regular": 0, "accessory": 1, "prohibited": 2} {
		if got, ok := ActivationPolicyValue(in); !ok || got != want {
			t.Errorf("policy %q = %d, %v", in, got, ok)
		}
	}
	if _, ok := ActivationPolicyValue("agent"); ok {
		t.Error("bad policy accepted")
	}
}

func TestChromeOptionsValidate(t *testing.T) {
	if !(ChromeOptions{}).IsZero() || !(ChromeOptions{TitleBar: "native"}).IsZero() {
		t.Error("zero options not zero")
	}
	if (ChromeOptions{Transparent: true}).IsZero() {
		t.Error("transparent options are zero")
	}
	o, err := ChromeOptions{TitleBar: "hidden", Overlay: &TitleBarOverlay{}}.Validate()
	if err != nil || o.Overlay.Height != 32 || o.TitleBar != "hidden" {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = ChromeOptions{}.Validate()
	if err != nil || o.TitleBar != "native" {
		t.Fatalf("%+v %v", o, err)
	}
	bad := []ChromeOptions{
		{TitleBar: "x"}, {Backdrop: "glass"}, {Vibrancy: "jelly"},
		{TrafficLight: &Point{X: -1}}, {TabbingMode: "many"},
		{Overlay: &TitleBarOverlay{Color: "nope"}},
		{TabbingID: strings.Repeat("a", 129)},
	}
	for i, b := range bad {
		if _, err := b.Validate(); err == nil {
			t.Errorf("case %d accepted: %+v", i, b)
		}
	}
}

func TestHitTest(t *testing.T) {
	in := HitInput{
		W: 800, H: 600, Border: 6, Resizable: true, CaptionH: 32,
		Buttons: HitRect{Left: 800 - 138, Top: 0, Right: 800, Bottom: 32}, NButtons: 3,
	}
	at := func(x, y int) int { in.X, in.Y = x, y; return HitTest(in) }
	cases := []struct {
		x, y int
		want int
		name string
	}{
		{400, 300, HTCLIENT, "client"},
		{400, 16, HTCAPTION, "caption strip"},
		{10, 16, HTCAPTION, "caption left"},
		{800 - 130, 16, HTMINBUTTON, "minimize"},
		{800 - 69, 16, HTMAXBUTTON, "maximize"},
		{800 - 10, 16, HTCLOSE, "close"},
		{800 - 138, 0, HTMINBUTTON, "buttons win over the top edge"},
		{400, 2, HTTOP, "top edge"},
		{2, 2, HTTOPLEFT, "top left"},
		{798, 598, HTBOTTOMRIGHT, "bottom right"},
		{2, 300, HTLEFT, "left"},
		{797, 300, HTRIGHT, "right"},
		{400, 597, HTBOTTOM, "bottom"},
		{2, 597, HTBOTTOMLEFT, "bottom left"},
		{-1, 5, HTNOWHERE, "outside left"},
		{800, 5, HTNOWHERE, "outside right"},
	}
	for _, c := range cases {
		if got := at(c.x, c.y); got != c.want {
			t.Errorf("%s: (%d,%d) = %d, want %d", c.name, c.x, c.y, got, c.want)
		}
	}
	in.Maximized = true
	if got := at(400, 2); got != HTCAPTION {
		t.Errorf("maximized top = %d, want caption", got)
	}
	in.Maximized, in.Resizable = false, false
	if got := at(2, 300); got != HTCLIENT {
		t.Errorf("fixed window left edge = %d, want client", got)
	}
	// A fixed window with only a close button.
	in.NButtons, in.Buttons = 1, HitRect{Left: 800 - 46, Right: 800, Bottom: 32}
	if got := at(800-20, 10); got != HTCLOSE {
		t.Errorf("close only = %d", got)
	}
	// Two buttons: maximize then close.
	in.NButtons, in.Buttons = 2, HitRect{Left: 800 - 92, Right: 800, Bottom: 32}
	if got := at(800-80, 10); got != HTMAXBUTTON {
		t.Errorf("two buttons left = %d", got)
	}
	if got := at(800-10, 10); got != HTCLOSE {
		t.Errorf("two buttons right = %d", got)
	}
	in.NButtons = 0
	if got := at(800-10, 10); got != HTCAPTION {
		t.Errorf("no buttons = %d", got)
	}
}

func TestQuoteArgs(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a", "b"}, "a b"},
		{[]string{""}, `""`},
		{[]string{"a b"}, `"a b"`},
		{[]string{`a"b`}, `"a\"b"`},
		{[]string{`C:\Program Files\x\`}, `"C:\Program Files\x\\"`},
		{[]string{`a\"b c`}, `"a\\\"b c"`},
		{[]string{`--open=C:\a\b`}, `--open=C:\a\b`},
		{[]string{"--new-window", "my file.txt"}, `--new-window "my file.txt"`},
	}
	for _, c := range cases {
		if got := QuoteArgs(c.in); got != c.want {
			t.Errorf("QuoteArgs(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// splitArgs is CommandLineToArgvW for arguments after the program name.
func splitArgs(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		var cur []byte
		inQ := false
		for i < len(s) {
			c := s[i]
			if c == '\\' {
				n := 0
				for i < len(s) && s[i] == '\\' {
					n++
					i++
				}
				if i < len(s) && s[i] == '"' {
					cur = append(cur, make([]byte, n/2)...)
					for j := range cur[len(cur)-n/2:] {
						cur[len(cur)-n/2+j] = '\\'
					}
					if n%2 == 1 {
						cur = append(cur, '"')
						i++
					}
				} else {
					for j := 0; j < n; j++ {
						cur = append(cur, '\\')
					}
				}
				continue
			}
			if c == '"' {
				inQ = !inQ
				i++
				continue
			}
			if !inQ && (c == ' ' || c == '\t') {
				break
			}
			cur = append(cur, c)
			i++
		}
		out = append(out, string(cur))
	}
	return out
}

func TestQuoteArgsRoundTrip(t *testing.T) {
	sets := [][]string{
		{"a", "b c", `d"e`, ``, `f\`, `g\\"h`, `C:\Program Files\App\`, "tab\tx", `\\server\share`, `"`, `\"`, `a  b`},
		{"--open", `C:\Users\me\My Documents\a "b".txt`},
	}
	for _, set := range sets {
		line := QuoteArgs(set)
		got := splitArgs(line)
		if len(got) != len(set) {
			t.Fatalf("%q -> %q (%d args, want %d)", set, line, len(got), len(set))
		}
		for i := range set {
			if got[i] != set[i] {
				t.Errorf("arg %d: %q -> %q", i, set[i], got[i])
			}
		}
	}
}

func TestValidateJumpTasks(t *testing.T) {
	ok := []JumpTask{{Title: "New window", Args: []string{"--new-window"}, IconPath: `C:\app.exe`}}
	if err := ValidateJumpTasks(ok); err != nil {
		t.Fatal(err)
	}
	if err := ValidateJumpTasks([]JumpTask{{Title: " "}}); err == nil {
		t.Error("blank title accepted")
	}
	if err := ValidateJumpTasks([]JumpTask{{Title: "a", Args: []string{"x\x00y"}}}); err == nil {
		t.Error("NUL accepted")
	}
	if err := ValidateJumpTasks([]JumpTask{{Title: "a", IconPath: "rel.ico"}}); err == nil {
		t.Error("relative icon accepted")
	}
	if err := ValidateJumpTasks(make([]JumpTask, MaxJumpTasks+1)); err == nil {
		t.Error("too many tasks accepted")
	}
}

func TestValidateThumbButtons(t *testing.T) {
	if err := ValidateThumbButtons([]ThumbButton{{ID: "a", Tooltip: "A"}, {ID: "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateThumbButtons([]ThumbButton{{ID: "a"}, {ID: "a"}}); err == nil {
		t.Error("duplicate id accepted")
	}
	if err := ValidateThumbButtons([]ThumbButton{{}}); err == nil {
		t.Error("empty id accepted")
	}
	if err := ValidateThumbButtons(make([]ThumbButton, 8)); err == nil {
		t.Error("eight buttons accepted")
	}
	if err := ValidateThumbButtons([]ThumbButton{{ID: "a", Tooltip: strings.Repeat("x", 260)}}); err == nil {
		t.Error("long tooltip accepted")
	}
}

func TestShareItemsValidate(t *testing.T) {
	if err := (ShareItems{Text: "hi"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ShareItems{URLs: []string{"https://krate.js.org"}, Files: []string{"/tmp/a.txt"}}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ShareItems{
		{},
		{URLs: []string{"javascript:alert(1)"}},
		{URLs: []string{"file:///etc/passwd"}},
		{Files: []string{"rel/path"}},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestEdgeIndex(t *testing.T) {
	if EdgeIndex("topLeft") != 0 || EdgeIndex("bottomRight") != 7 || EdgeIndex("top") != 1 || EdgeIndex("x") != -1 {
		t.Error("edge indexes")
	}
}

func TestJumpArgs(t *testing.T) {
	cases := []struct{ in, want []string }{
		{nil, nil},
		{[]string{"notes.txt"}, []string{"notes.txt"}},
		{[]string{"acme://open?x=1"}, []string{"acme://open?x=1"}},
		{[]string{"--new-window"}, []string{"--", "--new-window"}},
		{[]string{"open", "-x"}, []string{"--", "open", "-x"}},
		{[]string{"--"}, []string{"--"}},
	}
	for _, c := range cases {
		got := JumpArgs(c.in)
		if len(got) != len(c.want) {
			t.Errorf("JumpArgs(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("JumpArgs(%q) = %q, want %q", c.in, got, c.want)
			}
		}
	}
}
