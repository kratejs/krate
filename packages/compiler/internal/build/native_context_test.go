package build

import (
	"regexp"
	"strings"
	"testing"
)

// A native context value read in any expression position is a live read of
// the provider's value, and an object value keeps its getters and setters.
func TestNativeContextPrimitiveInEveryPosition(t *testing.T) {
	t.Parallel()
	_, outDir := buildCompatSrc(t, `import { createContext, createSignal } from "@krate/runtime";
const DensityCtx = createContext("compact");
function Row() {
  const density = DensityCtx.useContext();
  return <div class={"cx-" + density} data-t={"d-" + density}>{density === "wide" ? "W" : "C"}</div>;
}
export default function Page() {
  const [d, setD] = createSignal("compact");
  return (
    <DensityCtx.Provider value={d()}>
      <button onClick={() => setD(d() === "compact" ? "wide" : "compact")}>toggle</button>
      <Row />
    </DensityCtx.Provider>
  );
}`, nil)
	js := pageJS(t, outDir)
	for name, re := range map[string]string{
		"derived getter":      `(\w+)=krateDerived\(\(\)=>\w+\.__krate_ctx_DensityCtx\(\)\)`,
		"string concat":       `kbindAttr\("\d+","class",\(\)=>"cx-"\+\w+\(\)\)`,
		"second attribute":    `kbindAttr\("\d+","data-t",\(\)=>"d-"\+\w+\(\)\)`,
		"conditional content": `kbindContent\("\d+",\(\)=>\w+\(\)==="wide"\?"W":"C"\)`,
	} {
		if !regexp.MustCompile(re).MatchString(js) {
			t.Errorf("%s: no match for %s in:\n%s", name, re, js)
		}
	}
}

func TestNativeContextObjectValue(t *testing.T) {
	t.Parallel()
	html, outDir := buildCompatSrc(t, `import { createContext, createSignal } from "@krate/runtime";
function useCounter() {
  const [count, setCount] = createSignal(0);
  return { count, setCount };
}
const LiteralCtx = createContext({ count: () => 0, setCount: (n: number) => {} });
const HookCtx = createContext({ count: () => 0, setCount: (n: number) => {} });
function FromLiteral() {
  const { count, setCount } = LiteralCtx.useContext();
  return <button onClick={() => setCount(count() + 1)}>{count()}</button>;
}
function FromHook() {
  const store = HookCtx.useContext();
  return <button onClick={() => store.setCount(store.count() + 1)}>{store.count()}</button>;
}
export default function Page() {
  const [count, setCount] = createSignal(0);
  const store = useCounter();
  return (
    <LiteralCtx.Provider value={{ count, setCount }}>
      <HookCtx.Provider value={store}>
        <FromLiteral />
        <FromHook />
      </HookCtx.Provider>
    </LiteralCtx.Provider>
  );
}`, nil)
	js := pageJS(t, outDir)
	// The provider hands the getter itself over, not its current value.
	if m := regexp.MustCompile(`__krate_ctx_LiteralCtx:\(\)=>\(\{count:(\w+)(\(\))?,`).FindStringSubmatch(js); m == nil || m[2] != "" {
		t.Errorf("the provider value must carry the getter bare:\n%s", js)
	}
	if !regexp.MustCompile(`krateDerived\(\(\)=>\w+\.__krate_ctx_LiteralCtx\(\)\)`).MatchString(js) {
		t.Errorf("the destructured read must follow the provider value:\n%s", js)
	}
	if !regexp.MustCompile(`kbindContent\("\d+",\(\)=>\w+\(\)\.count\(\)\)`).MatchString(js) {
		t.Errorf("a member read of the hook value must stay live:\n%s", js)
	}
	if strings.Contains(visibleHTML(html), "function") {
		t.Errorf("a context getter leaked into the markup:\n%s", visibleHTML(html))
	}
}

// A bare getter is the reactive source of a resource, like an arrow function.
func TestNativeResourceBareGetterSource(t *testing.T) {
	t.Parallel()
	_, outDir := buildCompatSrc(t, `import { createSignal, createResource } from "@krate/runtime";
export default function Page() {
  const [id, setId] = createSignal("a");
  const [item] = createResource(id, (key: string) => Promise.resolve("got-" + key));
  return <p onClick={() => setId(id() + "x")}>{item()}</p>;
}`, nil)
	js := pageJS(t, outDir)
	if !regexp.MustCompile(`createResource\(\w+,`).MatchString(js) {
		t.Errorf("the getter must reach createResource as it is:\n%s", js)
	}
}

// A derived value written as a local function is read live, in the arrow and
// the function declaration form, with a reduce callback parameter in between.
func TestNativeDerivedLocalFunctionsStayLive(t *testing.T) {
	t.Parallel()
	_, outDir := buildCompatSrc(t, `import { createSignal } from "@krate/runtime";
export default function Page() {
  const [items, setItems] = createSignal([{ price: 2 }, { price: 3 }]);
  const total = () => items().reduce((a, b) => a + b.price, 0);
  function count() {
    return items().reduce(function (n) { return n + 1; }, 0);
  }
  return <div><b>{total()}</b><i>{count()}</i><button onClick={() => setItems([...items(), { price: 5 }])}>add</button></div>;
}`, nil)
	js := pageJS(t, outDir)
	if n := len(regexp.MustCompile(`kbind(Text|Content)\("\d+",\(\)=>`).FindAllString(js, -1)); n != 2 {
		t.Errorf("both derived reads must be live bindings, got %d:\n%s", n, js)
	}
}
