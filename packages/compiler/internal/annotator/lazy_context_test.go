package annotator

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func lowerContextSrc(t *testing.T, src string) string {
	t.Helper()
	prog := parseProg(t, src)
	ann := Annotate(prog, &config.Config{}, "src/pages/index.tsx", src)
	var sb strings.Builder
	for _, name := range []string{"Row", "Page"} {
		if fn := ann.Functions[name]; fn != nil {
			sb.WriteString(astprint.Print(&ast.Program{Body: []ast.Stmt{fn}}))
		}
	}
	return sb.String()
}

func TestLowerContextLocalsMakesReadsLive(t *testing.T) {
	t.Parallel()
	out := lowerContextSrc(t, `
const DensityCtx = createContext("compact");
function Row() {
  const density = DensityCtx.useContext();
  return <div class={"cx-" + density} title={"d=" + density}>{density === "wide" ? "W" : "C"}</div>;
}
export default function Page() { return <Row />; }
`)
	for _, want := range []string{
		"density = krateDerived(() => DensityCtx.useContext())",
		`"cx-" + density()`,
		`"d=" + density()`,
		`density() === "wide"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLowerContextLocalsDestructuredAndObject(t *testing.T) {
	t.Parallel()
	out := lowerContextSrc(t, `
const Ctx = createContext({ count: () => 0, setCount: (n) => {} });
function Row() {
  const { count, setCount } = Ctx.useContext();
  const store = Ctx.use();
  return <button onClick={() => setCount(count() + store.step)}>{count()}</button>;
}
export default function Page() { return <Row />; }
`)
	for _, want := range []string{
		"__krdctx1 = krateDerived(() => Ctx.useContext())",
		"count = krateDerived(",
		"setCount = krateDerived(",
		"store = krateDerived(() => Ctx.use())",
		"setCount()(count()() + store().step)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLowerContextLocalsLeavesShadowedAndScopes(t *testing.T) {
	t.Parallel()
	out := lowerContextSrc(t, `
const Ctx = createContext("a");
const ScopeCtx = createScope();
function Row() {
  const v = Ctx.useContext();
  const s = ScopeCtx.useContext();
  return <ul>{[1, 2].map((v) => <li>{v}</li>)}{s}</ul>;
}
export default function Page() { return <Row />; }
`)
	if strings.Contains(out, "krateDerived") {
		t.Errorf("a name rebound in an inner scope and a scope handle must stay plain:\n%s", out)
	}
}
