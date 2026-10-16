package main

import "testing"

func TestPluginIdent(t *testing.T) {
	cases := map[string]string{
		"@scope/my-plugin": "myPlugin",
		"my-plugin":        "myPlugin",
		"my_plugin":        "myPlugin",
		"plugin":           "plugin",
		"@krate/docs":      "docs",
	}
	for in, want := range cases {
		if got := pluginIdent(in); got != want {
			t.Errorf("pluginIdent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPluginRegisterSnippet(t *testing.T) {
	s := pluginRegisterSnippet("@scope/analytics")
	if !contains(s, "@scope/analytics") || !contains(s, "analytics()") {
		t.Errorf("snippet missing package/ident: %s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
