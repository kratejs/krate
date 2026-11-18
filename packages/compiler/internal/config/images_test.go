package config

import (
	"net/url"
	"testing"
)

func strp(s string) *string { return &s }

func TestRemoteAllowedProtocolPortSearch(t *testing.T) {
	cfg := ImagesConfig{RemotePatterns: []ImageRemotePattern{
		{Hostname: "cdn.example.com", Protocol: "https"},
		{Hostname: "assets.local", Protocol: "http:", Port: strp("9000"), Pathname: "/img/**"},
		{Hostname: "plain.example.com", Port: strp("")},
		{Hostname: "q.example.com", Search: strp("?v=1")},
		{Hostname: "any.example.com"},
	}}
	cases := []struct {
		raw  string
		want bool
	}{
		{"https://cdn.example.com/a.png", true},
		{"http://cdn.example.com/a.png", false},
		{"https://cdn.example.com:8443/a.png", true},
		{"http://assets.local:9000/img/a/b.png", true},
		{"http://assets.local:9001/img/a.png", false},
		{"http://assets.local/img/a.png", false},
		{"https://assets.local:9000/img/a.png", false},
		{"http://assets.local:9000/other/a.png", false},
		{"https://plain.example.com/a.png", true},
		{"https://plain.example.com:444/a.png", false},
		{"https://q.example.com/a.png?v=1", true},
		{"https://q.example.com/a.png?v=2", false},
		{"https://q.example.com/a.png", false},
		{"http://any.example.com:1234/a.png?x=1", true},
		{"https://other.example.com/a.png", false},
	}
	for _, c := range cases {
		u, err := url.Parse(c.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.RemoteAllowed(u); got != c.want {
			t.Errorf("RemoteAllowed(%s) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestLoadRemotePatternsKeepProtocolAndPort(t *testing.T) {
	root := writeConfig(t, `export default { images: { remotePatterns: [{ protocol: "http", hostname: "h.example.com", port: "3000", search: "" }] } };`)
	Warnings = nil
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Images.RemotePatterns[0]
	if p.Protocol != "http" || p.Port == nil || *p.Port != "3000" || p.Search == nil || *p.Search != "" {
		t.Fatalf("pattern = %+v", p)
	}
}
