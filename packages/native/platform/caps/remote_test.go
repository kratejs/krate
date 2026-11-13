package caps

import (
	"testing"
)

func mustPolicy(t *testing.T, o RemoteOptions) *RemotePolicy {
	t.Helper()
	p, err := o.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRemoteDecide(t *testing.T) {
	p := mustPolicy(t, RemoteOptions{URL: "https://example.com/a", Allow: []string{"https://*.cdn.example.net", "http://127.0.0.1:8080"}})
	cases := map[string]NavVerdict{
		"https://example.com/other":       NavAllow,
		"https://EXAMPLE.com:443/x":       NavAllow,
		"https://example.com:444/x":       NavExternal,
		"http://example.com/":             NavExternal,
		"https://a.cdn.example.net/":      NavAllow,
		"https://cdn.example.net/":        NavExternal,
		"https://evilcdn.example.net/":    NavExternal,
		"https://a.cdn.example.net.evil/": NavExternal,
		"http://127.0.0.1:8080/x":         NavAllow,
		"http://127.0.0.1:8081/x":         NavExternal,
		"https://example.com@evil.com/":   NavBlock,
		"https://evil.com/":               NavExternal,
		"mailto:a@b.c":                    NavExternal,
		"krate://app/":                    NavBlock,
		"krate://app/__krate/call":        NavBlock,
		"file:///etc/passwd":              NavBlock,
		"data:text/html,<b>x":             NavBlock,
		"blob:https://example.com/uuid":   NavBlock,
		"javascript:alert(1)":             NavBlock,
		"about:blank":                     NavAllow,
		"about:srcdoc":                    NavBlock,
		"https://app.localhost/":          NavBlock,
		"":                                NavBlock,
		"https://":                        NavBlock,
		"ftp://example.com/":              NavBlock,
		"HTTPS://example.com/":            NavAllow,
		"https://example.com\\@evil.com/": NavBlock,
	}
	for in, want := range cases {
		if got := p.Decide(in); got != want {
			t.Errorf("Decide(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRemoteResolveErrors(t *testing.T) {
	bad := []RemoteOptions{
		{URL: ""}, {URL: "krate://app/"}, {URL: "file:///x"}, {URL: "https://u:p@a.test/"}, {URL: "javascript:1"},
		{URL: "https://a.test/", Allow: []string{"https://*"}},
		{URL: "https://a.test/", Allow: []string{"https://*.com"}},
		{URL: "https://a.test/", Allow: []string{"https://a.*.test"}},
		{URL: "https://a.test/", Allow: []string{"https://b.test/path"}},
		{URL: "https://a.test/", Allow: []string{"krate://app"}},
		{URL: "https://a.test/", Partition: "../x"},
		{URL: "https://a.test/", Blocked: "maybe"},
		{URL: "https://app.localhost/"},
		{URL: "https://a.test/", Allow: []string{"https://app.localhost"}},
		{URL: "https://a.test/", Allow: []string{"https://*.app.localhost"}},
	}
	for _, o := range bad {
		if _, err := o.Resolve(); err == nil {
			t.Errorf("Resolve(%+v) succeeded", o)
		}
	}
}
