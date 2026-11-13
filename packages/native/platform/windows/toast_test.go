package windows

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/toastscheme"
)

// Known ids of parameterized WinRT interfaces, from the Windows SDK. They
// pin the pinterface algorithm used for the toast event handlers.
func TestPinterfaceIID(t *testing.T) {
	cases := []struct{ sig, want string }{
		// IVector<String>
		{"pinterface({913337e9-11a1-4345-a3a2-4e7f956e222d};string)", "98b9acc1-4b56-532e-ac73-03d5291cca90"},
		// IIterable<String>
		{"pinterface({faa585ea-6214-4217-afda-7f46de5869b3};string)", "e2fcc7c1-3bfc-5a0b-b2b0-72e769d1cb7e"},
	}
	for _, c := range cases {
		if got := pinterfaceIID(c.sig); got != c.want {
			t.Errorf("pinterfaceIID(%s) = %s, want %s", c.sig, got, c.want)
		}
	}
	for name, id := range map[string]string{
		"activated": iidToastActivatedHandler, "dismissed": iidToastDismissedHandler, "failed": iidToastFailedHandler,
	} {
		if _, err := parseGUID(id); err != nil {
			t.Errorf("%s handler id %q: %v", name, id, err)
		}
	}
	if iidToastActivatedHandler == iidToastDismissedHandler || iidToastDismissedHandler == iidToastFailedHandler {
		t.Error("handler ids must differ")
	}
}

func TestAUMIDFor(t *testing.T) {
	cases := map[string]string{
		"com.acme.app":   "com.acme.app",
		"dev.krate.demo": "dev.krate.demo",
		"My App":         "My.App",
		"tasks":          "Krate.tasks",
		"":               "Krate.App",
		"a b/c\\d":       "a.b.c.d",
		"..x..":          "Krate.x",
	}
	for in, want := range cases {
		if got := aumidFor(in); got != want {
			t.Errorf("aumidFor(%q) = %q, want %q", in, got, want)
		}
	}
	long := aumidFor("com." + strings.Repeat("a", 300))
	if len(long) != 128 {
		t.Errorf("long aumid has %d chars", len(long))
	}
	if aumidRegistryPath("com.acme.app") != `Software\Classes\AppUserModelId\com.acme.app` {
		t.Error("registry path")
	}
}

func TestToastMode(t *testing.T) {
	if toastModeFor(true) != toastPackaged || toastModeFor(false) != toastRegistered {
		t.Error("toastModeFor")
	}
}

func TestFileURI(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\icon.png`:    "file:///C:/Users/me/icon.png",
		`C:\My Files\a b.png`:     "file:///C:/My%20Files/a%20b.png",
		`\\server\share\icon.png`: "file://server/share/icon.png",
		"dialog-information":      "",
		"":                        "",
		`\rooted\no\drive.png`:    "",
	}
	for in, want := range cases {
		if got := fileURI(in); got != want {
			t.Errorf("fileURI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToastArgsRoundTrip(t *testing.T) {
	for _, c := range [][2]string{{"n1", ""}, {"n 2&x=y", "open"}, {"", "a&b"}, {"é€", "ok"}} {
		id, act, ok := parseToastArgs(toastArgs(c[0], c[1]))
		if !ok || id != c[0] || act != c[1] {
			t.Errorf("round trip %q: %q %q %v", c, id, act, ok)
		}
	}
	if _, _, ok := parseToastArgs("id=x"); ok {
		t.Error("foreign arguments accepted")
	}
	if _, _, ok := parseToastArgs("%zz"); ok {
		t.Error("garbage accepted")
	}
}

// wellFormed parses s as XML and returns the root element name.
func wellFormed(t *testing.T, s string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		_, err := d.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return
			}
			t.Fatalf("not well-formed: %v\n%s", err, s)
		}
	}
}

func TestBuildToastXML(t *testing.T) {
	n := caps.Notification{
		ID: "n1", Title: `Build <done> & "ok"`, Body: "It's 5 > 3\nline two",
		Actions: []caps.NotificationAction{{ID: "open", Label: `Open "it"`}, {ID: "x&y", Label: "<b>"}},
	}
	x := buildToastXML(n, `file:///C:/a%20b/i.png`)
	wellFormed(t, x)
	for _, want := range []string{
		`<text>Build &lt;done&gt; &amp; &#34;ok&#34;</text>`,
		`placement="appLogoOverride" src="file:///C:/a%20b/i.png"`,
		`scenario="`,
	} {
		if want == `scenario="` {
			if strings.Contains(x, want) {
				t.Errorf("unexpected scenario in %s", x)
			}
			continue
		}
		if !strings.Contains(x, want) {
			t.Errorf("missing %s in\n%s", want, x)
		}
	}
	if strings.Contains(x, "<b>") || strings.Contains(x, `"ok"`) {
		t.Errorf("unescaped content in %s", x)
	}
	// Action arguments decode back.
	if !strings.Contains(x, `arguments="id=n1&amp;krate=1"`) && !strings.Contains(x, `arguments="action=open&amp;id=n1&amp;krate=1"`) {
		t.Errorf("action arguments missing in %s", x)
	}
}

func TestBuildToastXMLVariants(t *testing.T) {
	// Injection attempts stay text.
	n := caps.Notification{ID: `"><script/>`, Title: `</text></binding><evil/>`, Body: "x"}
	x := buildToastXML(n, `"/><evil/>`)
	wellFormed(t, x)
	if strings.Contains(x, "<evil/>") || strings.Contains(x, "<script/>") {
		t.Errorf("injection in %s", x)
	}
	// No content still yields a text element (Windows rejects a toast without text).
	wellFormed(t, buildToastXML(caps.Notification{}, ""))
	if !strings.Contains(buildToastXML(caps.Notification{}, ""), "<text>") {
		t.Error("empty notification has no text")
	}
	// Silent, long timeout, critical urgency.
	x = buildToastXML(caps.Notification{Title: "t", Silent: true, TimeoutMS: 20000}, "")
	if !strings.Contains(x, `<audio silent="true"/>`) || !strings.Contains(x, `duration="long"`) {
		t.Errorf("silent/long: %s", x)
	}
	x = buildToastXML(caps.Notification{Title: "t", Urgency: 2, Actions: []caps.NotificationAction{{ID: "a", Label: "A"}}}, "")
	if !strings.Contains(x, `scenario="reminder"`) {
		t.Errorf("critical with action: %s", x)
	}
	x = buildToastXML(caps.Notification{Title: "t", TimeoutMS: -1}, "")
	if strings.Contains(x, "reminder") || !strings.Contains(x, `duration="long"`) {
		t.Errorf("never-expire without buttons must not be a reminder: %s", x)
	}
	// At most five buttons.
	var acts []caps.NotificationAction
	for i := 0; i < 9; i++ {
		acts = append(acts, caps.NotificationAction{ID: "a", Label: "L"})
	}
	if c := strings.Count(buildToastXML(caps.Notification{Title: "t", Actions: acts}, ""), "<action "); c != 5 {
		t.Errorf("%d buttons", c)
	}
	// Invalid XML characters are replaced, not passed through.
	wellFormed(t, buildToastXML(caps.Notification{Title: "a\x00b\x01c\ufffe"}, ""))
}

func TestToastDecisions(t *testing.T) {
	if !dismissEvent(dismissUserCanceled) || !dismissEvent(dismissTimedOut) || dismissEvent(dismissApplicationHid) {
		t.Error("dismissEvent")
	}
	if settingProblem(0) != "" || settingProblem(2) == "" || settingProblem(99) == "" {
		t.Error("settingProblem")
	}
}

func TestAUMIDValues(t *testing.T) {
	v := aumidValues("My App", "")
	if len(v) != 1 || v["DisplayName"] != "My App" {
		t.Errorf("values = %v", v)
	}
	v = aumidValues("My App", `C:\i.png`)
	if v["IconUri"] != `C:\i.png` {
		t.Errorf("values = %v", v)
	}
}

func TestToastXMLProtocolActivation(t *testing.T) {
	scheme := toastscheme.Scheme("com.acme.app")
	n := caps.Notification{ID: "n&1", Title: "t", Actions: []caps.NotificationAction{{ID: "ok", Label: "OK"}, {ID: "later", Label: "Later"}}}
	x := buildToastXMLFor(n, "", scheme)
	wellFormed(t, x)
	if strings.Contains(x, `activationType="foreground"`) {
		t.Errorf("a protocol toast has a foreground activation: %s", x)
	}
	if c := strings.Count(x, `activationType="protocol"`); c != 3 {
		t.Errorf("want the body and two buttons to use protocol activation, got %d in %s", c, x)
	}
	for _, act := range []string{"", "ok", "later"} {
		want := xmlText(toastscheme.URI(scheme, n.ID, act))
		if !strings.Contains(x, `="`+want+`"`) {
			t.Errorf("no activation URL for action %q (%s) in %s", act, want, x)
		}
	}
	// The foreground form is unchanged for an empty scheme.
	if buildToastXMLFor(n, "", "") != buildToastXML(n, "") || !strings.Contains(buildToastXML(n, ""), `activationType="foreground"`) {
		t.Error("buildToastXML must keep foreground activation")
	}
}
