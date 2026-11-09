package build

import (
	"reflect"
	"testing"
)

func TestDevNetworkURLs(t *testing.T) {
	lan := []string{"192.168.1.42", "10.0.0.7"}
	cases := []struct {
		name string
		host string
		want []string
	}{
		{"all interfaces", "0.0.0.0", []string{"http://192.168.1.42:3000", "http://10.0.0.7:3000"}},
		{"ipv6 wildcard", "::", []string{"http://192.168.1.42:3000", "http://10.0.0.7:3000"}},
		{"empty host", "", []string{"http://192.168.1.42:3000", "http://10.0.0.7:3000"}},
		{"loopback only", "127.0.0.1", nil},
		{"localhost", "localhost", nil},
		{"one address", "192.168.1.42", []string{"http://192.168.1.42:3000"}},
	}
	for _, c := range cases {
		if got := devNetworkURLs(c.host, 3000, lan); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if got := devNetworkURLs("0.0.0.0", 3000, nil); len(got) != 0 {
		t.Errorf("no LAN address, no URL: %v", got)
	}
}

func TestParseShortcut(t *testing.T) {
	for in, want := range map[string]shortcutAction{
		"r\n": shortcutRestart, "R": shortcutRestart, " o \r\n": shortcutOpen, "c": shortcutClear,
		"h": shortcutHelp, "?": shortcutHelp, "": shortcutNone, "rr": shortcutNone, "restart": shortcutNone,
	} {
		if got := parseShortcut(in); got != want {
			t.Errorf("parseShortcut(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestShortcutRestartRequestsRestart(t *testing.T) {
	hub := NewDevHub()
	(&devShortcuts{hub: hub, url: "http://localhost:3000"}).run(shortcutRestart)
	select {
	case <-hub.RestartRequested():
	default:
		t.Fatal("the restart shortcut did not ask the hub to restart")
	}
}
