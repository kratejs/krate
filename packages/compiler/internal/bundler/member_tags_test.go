package bundler

import (
	"strings"
	"testing"
)

func TestMemberTagImportsAreCapitalized(t *testing.T) {
	out := rewriteSrc(t, `
		import * as ui from './ui';
		import cards, { rows } from './parts';
		import { Row } from './row';
		export default function Page() {
			return <main><ui.Button>go</ui.Button><cards.Item /><rows.Cell /><Row /><div /></main>;
		}
	`)
	for _, want := range []string{"import * as Ui$ns from", "<Ui$ns.Button>", "</Ui$ns.Button>", "<Cards$ns.Item", "<Rows$ns.Cell"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, bad := range []string{"<ui.", "<cards.", "<rows."} {
		if strings.Contains(out, bad) {
			t.Errorf("lowercase member tag %q left in:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "<Row") || !strings.Contains(out, "<div") {
		t.Errorf("plain tags must be untouched:\n%s", out)
	}
}

func TestMemberTagOfNonImportIsLeftAlone(t *testing.T) {
	out := rewriteSrc(t, `
		export default function Page(props) {
			return <main><props.Icon /></main>;
		}
	`)
	if !strings.Contains(out, "<props.Icon") {
		t.Errorf("a member tag of a parameter must be left alone:\n%s", out)
	}
}
