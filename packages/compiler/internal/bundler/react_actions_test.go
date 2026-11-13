package bundler

import (
	"strings"
	"testing"
)

func actionsContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
}

func TestLowerUseActionState(t *testing.T) {
	out := rewriteSrc(t, `import { useActionState } from "react";
export default function Page() {
  const [count, formAction, isPending] = useActionState(increment, 0, "/count");
  return <form action={formAction}><p>{count} {isPending ? "..." : ""}</p></form>;
}`)
	actionsContain(t, out,
		`[count, __krate_as_set_count] = createSignal(0)`,
		`[isPending, __krate_as_setp_count] = createSignal(false)`,
		`formAction = __krateActionState(increment, () => count(), __krate_as_set_count, __krate_as_setp_count, 0, "/count")`,
		`{count()}`,
		`isPending() ?`,
	)
	if strings.Contains(out, "@krate/runtime/react") {
		t.Fatalf("lowered hook must not import the compat runtime:\n%s", out)
	}
}
