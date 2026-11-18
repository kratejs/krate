package routesource

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSourceMatch(t *testing.T) {
	type vals = map[string]string
	tests := []struct {
		name   string
		kind   Kind
		source string
		path   string
		want   vals // nil means no match
	}{
		{"literal", KindRedirect, "/old", "/old", vals{}},
		{"literal does not match trailing slash", KindRedirect, "/old", "/old/", nil},
		{"literal miss", KindRedirect, "/old", "/older", nil},
		{"legacy splat", KindRedirect, "/legacy/*", "/legacy/a/b", vals{"splat": "a/b"}},
		{"legacy splat bare", KindRedirect, "/legacy/*", "/legacy", vals{"splat": ""}},
		{"legacy splat miss", KindRedirect, "/legacy/*", "/other/a", nil},
		{"named", KindRedirect, "/u/:name", "/u/ann", vals{"name": "ann"}},
		{"named is one segment", KindRedirect, "/u/:name", "/u/ann/x", nil},
		{"named needs a value", KindRedirect, "/u/:name", "/u", nil},
		{"named tolerates a trailing slash", KindRedirect, "/u/:name", "/u/ann/", vals{"name": "ann"}},
		{"star", KindRedirect, "/docs/:path*", "/docs/a/b/c", vals{"path": "a/b/c"}},
		{"star empty", KindRedirect, "/docs/:path*", "/docs", vals{"path": ""}},
		{"plus", KindRedirect, "/docs/:path+", "/docs/a/b", vals{"path": "a/b"}},
		{"plus needs a value", KindRedirect, "/docs/:path+", "/docs", nil},
		{"optional present", KindRedirect, "/p/:id?", "/p/7", vals{"id": "7"}},
		{"optional absent", KindRedirect, "/p/:id?", "/p", vals{"id": ""}},
		{"root star", KindRedirect, "/:path*", "/", vals{"path": ""}},
		{"root star deep", KindRedirect, "/:path*", "/a/b", vals{"path": "a/b"}},
		{"custom regex", KindRedirect, "/files/:file(\\d+)", "/files/42", vals{"file": "42"}},
		{"custom regex miss", KindRedirect, "/files/:file(\\d+)", "/files/abc", nil},
		{"custom regex multi segment", KindRedirect, "/files/:rest(.*)", "/files/a/b", vals{"rest": "a/b"}},
		{"unnamed pattern", KindRedirect, "/x/(\\d+)", "/x/9", vals{}},
		{"unnamed pattern miss", KindRedirect, "/x/(\\d+)", "/x/z", nil},
		{"lookahead accepts", KindRedirect, "/store/:slug((?!compare$)[^/]+)", "/store/foo", vals{"slug": "foo"}},
		{"lookahead rejects", KindRedirect, "/store/:slug((?!compare$)[^/]+)", "/store/compare", nil},
		{"lookahead rejects with trailing slash", KindRedirect, "/store/:slug((?!compare$)[^/]+)", "/store/compare/", nil},
		{"lookahead keeps prefixes", KindRedirect, "/store/:slug((?!compare$)[^/]+)", "/store/compare-all", vals{"slug": "compare-all"}},
		{"lookahead alternatives", KindRedirect, "/s/:slug((?!a$|b$)[^/]+)", "/s/b", nil},
		{"lookahead alternatives pass", KindRedirect, "/s/:slug((?!a$|b$)[^/]+)", "/s/c", vals{"slug": "c"}},
		{"lookahead prefix form", KindRedirect, "/s/:slug((?!api)[^/]+)", "/s/apiary", nil},
		{"two lookaheads", KindRedirect, "/s/:slug((?!a$)(?!b$)[^/]+)", "/s/b", nil},
		{"extension prefix", KindRedirect, "/img/:name.:ext", "/img/cat.png", vals{"name": "cat", "ext": "png"}},
		{"suffix after param", KindRedirect, "/a/:id-x", "/a/7-x", vals{"id": "7"}},
		{"brace optional group", KindRedirect, "/blog{/:slug}?", "/blog/hello", vals{"slug": "hello"}},
		{"brace optional group absent", KindRedirect, "/blog{/:slug}?", "/blog", vals{"slug": ""}},
		{"escaped colon", KindRedirect, `/a\:b`, "/a:b", vals{}},
		{"splat after params", KindRedirect, "/v/:ver/*", "/v/2/x/y", vals{"ver": "2", "splat": "x/y"}},
		{"header exact", KindHeader, "/a", "/a", vals{}},
		{"header exact miss", KindHeader, "/a", "/a/b", nil},
		{"header prefix", KindHeader, "/assets/*", "/assets/x.js", vals{}},
		{"header prefix no slash", KindHeader, "/assets*", "/assetsX", vals{}},
		{"header glob", KindHeader, "/a/*.js", "/a/x.js", vals{}},
		{"header glob stays one segment", KindHeader, "/a/*.js", "/a/b/x.js", nil},
		{"header next all", KindHeader, "/:path*", "/anything/here", vals{"path": "anything/here"}},
		{"header next all root", KindHeader, "/:path*", "/", vals{"path": ""}},
		{"header next regex", KindHeader, "/:file(.+\\.(?:js|css))", "/x.js", vals{"file": "x.js"}},
		{"header next regex miss", KindHeader, "/:file(.+\\.(?:js|css))", "/x.png", nil},
		{"header trims spaces", KindHeader, " /a ", "/a", vals{}},
		{"header empty never matches", KindHeader, "", "/a", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Compile(tt.source, tt.kind)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tt.source, err)
			}
			got, ok := s.Match(tt.path)
			if (tt.want != nil) != ok {
				t.Fatalf("Match(%q) ok = %v, want %v (params %v)", tt.path, ok, tt.want != nil, got.Values)
			}
			if ok && !reflect.DeepEqual(got.Values, tt.want) {
				t.Fatalf("Match(%q) params = %v, want %v", tt.path, got.Values, tt.want)
			}
		})
	}
}

func TestSourceModifiers(t *testing.T) {
	s, err := Compile("/d/:rest*/x/:id", KindRedirect)
	if err == nil {
		// A repeated parameter in the middle is legal path-to-regexp; just
		// check the modifiers are reported.
		p, ok := s.Match("/d/a/b/x/9")
		if !ok || p.Mods["rest"] != '*' || p.Mods["id"] != 0 {
			t.Fatalf("mods = %v ok = %v", p.Mods, ok)
		}
	}
}

func TestSourceUnsupported(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"lookbehind", "/a/:x((?<=b)c)", "RE2"},
		{"backreference", `/a/:x(([ab])\1)`, "capturing"},
		{"lookahead not leading", "/a/:x([^/]+(?!b))", "RE2"},
		{"positive lookahead", "/a/:x((?=b)[^/]+)", "RE2"},
		{"top level lookahead", "/a/(?!b)x", "cannot start"},
		{"capturing group", "/a/:x((a)b)", "capturing"},
		{"bare star", "/a/:x/*/b", "unexpected"},
		{"missing name", "/a/:", "missing parameter name"},
		{"unbalanced", "/a/:x(abc", "unbalanced"},
		{"lookahead alone", "/a/:x((?!b))", "after it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(tt.source, KindRedirect)
			if err == nil {
				t.Fatalf("Compile(%q) succeeded, want an error", tt.source)
			}
			if !IsUnsupported(err) {
				t.Fatalf("error %T, want UnsupportedError", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func req(target string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest("GET", target, nil)
	for k, v := range hdr {
		if strings.EqualFold(k, "host") {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	return r
}

func mustConds(t *testing.T, has, missing []Condition) *Conditions {
	t.Helper()
	c, err := CompileConditions(has, missing)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConditions(t *testing.T) {
	type vals = map[string]string
	tests := []struct {
		name    string
		has     []Condition
		missing []Condition
		r       *http.Request
		want    vals // nil means no match
	}{
		{"query presence", []Condition{{Type: "query", Key: "vehicle"}}, nil, req("/store?vehicle=x", nil), vals{"vehicle": "x"}},
		{"query absent", []Condition{{Type: "query", Key: "vehicle"}}, nil, req("/store?other=1", nil), nil},
		{"query empty value is not present", []Condition{{Type: "query", Key: "vehicle"}}, nil, req("/store?vehicle=", nil), nil},
		{"query repeated uses last", []Condition{{Type: "query", Key: "v", Value: "b"}}, nil, req("/s?v=a&v=b", nil), vals{}},
		{"query regex", []Condition{{Type: "query", Key: "v", Value: "a|b"}}, nil, req("/s?v=b", nil), vals{}},
		{"query regex is anchored", []Condition{{Type: "query", Key: "v", Value: "a"}}, nil, req("/s?v=ab", nil), nil},
		{"alternation is grouped", []Condition{{Type: "query", Key: "v", Value: "a|b"}}, nil, req("/s?v=ab", nil), nil},
		{"named capture", []Condition{{Type: "query", Key: "v", Value: "(?P<kind>[a-z]+)-(?P<n>\\d+)"}}, nil, req("/s?v=car-7", nil), vals{"kind": "car", "n": "7"}},
		{"js named capture syntax", []Condition{{Type: "query", Key: "v", Value: "(?<kind>[a-z]+)"}}, nil, req("/s?v=car", nil), vals{"kind": "car"}},
		{"header presence key lowercased", []Condition{{Type: "header", Key: "X-Token"}}, nil, req("/s", map[string]string{"x-token": "abc"}), vals{"xtoken": "abc"}},
		{"header regex", []Condition{{Type: "header", Key: "x-mode", Value: "(?P<mode>on|off)"}}, nil, req("/s", map[string]string{"x-mode": "on"}), vals{"mode": "on"}},
		{"header miss", []Condition{{Type: "header", Key: "x-mode", Value: "on"}}, nil, req("/s", map[string]string{"x-mode": "off"}), nil},
		{"cookie presence", []Condition{{Type: "cookie", Key: "session"}}, nil, req("/s", map[string]string{"Cookie": "a=1; session=zz"}), vals{"session": "zz"}},
		{"cookie absent", []Condition{{Type: "cookie", Key: "session"}}, nil, req("/s", map[string]string{"Cookie": "a=1"}), nil},
		{"host regex", []Condition{{Type: "host", Value: "(?P<sub>[a-z]+)\\.example\\.com"}}, nil, req("/s", map[string]string{"Host": "App.Example.com:8080"}), vals{"sub": "app"}},
		{"host without groups captures host", []Condition{{Type: "host", Value: "example\\.com"}}, nil, req("/s", map[string]string{"Host": "example.com"}), vals{"host": "example.com"}},
		{"host mismatch", []Condition{{Type: "host", Value: "example\\.com"}}, nil, req("/s", map[string]string{"Host": "other.com"}), nil},
		{"missing blocks", nil, []Condition{{Type: "cookie", Key: "auth"}}, req("/s", map[string]string{"Cookie": "auth=1"}), nil},
		{"missing passes", nil, []Condition{{Type: "cookie", Key: "auth"}}, req("/s", nil), vals{}},
		{"missing value regex only blocks on match", nil, []Condition{{Type: "query", Key: "m", Value: "off"}}, req("/s?m=on", nil), vals{}},
		{"mixed has and missing", []Condition{{Type: "query", Key: "a"}}, []Condition{{Type: "header", Key: "x-skip"}}, req("/s?a=1", nil), vals{"a": "1"}},
		{"mixed blocked by missing", []Condition{{Type: "query", Key: "a"}}, []Condition{{Type: "header", Key: "x-skip"}}, req("/s?a=1", map[string]string{"x-skip": "1"}), nil},
		{"all has must match", []Condition{{Type: "query", Key: "a"}, {Type: "query", Key: "b"}}, nil, req("/s?a=1", nil), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustConds(t, tt.has, tt.missing)
			got, ok := c.Match(tt.r)
			if (tt.want != nil) != ok {
				t.Fatalf("ok = %v, want %v (params %v)", ok, tt.want != nil, got)
			}
			if ok && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("params = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionsNilMatchesEverything(t *testing.T) {
	var c *Conditions
	if _, ok := c.Match(req("/x", nil)); !ok {
		t.Fatal("a nil Conditions must match")
	}
	c2, err := CompileConditions(nil, nil)
	if err != nil || c2 != nil {
		t.Fatalf("CompileConditions(nil, nil) = %v, %v; want nil, nil", c2, err)
	}
}

func TestConditionErrors(t *testing.T) {
	tests := []struct {
		name string
		has  []Condition
		want string
		uns  bool
	}{
		{"bad type", []Condition{{Type: "body", Key: "a"}}, "type must be", false},
		{"missing key", []Condition{{Type: "query"}}, "needs a key", false},
		{"lookbehind", []Condition{{Type: "header", Key: "a", Value: "(?<=x)y"}}, "RE2", true},
		{"backreference", []Condition{{Type: "header", Key: "a", Value: `(a)\1`}}, "RE2", true},
		{"lookahead", []Condition{{Type: "query", Key: "a", Value: "(?!x)y"}}, "RE2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompileConditions(tt.has, nil)
			if err == nil {
				t.Fatal("want an error")
			}
			if IsUnsupported(err) != tt.uns {
				t.Fatalf("IsUnsupported = %v, want %v (%v)", IsUnsupported(err), tt.uns, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}
