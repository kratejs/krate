package hostts

import (
	"path/filepath"
	"strings"
	"testing"
)

const inlinePage = `import { db } from "@krate/app/db";
import { useThing } from "../lib/ui";

@host
export async function addTask(title: string) {
  return db.insert({ title });
}

export default function Tasks() {
  const t = useThing();
  return <button onClick={() => addTask("Buy milk")}>{t}</button>;
}
`

func TestInlineSplit(t *testing.T) {
	root, res, ds := buildProject(t, map[string]string{
		"src/lib/ui.ts":       "export const useThing = () => 'x';\n",
		"src/pages/index.tsx": inlinePage,
	}, nil)
	mustNoErrors(t, ds)
	if got := strings.Join(ids(res), ","); got != "src/pages/index#addTask" {
		t.Fatalf("registered %s", got)
	}
	ui := res.Overlays[filepath.Join(root, "src/pages/index.tsx")]
	t.Logf("UI:\n%s", ui)
	if strings.Contains(ui, "db.insert") || strings.Contains(ui, "@krate/app/db") {
		t.Fatalf("host code or its import left in the UI:\n%s", ui)
	}
	if !strings.Contains(ui, `export const addTask = (...a) => __krateCall("src/pages/index#addTask", a);`) ||
		!strings.Contains(ui, `import { call as __krateCall } from "@krate/app";`) ||
		!strings.Contains(ui, `import { useThing } from "../lib/ui";`) {
		t.Fatalf("UI:\n%s", ui)
	}
	if strings.Count(ui, "\n") != strings.Count(inlinePage, "\n")+1 {
		t.Errorf("line count changed: %d vs %d", strings.Count(ui, "\n"), strings.Count(inlinePage, "\n"))
	}
	js := read(t, res.HostBundle)
	if !strings.Contains(js, "insert") {
		t.Fatalf("host bundle lacks the function:\n%s", js)
	}
}
