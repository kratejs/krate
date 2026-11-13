package reactive

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func sig(signals []irtree.SignalDecl, effects, memos, extra []string) irtree.ComponentSignature {
	return irtree.ComponentSignature{
		ComponentID: "test",
		Signals:     signals,
		Effects:     effects,
		Memos:       memos,
		ExtraVars:   extra,
	}
}

func TestCircularDependency(t *testing.T) {
	g := Build([]irtree.ComponentSignature{sig(
		[]irtree.SignalDecl{{Name: "a", SetterName: "setA"}, {Name: "b", SetterName: "setB"}},
		[]string{
			"createEffect(() => setB(b() + a()))",
			"createEffect(() => setA(a() + b()))",
		},
		nil, nil,
	)})
	diags := g.Validate()
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "circular") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected circular dependency diagnostic, got: %+v", diags)
	}
}
