package caps

import (
	"strings"
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
		"https://app.localhost/":          NavExternal,
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

func TestRemoteDecideModes(t *testing.T) {
	drop := mustPolicy(t, RemoteOptions{URL: "https://a.test/", Blocked: BlockedDrop})
	if drop.Decide("https://b.test/") != NavBlock || drop.Decide("mailto:x@y.z") != NavBlock {
		t.Fatal("block mode must drop")
	}
	any := mustPolicy(t, RemoteOptions{URL: "https://a.test/", AllowAnyHTTPS: true})
	if any.Decide("https://b.test/") != NavAllow || any.Decide("http://b.test/") != NavExternal {
		t.Fatal("allowAnyHTTPS allows https only")
	}
	if any.Decide("krate://app/") != NavBlock {
		t.Fatal("the app origin is never allowed")
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
	}
	for _, o := range bad {
		if _, err := o.Resolve(); err == nil {
			t.Errorf("Resolve(%+v) succeeded", o)
		}
	}
}

func TestPartitions(t *testing.T) {
	cases := []struct {
		in         string
		name       string
		persistent bool
		key        string
	}{
		{"", "default", true, "p:default"},
		{"persist:shop", "shop", true, "p:shop"},
		{"shop", "shop", false, "m:shop"},
		{"persist:a.b-c_d", "a.b-c_d", true, "p:a.b-c_d"},
	}
	for _, c := range cases {
		p, err := ParsePartition(c.in)
		if err != nil || p.Name != c.name || p.Persistent != c.persistent || p.Key() != c.key {
			t.Errorf("ParsePartition(%q) = %+v, %v", c.in, p, err)
		}
		if again, _ := ParsePartition(p.String()); again != p {
			t.Errorf("String round trip of %q: %+v", c.in, again)
		}
	}
	for _, bad := range []string{"persist:", "a/b", "..", ".x", "a b", strings.Repeat("a", 65), "persist:../x"} {
		if _, err := ParsePartition(bad); err == nil {
			t.Errorf("ParsePartition(%q) succeeded", bad)
		}
	}
	a, _ := ParsePartition("x")
	b, _ := ParsePartition("persist:x")
	if a.Key() == b.Key() {
		t.Fatal("memory and persistent partitions must differ")
	}
}

func TestMatchURLPattern(t *testing.T) {
	cases := []struct {
		pat, url string
		want     bool
	}{
		{"<all_urls>", "https://a.test/x", true},
		{"<all_urls>", "krate://app/x", false},
		{"https://ads.test/*", "https://ads.test/x?y=1", true},
		{"https://ads.test/*", "http://ads.test/x", false},
		{"*://ads.test/*", "http://ads.test/x", true},
		{"*://*.ads.test/*", "https://a.ads.test/x", true},
		{"*://*.ads.test/*", "https://ads.test/x", false},
		{"*://*.ads.test/*", "https://xads.test/x", false},
		{"https://a.test/exact", "https://a.test/exact", true},
		{"https://a.test/exact", "https://a.test/exact2", false},
		{"https://a.test/img/*.png", "https://a.test/img/x.png", true},
		{"https://a.test/img/*.png", "https://a.test/img/x.jpg", false},
		{"http://127.0.0.1:8080/*", "http://127.0.0.1:8080/a", true},
		{"http://127.0.0.1:8080/*", "http://127.0.0.1:8081/a", false},
		{"https://a.test", "https://a.test/", false},
		{"ftp://a.test/*", "ftp://a.test/", false},
	}
	for _, c := range cases {
		if got := MatchURLPattern(c.pat, c.url); got != c.want {
			t.Errorf("MatchURLPattern(%q, %q) = %v", c.pat, c.url, got)
		}
	}
}

func TestEvalRules(t *testing.T) {
	rules := []RequestRule{
		{URLs: []string{"https://ads.test/*"}, Action: RuleBlock},
		{URLs: []string{"https://old.test/*"}, Action: RuleRedirect, Redirect: "https://new.test/"},
		{URLs: []string{"<all_urls>"}, Action: RuleHeaders, Headers: map[string]string{"X-One": "1"}},
		{URLs: []string{"https://a.test/*"}, Action: RuleHeaders, Headers: map[string]string{"X-One": "2", "X-Two": "2"}},
	}
	if err := ValidateRules(rules); err != nil {
		t.Fatal(err)
	}
	if r := EvalRules(rules, "https://ads.test/x"); !r.Block || r.Headers["X-One"] != "1" {
		t.Fatalf("ads: %+v", r)
	}
	if r := EvalRules(rules, "https://old.test/x"); r.Redirect != "https://new.test/" || r.Block {
		t.Fatalf("old: %+v", r)
	}
	if r := EvalRules(rules, "https://a.test/x"); r.Headers["X-One"] != "2" || r.Headers["X-Two"] != "2" {
		t.Fatalf("a: %+v", r)
	}
	if r := EvalRules(rules, "krate://app/"); r.Any() {
		t.Fatalf("app origin matched: %+v", r)
	}
}

func TestRuleValidation(t *testing.T) {
	bad := []RequestRule{
		{Action: RuleBlock},
		{URLs: []string{"bogus"}, Action: RuleBlock},
		{URLs: []string{"https://a.test/*"}, Action: "allow"},
		{URLs: []string{"https://a.test/*"}, Action: RuleRedirect, Redirect: "krate://app/"},
		{URLs: []string{"https://a.test/*"}, Action: RuleHeaders},
		{URLs: []string{"https://a.test/*"}, Action: RuleHeaders, Headers: map[string]string{"Host": "x"}},
		{URLs: []string{"https://a.test/*"}, Action: RuleHeaders, Headers: map[string]string{"X-A": "a\r\nb: c"}},
		{URLs: []string{"https://a.test/*"}, Action: RuleHeaders, Headers: map[string]string{"X A": "a"}},
		{URLs: []string{"file:///*"}, Action: RuleBlock},
	}
	for i, r := range bad {
		if r.Validate() == nil {
			t.Errorf("rule %d validated", i)
		}
	}
}

func TestContentBlockerJSON(t *testing.T) {
	b, err := ContentBlockerJSON([]RequestRule{
		{URLs: []string{"*://*.ads.test/*", "https://t.test/x.js"}, Action: RuleBlock},
		{URLs: []string{"https://r.test/*"}, Action: RuleRedirect, Redirect: "https://a.test/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"^https?://[^/]*\\.ads\\.test(:[0-9]*)?/.*"`, `"^https://t\\.test(:[0-9]*)?/x\\.js$"`, `"type":"block"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if strings.Contains(s, "r.test") {
		t.Fatal("redirect rule leaked into the blocker list")
	}
	if b, _ := ContentBlockerJSON(nil); b != nil {
		t.Fatal("no rules, no list")
	}
}

func TestProxyValidate(t *testing.T) {
	ok := []ProxyConfig{{Mode: ProxySystem}, {Mode: ProxyDirect}, {Mode: ProxyFixed, URL: "http://127.0.0.1:3128"}, {Mode: ProxyFixed, URL: "socks5://h:1080"}}
	for _, c := range ok {
		if c.Validate() != nil {
			t.Errorf("%+v", c)
		}
	}
	bad := []ProxyConfig{{}, {Mode: ProxyFixed}, {Mode: ProxyFixed, URL: "ftp://x:1"}, {Mode: ProxyFixed, URL: "krate://app"}}
	for _, c := range bad {
		if c.Validate() == nil {
			t.Errorf("%+v", c)
		}
	}
}

func TestScripts(t *testing.T) {
	s := DeliverToRemoteScript("krate://app", "https://a.test", []byte(`{"x":"</script> "}`))
	if strings.Contains(s, "</script>") || !strings.Contains(s, "location.origin!==t") {
		t.Fatal(s)
	}
	if !strings.Contains(RemotePageScript("function(j){}", "tok"), "krateRemote") {
		t.Fatal("bridge script")
	}
}

func TestParseRemoteMessage(t *testing.T) {
	tok := NewRemoteToken()
	if len(tok) != 32 || tok == NewRemoteToken() {
		t.Fatal("token")
	}
	if d, ok := ParseRemoteMessage(tok+"\n"+`{"a":1}`, tok); !ok || string(d) != `{"a":1}` {
		t.Fatal("valid message refused")
	}
	for _, bad := range []string{`{"a":1}`, "wrong\n{}", tok + "\n{", tok + "\n" + strings.Repeat("1", MaxRemoteMessage+1), ""} {
		if _, ok := ParseRemoteMessage(bad, tok); ok {
			t.Errorf("accepted %.20q", bad)
		}
	}
}
