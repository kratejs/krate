package login

import (
	"strings"
	"testing"
)

func TestDesktopQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/opt/acme/acme": "/opt/acme/acme",
		"--background":   "--background",
		"/opt/my app/x":  `"/opt/my app/x"`,
		`a"b`:            `"a\"b"`,
		"$HOME":          `"\$HOME"`,
		"100%":           `"100%%"`,
		"":               `""`,
	} {
		if got := desktopQuote(in); got != want {
			t.Errorf("desktopQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestAutostartEntry(t *testing.T) {
	e := autostartEntry("Acme\nEvil=1", "/opt/my app/acme", []string{"--background", "a b"})
	for _, want := range []string{"[Desktop Entry]\n", "Type=Application\n", "Name=Acme Evil=1\n",
		`Exec="/opt/my app/acme" --background "a b"` + "\n", "X-GNOME-Autostart-enabled=true\n"} {
		if !strings.Contains(e, want) {
			t.Errorf("entry lacks %q:\n%s", want, e)
		}
	}
	if !autostartEnabled(e) {
		t.Error("a fresh entry reads as disabled")
	}
	if autostartEnabled(e + "Hidden=true\n") {
		t.Error("Hidden=true is disabled")
	}
	if autostartEnabled(strings.Replace(e, "Autostart-enabled=true", "Autostart-enabled=false", 1)) {
		t.Error("X-GNOME-Autostart-enabled=false is disabled")
	}
}

func TestLaunchAgentPlist(t *testing.T) {
	p := launchAgentPlist("com.acme.app", "/Applications/A&B.app/Contents/MacOS/a", []string{"--x"})
	for _, want := range []string{"<string>com.acme.app</string>", "<string>/Applications/A&amp;B.app/Contents/MacOS/a</string>",
		"<string>--x</string>", "<key>RunAtLoad</key>\n\t<true/>"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q:\n%s", want, p)
		}
	}
}

func TestWindowsCommandLine(t *testing.T) {
	got := windowsCommandLine(`C:\Program Files\Acme\acme.exe`, []string{"--background", `a "b"`, `C:\dir\`, ""})
	want := `"C:\Program Files\Acme\acme.exe" --background "a \"b\"" C:\dir\ ""`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if g := windowsCommandLine("x.exe", []string{`has space\`}); g != `"x.exe" "has space\\"` {
		t.Errorf("trailing slashes before the closing quote must double: %s", g)
	}
}
