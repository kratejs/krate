package build

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGoHost answers /__krate/gorender like the generated host: it renders
// from the forwarded request context and reports what it read (Vary).
type fakeGoHost struct {
	srv     *httptest.Server
	mu      sync.Mutex
	renders int
}

func newFakeGoHost(t *testing.T, render func(name string, req map[string]any) (html string, vary map[string]any)) *fakeGoHost {
	t.Helper()
	h := &fakeGoHost{}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Regions []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Request string `json:"request"`
			} `json:"regions"`
		}
		_ = json.Unmarshal(body, &in)
		for _, reg := range in.Regions {
			var ctx map[string]any
			_ = json.Unmarshal([]byte(reg.Request), &ctx)
			h.mu.Lock()
			h.renders++
			h.mu.Unlock()
			html, vary := render(reg.Name, ctx)
			frame := map[string]any{"type": "region", "id": reg.ID, "status": 200, "html": html}
			if vary != nil {
				frame["vary"] = vary
			}
			_ = json.NewEncoder(w).Encode(frame)
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *fakeGoHost) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.renders
}

func newVarySupervisor(h *fakeGoHost) *goAPISupervisor {
	return &goAPISupervisor{
		client:      &http.Client{Timeout: 10 * time.Second},
		renderURL:   h.srv.URL,
		regionCache: map[string]regionCacheEntry{},
	}
}

func reqCtx(headers map[string]string, query map[string]string) string {
	b, _ := json.Marshal(map[string]any{"method": "GET", "url": "/p", "params": map[string]string{"id": "7"}, "query": query, "headers": headers})
	return string(b)
}

func cookieOf(ctx map[string]any, name string) string {
	hdrs, _ := ctx["headers"].(map[string]any)
	raw, _ := hdrs["Cookie"].(string)
	for _, part := range strings.Split(raw, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == name {
			return kv[1]
		}
	}
	return ""
}

func renderOne(t *testing.T, g *goAPISupervisor, name, request string) string {
	t.Helper()
	out, err := g.Render([]goRenderRegion{{ID: "r1", Name: name, Props: `{}`, Request: request, Revalidate: 60}})
	if err != nil {
		t.Fatal(err)
	}
	return out["r1"]
}

// Two visitors with different cookies must get their own render through the
// cache; the same visitor hits; a cookie or header the component never reads
// does not split the entries.
func TestGoRegionCacheKeepsUsersApart(t *testing.T) {
	host := newFakeGoHost(t, func(name string, ctx map[string]any) (string, map[string]any) {
		return "<p>hello " + cookieOf(ctx, "sid") + "</p>", map[string]any{"cookies": []string{"sid"}}
	})
	g := newVarySupervisor(host)
	alice := reqCtx(map[string]string{"Cookie": "sid=alice", "User-Agent": "ua-1"}, nil)
	bob := reqCtx(map[string]string{"Cookie": "sid=bob", "User-Agent": "ua-1"}, nil)

	if got := renderOne(t, g, "Hello", alice); got != "<p>hello alice</p>" {
		t.Fatalf("alice first render = %q", got)
	}
	if got := renderOne(t, g, "Hello", bob); got != "<p>hello bob</p>" {
		t.Fatalf("bob must not be served alice's render, got %q", got)
	}
	before := host.count()
	if got := renderOne(t, g, "Hello", alice); got != "<p>hello alice</p>" {
		t.Fatalf("alice second request = %q", got)
	}
	if got := renderOne(t, g, "Hello", bob); got != "<p>hello bob</p>" {
		t.Fatalf("bob second request = %q", got)
	}
	if host.count() != before {
		t.Errorf("both repeat requests should have been cache hits, host renders went %d -> %d", before, host.count())
	}
	// Unrelated headers, cookies and query keys share the entry.
	noisy := reqCtx(map[string]string{"Cookie": "sid=alice; ads=" + fmt.Sprint(time.Now().UnixNano()), "User-Agent": "ua-2", "Referer": "/x", "X-Forwarded-For": "9.9.9.9"}, map[string]string{"utm": "z"})
	if got := renderOne(t, g, "Hello", noisy); got != "<p>hello alice</p>" {
		t.Fatalf("noisy alice = %q", got)
	}
	if host.count() != before {
		t.Errorf("headers and cookies the component does not read must not split the cache (renders %d -> %d)", before, host.count())
	}
	// A visitor with no cookie is not alice either.
	if got := renderOne(t, g, "Hello", reqCtx(nil, nil)); got != "<p>hello </p>" {
		t.Fatalf("anonymous = %q", got)
	}
}

// A component that reads a header, an Authorization token or a query key must
// vary on exactly that.
func TestGoRegionCacheVariesOnWhatWasRead(t *testing.T) {
	host := newFakeGoHost(t, func(name string, ctx map[string]any) (string, map[string]any) {
		hdrs, _ := ctx["headers"].(map[string]any)
		q, _ := ctx["query"].(map[string]any)
		auth, _ := hdrs["Authorization"].(string)
		page, _ := q["page"].(string)
		return "<p>" + auth + "|" + page + "</p>", map[string]any{"headers": []string{"authorization"}, "query": []string{"page"}}
	})
	g := newVarySupervisor(host)
	a := reqCtx(map[string]string{"Authorization": "Bearer A"}, map[string]string{"page": "1", "junk": "1"})
	if got := renderOne(t, g, "Feed", a); got != "<p>Bearer A|1</p>" {
		t.Fatal(got)
	}
	if got := renderOne(t, g, "Feed", reqCtx(map[string]string{"Authorization": "Bearer B"}, map[string]string{"page": "1"})); got != "<p>Bearer B|1</p>" {
		t.Errorf("another token must not get the first render: %q", got)
	}
	if got := renderOne(t, g, "Feed", reqCtx(map[string]string{"Authorization": "Bearer A"}, map[string]string{"page": "2"})); got != "<p>Bearer A|2</p>" {
		t.Errorf("another page must not get the first render: %q", got)
	}
	n := host.count()
	if got := renderOne(t, g, "Feed", reqCtx(map[string]string{"Authorization": "Bearer A"}, map[string]string{"page": "1", "other": "x"})); got != "<p>Bearer A|1</p>" || host.count() != n {
		t.Errorf("same token and page with other noise should hit: %q (renders %d -> %d)", got, n, host.count())
	}
}

// A component that cannot show what it reads (its fields, or a host that
// predates recording) is keyed on the whole request, so even a header such as
// X-Forwarded-For splits it.
func TestGoRegionCacheKeysUnrecordedComponentsOnTheWholeRequest(t *testing.T) {
	for name, vary := range map[string]map[string]any{"full": {"full": true}, "no record": nil} {
		t.Run(name, func(t *testing.T) {
			host := newFakeGoHost(t, func(string, map[string]any) (string, map[string]any) {
				return "<p>x</p>", vary
			})
			g := newVarySupervisor(host)
			one := reqCtx(map[string]string{"X-Forwarded-For": "1.1.1.1"}, nil)
			two := reqCtx(map[string]string{"X-Forwarded-For": "2.2.2.2"}, nil)
			renderOne(t, g, "Raw", one)
			renderOne(t, g, "Raw", one)
			if host.count() != 1 {
				t.Errorf("an identical request should hit, host renders = %d", host.count())
			}
			renderOne(t, g, "Raw", two)
			if host.count() != 2 {
				t.Errorf("a different client must render again, host renders = %d", host.count())
			}
		})
	}
}

// What a component reads can depend on its inputs: a read learned later widens
// the key, so no earlier entry answers a request it does not match.
func TestGoRegionCacheLearnsConditionalReads(t *testing.T) {
	host := newFakeGoHost(t, func(name string, ctx map[string]any) (string, map[string]any) {
		q, _ := ctx["query"].(map[string]any)
		if q["mode"] == "admin" {
			return "<p>admin " + cookieOf(ctx, "tok") + "</p>", map[string]any{"query": []string{"mode"}, "cookies": []string{"tok"}}
		}
		return "<p>public</p>", map[string]any{"query": []string{"mode"}}
	})
	g := newVarySupervisor(host)
	public := reqCtx(nil, map[string]string{"mode": "user"})
	if got := renderOne(t, g, "Panel", public); got != "<p>public</p>" {
		t.Fatal(got)
	}
	admin := func(tok string) string {
		return renderOne(t, g, "Panel", reqCtx(map[string]string{"Cookie": "tok=" + tok}, map[string]string{"mode": "admin"}))
	}
	if got := admin("T1"); got != "<p>admin T1</p>" {
		t.Fatal(got)
	}
	if got := admin("T2"); got != "<p>admin T2</p>" {
		t.Errorf("a second admin token was served the first one's render: %q", got)
	}
	if got := admin("T1"); got != "<p>admin T1</p>" {
		t.Errorf("repeat of the first admin: %q", got)
	}
	if got := renderOne(t, g, "Panel", public); got != "<p>public</p>" {
		t.Errorf("public after learning: %q", got)
	}
}

// One client minting request variants of an unrecorded component cannot push
// out the entry that is being hit.
func TestGoRegionCacheEvictionKeepsHitEntries(t *testing.T) {
	host := newFakeGoHost(t, func(string, map[string]any) (string, map[string]any) {
		return "<p>x</p>", map[string]any{"full": true}
	})
	g := newVarySupervisor(host)
	hot := reqCtx(map[string]string{"X-Id": "hot"}, nil)
	renderOne(t, g, "Raw", hot)
	renderOne(t, g, "Raw", hot) // a hit: the entry is now protected
	for i := 0; i < goRegionCacheMax+200; i++ {
		renderOne(t, g, "Raw", reqCtx(map[string]string{"X-Id": fmt.Sprint("v", i)}, nil))
	}
	g.mu.Lock()
	n := len(g.regionCache)
	g.mu.Unlock()
	if n > goRegionCacheMax {
		t.Errorf("cache holds %d entries, bound is %d", n, goRegionCacheMax)
	}
	before := host.count()
	renderOne(t, g, "Raw", hot)
	if host.count() != before {
		t.Error("the entry that was hit twice was evicted by one-off variants")
	}
}
