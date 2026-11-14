package build

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestInlineActionInAsyncLayout is the Auth.js nav pattern: an async layout
// loads the session and defines an inline sign-out action that closes over it.
// The layout renders per request, so the value is sealed there; the form posts
// to the running server and the action gets the session id back.
func TestInlineActionInAsyncLayout(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/lib/auth.ts": `export async function getSession() { return { id: "sess-4711-SECRET", name: "ada" }; }`,
		"src/pages/app/_layout.tsx": `import { getSession } from "../../lib/auth";
export default async function Layout({ children }: any) {
  const session = await getSession();
  async function signOutAction() {
    "use server";
    return { signedOut: session.id };
  }
  return <div><nav>hello {session.name}<form action={signOutAction}><button>out</button></form></nav>{children}</div>;
}`,
		"src/pages/app/index.tsx": `export default function Page() { return <main>app home</main>; }`,
	})
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, page := getBody(t, base+"/app/")
	if status != 200 || !strings.Contains(page, "hello ada") {
		t.Fatalf("layout did not render: %d\n%s", status, page)
	}
	if strings.Contains(page, "sess-4711") {
		t.Fatalf("the captured session id leaked into the HTML:\n%s", page)
	}
	m := formActionAttr.FindStringSubmatch(page)
	if m == nil || !strings.Contains(m[1], "?__krate_args=") || !strings.Contains(m[1], "k1.") {
		t.Fatalf("want a form with a sealed token, got %v in\n%s", m, page)
	}
	target := html.UnescapeString(m[1])
	post := func(u string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+u, strings.NewReader("x=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Krate-Action", "fetch")
		req.Header.Set("Accept", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}
	code, body := post(target)
	var env struct {
		Result struct {
			SignedOut string `json:"signedOut"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(body), &env)
	if code != 200 || env.Result.SignedOut != "sess-4711-SECRET" {
		t.Fatalf("the layout action did not get its captured value: %d %s", code, body)
	}
	if code, _ := post(strings.SplitN(target, "?", 2)[0]); code != 400 {
		t.Errorf("a post without the sealed token must be refused, got %d", code)
	}
}

// TestInlineActionNextCaptureShapes covers what Next apps write: destructured,
// awaited, template and searchParams locals captured by a declaration, an
// arrow and an action declared inside a list callback of an async page. Each
// posted action gets its values back from the sealed token.
func TestInlineActionNextCaptureShapes(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/lib/db.ts": `export const db = { find: async (id: string) => ({ id, version: 7, at: new Date(0), tags: ["a", null] }) };`,
		"src/pages/items/[id].tsx": `import { db } from "../../lib/db";
export default async function Page({ params, searchParams }: any) {
  const { id } = await params;
  const { q } = await searchParams;
  const item = await db.find(id);
  const label = ` + "`Item ${id}`" + `;
  async function save(formData: FormData) {
    'use server';
    return { saved: [id, formData.get("name"), label, item.version, item.at, item.tags, q] };
  }
  const remove = async () => { 'use server'; return { removed: item.id }; };
  return <main>
    <form action={save}><input name="name" /><button>Save</button><button formAction={remove}>Delete</button></form>
    <ul>{[1, 2].map((n) => <li key={n}><form action={async () => { 'use server'; return { row: [n, id] }; }}><button>row</button></form></li>)}</ul>
  </main>;
}`,
	})
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, page := getBody(t, base+"/items/p1/?q=hi")
	if status != 200 {
		t.Fatalf("page: %d\n%s", status, page)
	}
	urls := regexp.MustCompile(`(?:action|formaction)="([^"]*__krate_args=[^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(urls) != 4 {
		t.Fatalf("want 4 sealed actions, got %d in\n%s", len(urls), page)
	}
	want := []string{
		`"saved":["p1","Bo","Item p1",7,"1970-01-01T00:00:00.000Z",["a",null],"hi"]`,
		`"removed":"p1"`,
		`"row":[1,"p1"]`,
		`"row":[2,"p1"]`,
	}
	for i, m := range urls {
		req, _ := http.NewRequest(http.MethodPost, base+html.UnescapeString(m[1]), strings.NewReader("name=Bo"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Krate-Action", "fetch")
		req.Header.Set("Accept", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(string(body), want[i]) {
			t.Errorf("action %d: %d %s\nwant %s", i, res.StatusCode, body, want[i])
		}
	}
}
