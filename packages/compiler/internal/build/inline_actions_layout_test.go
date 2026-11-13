package build

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"path/filepath"
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
