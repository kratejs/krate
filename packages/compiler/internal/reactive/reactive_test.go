package reactive

import (
	"strings"
	"testing"

	"krate-compiler/internal/irtree"
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

func TestUnusedSignal(t *testing.T) {
	g := Build([]irtree.ComponentSignature{sig(
		[]irtree.SignalDecl{{Name: "used", SetterName: "setUsed"}, {Name: "dead", SetterName: "setDead"}},
		[]string{"createEffect(() => console.log(used()))"},
		nil, nil,
	)})
	diags := g.Validate()
	found := false
	for _, d := range diags {
		if d.Severity == "warning" && strings.Contains(d.Message, `"dead"`) {
			found = true
		}
		if strings.Contains(d.Message, `"used"`) {
			t.Errorf("expected 'used' signal to be considered live, got: %s", d.Message)
		}
	}
	if !found {
		t.Errorf("expected an unused-signal diagnostic for 'dead', got: %+v", diags)
	}
}

func TestWriteOnlyEffect(t *testing.T) {
	// An effect that reads signal a and writes signal b has no cycle.
	g := Build([]irtree.ComponentSignature{sig(
		[]irtree.SignalDecl{{Name: "a", SetterName: "setA"}, {Name: "b", SetterName: "setB"}},
		[]string{"createEffect(() => setB(a() + 1))"},
		nil, nil,
	)})
	diags := g.Validate()
	for _, d := range diags {
		if d.Severity == "warning" && strings.Contains(d.Message, "circular") {
			t.Errorf("expected no circular dependency, got: %s", d.Message)
		}
		if strings.Contains(d.Message, "reads no signals") {
			t.Errorf("effect reads a signal; unexpected diagnostic: %s", d.Message)
		}
	}
}

func TestSelfReferentialEffectIsCircular(t *testing.T) {
	// An effect that reads AND writes the same signal is a feedback loop.
	g := Build([]irtree.ComponentSignature{sig(
		[]irtree.SignalDecl{{Name: "count", SetterName: "setCount"}},
		[]string{"createEffect(() => setCount(count() + 1))"},
		nil, nil,
	)})
	diags := g.Validate()
	for _, d := range diags {
		if d.Severity == "warning" && strings.Contains(d.Message, "circular") {
			return
		}
	}
	t.Errorf("expected circular dependency for self-referential effect, got: %+v", diags)
}

func TestWriteOnlyEffectWithoutRead(t *testing.T) {
	// A setInterval callback that writes a signal is a legitimate one-shot
	// imperative effect — the write is deferred, so it must NOT warn.
	g := Build([]irtree.ComponentSignature{sig(
		[]irtree.SignalDecl{{Name: "tick", SetterName: "setTick"}},
		[]string{"createEffect(() => { const id = setInterval(() => setTick(Date.now()), 1000); onCleanup(() => clearInterval(id)); })"},
		nil, nil,
	)})
	diags := g.Validate()
	for _, d := range diags {
		if strings.Contains(d.Message, "reads no signals") || strings.Contains(d.Message, "writes no signals") {
			t.Errorf("deferred callback writes are not a write-only bug: %s", d.Message)
		}
	}

	// A bare synchronous `createEffect(() => setTick(5))` IS the smell.
	g2 := Build([]irtree.ComponentSignature{sig(
		[]irtree.SignalDecl{{Name: "tick", SetterName: "setTick"}},
		[]string{"createEffect(() => setTick(5))"},
		nil, nil,
	)})
	diags2 := g2.Validate()
	found := false
	for _, d := range diags2 {
		if strings.Contains(d.Message, "calls setters but reads no signals") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected write-only diagnostic for bare setter effect, got: %+v", diags2)
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

func TestNoReadEffectWarns(t *testing.T) {
	g := Build([]irtree.ComponentSignature{sig(
		[]irtree.SignalDecl{{Name: "x", SetterName: "setX"}},
		[]string{"createEffect(() => console.log('static only'))"},
		nil, nil,
	)})
	diags := g.Validate()
	for _, d := range diags {
		if d.Severity == "warning" && strings.Contains(d.Message, "reads no signals") {
			return
		}
	}
	t.Errorf("expected no-read effect warning, got: %+v", diags)
}

func TestHandlerAndBindingMarkSignalsUsed(t *testing.T) {
	s := sig(
		[]irtree.SignalDecl{{Name: "count", SetterName: "setCount"}},
		nil, nil, nil,
	)
	s.Handlers = []irtree.HandlerDecl{{Event: "click", Signals: []string{"count"}}}
	s.SlotBindings = []irtree.SlotBinding{{Type: "text", ExprJS: "String(count())", Signals: []string{"count"}}}
	g := Build([]irtree.ComponentSignature{s})
	diags := g.Validate()
	for _, d := range diags {
		if strings.Contains(d.Message, `"count"`) {
			t.Errorf("signal 'count' is used by handler/binding; unexpected diagnostic: %s", d.Message)
		}
	}
}
