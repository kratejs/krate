package renderer

import (
	"strings"
	"testing"
)

func ids(v ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		m[s] = true
	}
	return m
}

// Two instances that differ only in their ids and folded data compile to
// the same template, and their arguments carry the differences.
func TestTemplateScopeAbstractsInstanceValues(t *testing.T) {
	a := `var tags=['install','scaffold'];
var props=__krate_props["A.Nav_c0"];
const [open,setOpen]=createSignal(false);
kbindHandler("I","onClick",()=>setOpen(!open()));
kbindAttr("I","aria-label",()=>(open()?"Close":'Open'));
kbindText("J",()=>tags.join(','));
`
	b := strings.NewReplacer(`['install','scaffold']`, `['wasm']`, `"A.Nav_c0"`, `"Q.Nav_c3"`, `"I"`, `"7"`, `"J"`, `"8"`).Replace(a)
	pageIDs := ids("A.Nav_c0", "Q.Nav_c3", "I", "J", "7", "8")
	ta, argsA, ok := templateScope(a, nil, pageIDs)
	if !ok {
		t.Fatal("scope a was rejected")
	}
	tb, argsB, ok := templateScope(b, nil, pageIDs)
	if !ok {
		t.Fatal("scope b was rejected")
	}
	if ta != tb {
		t.Fatalf("templates differ:\n%s\n---\n%s", ta, tb)
	}
	for _, want := range []string{`"onClick"`, `"aria-label"`, `"Close"`, `'Open'`, `createSignal(false)`} {
		if !strings.Contains(ta, want) {
			t.Errorf("template lost the code constant %s:\n%s", want, ta)
		}
	}
	if got := strings.Join(argsA, " "); got != `['install','scaffold'] "A.Nav_c0" "I" "J"` {
		t.Errorf("args of a = %s", got)
	}
	if got := strings.Join(argsB, " "); got != `['wasm'] "Q.Nav_c3" "7" "8"` {
		t.Errorf("args of b = %s", got)
	}
}

// Literals are hoisted only where they run once per scope: not inside a
// function, a loop or a nested assignment.
func TestTemplateScopeHoistsOnlyOncePerRunLiterals(t *testing.T) {
	body := `var once={items:[1,2],label:'x'};
var f=()=>[1,2];
function g(){var inner=[3];return inner;}
for(var i=0;i<2;i++){var loop=[4];}
var nested=a=[5];
var withRef=[x,1];
var mixed={a:[6],b:handler};
g(f(),loop,nested,withRef,mixed,once);
`
	tpl, args, ok := templateScope(body, nil, nil)
	if !ok {
		t.Fatal("rejected")
	}
	if strings.Join(args, " ") != `{items:[1,2],label:'x'} [6]` {
		t.Errorf("args = %q\n%s", args, tpl)
	}
	for _, kept := range []string{"()=>[1,2]", "var inner=[3]", "var loop=[4]", "a=[5]", "[x,1]", "b:handler"} {
		if !strings.Contains(tpl, kept) {
			t.Errorf("template should keep %s inline:\n%s", kept, tpl)
		}
	}
}

// Equal object literals stay distinct objects: each gets its own argument.
func TestTemplateScopeKeepsObjectIdentity(t *testing.T) {
	body := "var a={current:null};\nvar b={current:null};\nuse(a,b);\n"
	tpl, args, ok := templateScope(body, nil, nil)
	if !ok || len(args) != 2 || !strings.Contains(tpl, SharedScopeArg+"[0]") || !strings.Contains(tpl, SharedScopeArg+"[1]") {
		t.Errorf("want two arguments, got %v:\n%s", args, tpl)
	}
}

// A declaration of data nothing reads is dropped rather than passed.
func TestTemplateScopeDropsUnreadData(t *testing.T) {
	tpl, args, ok := templateScope("var items=[{label:'a'}];\nvar navRef=null;\nkbindRef(\"N\",()=>navRef,v=>{navRef=v;});\n", nil, ids("N"))
	if !ok || len(args) != 1 || strings.Contains(tpl, "items") {
		t.Errorf("args %v:\n%s", args, tpl)
	}
}

// Strings that are not values (keys, method names, directives) and strings
// that are not instance ids stay literal.
func TestTemplateScopeLeavesNonValueStrings(t *testing.T) {
	body := `var o={"I":1,"m"(){return 1}};
var c=x?"I":"J";
switch(k){case "I":break;}
"use strict";
`
	tpl, args, ok := templateScope(body, nil, ids("I", "J", "use strict"))
	if !ok {
		t.Fatal("rejected")
	}
	if !strings.Contains(tpl, `{"I":1,"m"(){`) || !strings.Contains(tpl, `"use strict"`) {
		t.Errorf("keys, method names and directives must stay literal:\n%s", tpl)
	}
	if !strings.Contains(tpl, "case "+SharedScopeArg+"[0]:") || strings.Join(args, " ") != `"I" "J"` {
		t.Errorf("values should become arguments (%v):\n%s", args, tpl)
	}
}

// A scope reading a name the page script declares around it, or one whose
// meaning depends on the enclosing function, stays inline.
func TestTemplateScopeRejectsPageNames(t *testing.T) {
	reserved := pageLevelNames(&EmitResult{SharedModuleVars: []string{"var counter=0"}})
	for _, body := range []string{
		"kbindText(\"I\",()=>counter);\n",
		"var a=arguments.length;\n",
		"var self=this;\n",
		"class A{}\n",
		"with(o){x()}\n",
		"var x=`unterminated;\n",
	} {
		if _, _, ok := templateScope(body, reserved, ids("I")); ok {
			t.Errorf("accepted %q", body)
		}
	}
	// Property names and object keys are not reads of the name.
	if _, _, ok := templateScope("var o={counter:1,class:'c'};o.counter++;\n", reserved, nil); !ok {
		t.Error("rejected a property named like a page variable")
	}
}

// The source markers of a sourcemap build are numbered per template, so the
// same component on two pages compiles to one template.
func TestSharedScopeLocalMarks(t *testing.T) {
	page := []SrcMark{{File: "x.tsx", Line: 1}, {File: "nav.tsx", Line: 4, Col: 2}, {File: "nav.tsx", Line: 9, Col: 3}}
	body := "/*@k:1*/kbindText(\"I\",()=>a());/*@k:end*/\n/*@k:2*/kbindHandler(\"I\",\"onClick\",f);/*@k:end*/\n"
	got, marks := localMarks(body, page)
	if !strings.Contains(got, "/*@k:0*/kbindText") || !strings.Contains(got, "/*@k:1*/kbindHandler") || len(marks) != 2 || marks[0].Line != 4 {
		t.Errorf("localMarks = %q %v", got, marks)
	}
	_, a, ok1 := shareScope(got, marks, nil, ids("I"), "nav.tsx")
	_, b, ok2 := shareScope(strings.ReplaceAll(got, `"I"`, `"K"`), marks, nil, ids("K"), "nav.tsx")
	if !ok1 || !ok2 || a.ID != b.ID {
		t.Errorf("same component, different ids: %v %v", a.ID, b.ID)
	}
	_, c, _ := shareScope(got, []SrcMark{{File: "other.tsx", Line: 4, Col: 2}, marks[1]}, nil, ids("I"), "other.tsx")
	if c.ID == a.ID {
		t.Error("templates mapping to different sources share an id")
	}
}
