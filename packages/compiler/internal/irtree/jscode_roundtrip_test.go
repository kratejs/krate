package irtree

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// Each expression is evaluated as written and after a parse/print round trip
// in Node; the printer must preserve parentheses the grammar requires.
func TestPrinterRoundTripMatchesNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	exprs := []string{
		"(255).toString(16)",
		"(1.5).toFixed(1)",
		"(1e3).toFixed(0)",
		"(0xff).toString(2)",
		"(()=>({a:1})).call().a",
		"(()=>({a:1}).a)()",
		"(()=>({a:1})['a'])()",
		"(-2)**2",
		"(2**3)**2",
		"2**(3**2)",
		"(-x()).toFixed(2)",
		"(!f()).toString()",
		"(typeof x()).length",
		"(void 0)?.a",
		"(x() + 1).toFixed(1)",
		"(x(), 3)",
		"[1,2].map((n)=>(n, n*2))",
		"new (class { constructor(){ this.v=3 } })().v",
		"new (g().Ctor)().v",
		"(new Date(0)).getTime()",
		"(()=>1)()",
		"(function(){return 4})()",
		"(async()=>1)() instanceof Promise",
		"(a ?? b) || c",
		"(a || b) ?? c",
		"(x() > 2 ? 'a' : 'b').length",
		"(a = 3, a + 1)",
		"(-(-x()))",
		"(+x()).toFixed(2)",
		"(~x()).toString()",
		"(x(), y).toString()",
		"typeof (()=>1)",
		"!(()=>1)",
		"`${(-x()).toFixed(1)}`",
		"({a:1}).a",
		"({a:1}['a'])",
		"[...[1,2]].length",
		"((x))",
		"(1, 2) + 1",
		"(await0 = 5)",
		"-(2 ** 2)",
		"(-1) ** 2 + 1",
	}
	prelude := "var x=()=>2.5,y=7,f=()=>false,g=()=>({Ctor:class{constructor(){this.v=9}}}),a=null,b=0,c=4,await0=0;\n"
	for _, src := range exprs {
		p := parser.New(lexer.New("var __r = " + src + ";").Tokenize())
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Errorf("%q: parse: %v", src, errs)
			continue
		}
		var out strings.Builder
		for _, s := range prog.Body {
			out.WriteString(renderStmtJS(s, nil))
		}
		printed := out.String()
		run := func(code string) string {
			js := prelude + code + "\nconsole.log(typeof __r, String(__r));"
			b, err := exec.Command("node", "-e", js).CombinedOutput()
			if err != nil {
				return "ERR " + firstLine(string(b))
			}
			return strings.TrimSpace(string(b))
		}
		want := run("var __r = " + src + ";")
		got := run(printed)
		if strings.HasPrefix(want, "ERR") {
			t.Errorf("bad test expr %q: %s", src, want)
			continue
		}
		if got != want {
			t.Errorf("%q printed as %q: got %q want %q", src, printed, got, want)
		}
	}
}

func TestPrinterStatementStartObjectAndArrowBody(t *testing.T) {
	p := parser.New(lexer.New("({a:1}).a; (function(){})(); functionName(); classNames();").Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatal(errs)
	}
	var out strings.Builder
	for _, s := range prog.Body {
		out.WriteString(renderStmtJS(s, nil))
	}
	got := out.String()
	for _, want := range []string{"({a:1}.a);", "functionName();", "classNames();"} {
		if !strings.Contains(strings.ReplaceAll(got, " ", ""), want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
