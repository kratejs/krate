package bundler

import (
	"strings"
	"testing"
)

// TestRewriteReactInlineGetterHooks: a useSyncExternalStore or useMemo called
// inline in a component's JSX lowers like a declared one, through a hoisted
// binding read as a getter. Calls in a conditional branch, after &&, or in a
// nested function stay where they are.
func TestRewriteReactInlineGetterHooks(t *testing.T) {
	out := rewriteSrc(t, `
		import { useSyncExternalStore, useMemo } from 'react';
		import * as React from 'react';
		const sub = (cb) => () => {};
		export default function Reader({ id }) {
			const v = useSyncExternalStore(sub, () => 'b', () => 'a');
			const onClick = () => useMemo(() => 0, []);
			return <span id={id} data-v={v} title={React.useSyncExternalStore(sub, () => 't', () => 'u')} onClick={onClick}>
				{useSyncExternalStore(sub, () => 'b', () => 'a')}
				{useMemo(() => id + '!', [id])}
				{id && useMemo(() => 1, [])}
				{id ? useMemo(() => 2, []) : null}
			</span>;
		}
	`)
	for _, want := range []string{
		"const v = createExternalStore(sub,",
		"const __krate_hook0 = createExternalStore(sub, () => \"t\", () => \"u\")",
		"const __krate_hook1 = createExternalStore(sub, () => \"b\", () => \"a\")",
		"const __krate_hook2 = createMemoDeps(",
		"title={__krate_hook0()}",
		"{__krate_hook1()}",
		"{__krate_hook2()}",
		"{id && createMemoDeps(() => 1,",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "__krate_hook3") || strings.Contains(out, "useSyncExternalStore(") {
		t.Errorf("a conditional or nested call was hoisted, or a call was left unlowered:\n%s", out)
	}
}
