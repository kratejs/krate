package build

import (
	"encoding/json"
	"html"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var formActionAttr = regexp.MustCompile(`<form action="([^"]+)"`)

// TestInlineServerActionsE2E builds an async server component whose inline
// "use server" functions close over request data and a secret, renders it in
// the real SSR sidecar, and submits the rendered forms to the real action
// sidecar. The closed-over values reach the action, never appear in the HTML
// or in the public output, and a token that is tampered with or presented to
// another action is refused.
func TestInlineServerActionsE2E(t *testing.T) {
	ensureRuntimeDist(t)
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	const secret = "sk-live-INLINE-SECRET-9137"
	t.Setenv("KRATE_TEST_API_TOKEN", secret)

	b, root := scProject(t, map[string]string{
		"src/lib/greet.ts": `export function greet(name: string) { return "hello " + name; }`,
		"src/components/Danger.tsx": `export async function Danger({ projectId }: { projectId: string }) {
  async function remove() {
    'use server';
    return { removed: projectId };
  }
  return <form action={remove}><button>Delete</button></form>;
}`,
		"src/pages/projects/[id].tsx": `import { greet } from '../../lib/greet';
import { Danger } from '../../components/Danger';
const PREFIX = "project:";
export default async function Project({ params }: any) {
  const { id } = await params;
  const apiToken = process.env.KRATE_TEST_API_TOKEN;
  const owner = { name: "ada", since: new Date(0), nick: undefined as string | undefined };
  async function rename(formData: FormData) {
    'use server';
    return { key: PREFIX + id, apiToken, owner, nickKept: "nick" in owner, sinceIsDate: owner.since instanceof Date, name: String(formData.get("name")), hi: greet("x") };
  }
  async function archive() {
    'use server';
    return { archived: id };
  }
  return <article>
    <form action={rename}><input name="name" /><button>Rename</button></form>
    <form action={archive}><button>Archive</button></form>
    <form action={async () => { 'use server'; return { signedOut: true }; }}><button>Sign out</button></form>
    <Danger projectId={id} />
  </article>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	outDir := b.Cfg.OutDir
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	base, stop := startSidecar(t, node, repoRoot, root, outDir)
	defer stop()

	frames := postSCRegions(t, base, map[string]any{
		"route": "/projects/[id]", "url": "/projects/p1", "method": "GET",
		"params":  map[string]string{"id": "p1"},
		"regions": []map[string]string{{"id": "page", "kind": "page"}},
	})
	if len(frames) != 1 || frames[0].Status != 200 {
		t.Fatalf("render frames: %+v", frames)
	}
	page := frames[0].HTML
	for _, plain := range []string{secret, "ada", "1970-01-01"} {
		if strings.Contains(page, plain) {
			t.Fatalf("closed-over value %q leaked into the HTML:\n%s", plain, page)
		}
	}
	forms := formActionAttr.FindAllStringSubmatch(page, -1)
	if len(forms) != 4 {
		t.Fatalf("want 4 form actions, got %v in\n%s", forms, page)
	}
	renameURL, archiveURL, signOutURL := html.UnescapeString(forms[0][1]), html.UnescapeString(forms[1][1]), html.UnescapeString(forms[2][1])
	if !strings.Contains(renameURL, "?__krate_args=") || !strings.Contains(archiveURL, "?__krate_args=") {
		t.Fatalf("capturing actions must carry a sealed token: %q %q", renameURL, archiveURL)
	}
	if strings.Contains(signOutURL, "?") {
		t.Fatalf("an action that captures nothing needs no token: %q", signOutURL)
	}

	srv := serveActionSidecar(t, "node", filepath.Join(root, ".krate", "actions"), false)
	post := func(target, body string) sidecarResult {
		return doAction(t, actionRequest(t, srv, target, "application/x-www-form-urlencoded", body, true))
	}

	r := post(renameURL, "name=New+name")
	if r.status != 200 {
		t.Fatalf("rename: %d %s", r.status, r.raw)
	}
	got, _ := json.Marshal(r.body["result"])
	want := `{"apiToken":"` + secret + `","hi":"hello x","key":"project:p1","name":"New name","nickKept":true,"owner":{"name":"ada","since":"1970-01-01T00:00:00.000Z"},"sinceIsDate":true}`
	if string(got) != want {
		t.Errorf("rename result:\n got %s\nwant %s", got, want)
	}
	if r := post(archiveURL, ""); r.status != 200 || !strings.Contains(r.raw, `"archived":"p1"`) {
		t.Errorf("archive: %d %s", r.status, r.raw)
	}
	removeURL := html.UnescapeString(forms[3][1])
	if r := post(removeURL, ""); r.status != 200 || !strings.Contains(r.raw, `"removed":"p1"`) {
		t.Errorf("child component action: %d %s", r.status, r.raw)
	}
	if r := post(signOutURL, ""); r.status != 200 || !strings.Contains(r.raw, `"signedOut":true`) {
		t.Errorf("sign out: %d %s", r.status, r.raw)
	}

	// Tampering: a flipped byte, a missing token and another action's token.
	u, _ := url.Parse(renameURL)
	var args []string
	_ = json.Unmarshal([]byte(u.Query().Get("__krate_args")), &args)
	token := args[0]
	flipped := token[:len(token)-3] + map[string]string{"A": "B"}[token[len(token)-3:len(token)-2]] + token[len(token)-2:]
	if flipped == token {
		flipped = token[:len(token)-3] + "A" + token[len(token)-2:]
	}
	tampered, _ := json.Marshal([]string{flipped})
	archivePath := strings.SplitN(archiveURL, "?", 2)[0]
	for name, target := range map[string]string{
		"flipped byte":     strings.SplitN(renameURL, "?", 2)[0] + "?__krate_args=" + url.QueryEscape(string(tampered)),
		"missing token":    strings.SplitN(renameURL, "?", 2)[0],
		"another action's": archivePath + "?__krate_args=" + url.QueryEscape(`["`+token+`"]`),
	} {
		if r := post(target, "name=x"); r.status != 400 || strings.Contains(r.raw, "ada") {
			t.Errorf("%s: want 400, got %d %s", name, r.status, r.raw)
		}
	}
	// A client cannot choose the values by sending its own arguments.
	forged := post(strings.SplitN(archiveURL, "?", 2)[0], "")
	if forged.status != 400 {
		t.Errorf("forged call without a token: %d %s", forged.status, forged.raw)
	}

	// Nothing the server captured is in the public output.
	_ = filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if rel, _ := filepath.Rel(outDir, path); strings.HasPrefix(filepath.ToSlash(rel), ".krate/") {
			return nil
		}
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), secret) {
			t.Errorf("%s contains the captured secret", path)
		}
		return nil
	})
}
