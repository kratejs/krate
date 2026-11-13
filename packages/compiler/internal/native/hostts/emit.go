package hostts

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// stubSource is the module the UI bundle gets in place of a host module.
// build names the exports that are build functions: their stubs read the
// baked values instead of calling the host.
func stubSource(modID string, exps []*export, build map[string]bool) string {
	var body strings.Builder
	usesCall, usesStream, usesSignal, usesBaked := false, false, false, false
	for _, e := range exps {
		id := quote(modID + "." + e.Name)
		var expr string
		switch {
		case e.Kind == kindValue:
			continue
		case build[e.Name]:
			expr = "(...a) => __krateBaked(" + id + ", a)"
			usesBaked = true
		case e.Kind == KindStream:
			expr = "(...a) => __krateStream(" + id + ", a)"
			usesStream = true
		case e.Kind == KindSignal:
			expr = "__krateSig(" + id + ")"
			usesSignal = true
		default:
			expr = "(...a) => __krateCall(" + id + ", a)"
			usesCall = true
		}
		if e.Name == "default" {
			body.WriteString("const __default = " + expr + ";\nexport default __default;\n")
		} else {
			body.WriteString("export const " + e.Name + " = " + expr + ";\n")
		}
	}
	var names []string
	if usesCall {
		names = append(names, "call as __krateCall")
	}
	if usesSignal {
		names = append(names, "signal as __krateSignal")
	}
	if usesStream {
		names = append(names, "stream as __krateStream")
	}
	if usesBaked {
		names = append(names, "baked as __krateBaked")
	}
	var sb strings.Builder
	sb.WriteString("// generated: host stub for " + modID + "\n")
	if len(names) > 0 {
		sb.WriteString("import { " + strings.Join(names, ", ") + " } from " + quote(appPkg) + ";\n")
	}
	if usesSignal {
		// The signal is created on first use, after the page has installed the
		// reactive client, so importing the module never opens a connection.
		sb.WriteString("const __krateSig = (name) => {\n  let s;\n  const get = () => (s ??= __krateSignal(name));\n  const f = () => get()();\n  f.subscribe = (fn) => get().subscribe(fn);\n  return f;\n};\n")
	}
	sb.WriteString(body.String())
	return sb.String()
}

// entrySource generates the host entry module.
func entrySource(regs []regEntry) string {
	type imp struct {
		from  string
		names []string
	}
	var order []string
	imps := map[string]*imp{}
	var calls []string
	for i, r := range regs {
		im := imps[r.importFrom]
		if im == nil {
			im = &imp{from: r.importFrom}
			imps[r.importFrom] = im
			order = append(order, r.importFrom)
		}
		v := fmt.Sprintf("__e%d", i)
		if r.local == "default" {
			im.names = append(im.names, "default as "+v)
		} else {
			im.names = append(im.names, r.local+" as "+v)
		}
		id := quote(r.ID)
		switch Kind(r.Kind) {
		case KindFunc, KindStream:
			calls = append(calls, fmt.Sprintf("__h.register(%s, %s, %s);", id, v, quote(r.Kind)))
		case KindSignal:
			calls = append(calls, fmt.Sprintf("__h.registerSignal(%s, %s);", id, v))
		default:
			calls = append(calls, fmt.Sprintf("__auto(%s, %s);", id, v))
		}
	}
	var sb strings.Builder
	sb.WriteString("// generated host entry\n")
	for _, f := range order {
		im := imps[f]
		sb.WriteString("import { " + strings.Join(im.names, ", ") + " } from " + quote(im.from) + ";\n")
	}
	sb.WriteString(`const __h = globalThis.__krateHost;
function __auto(id, v) {
  if (typeof v === "function") {
    const c = v.constructor && v.constructor.name;
    __h.register(id, v, c === "AsyncGeneratorFunction" || c === "GeneratorFunction" ? "stream" : "func");
  } else if (typeof v === "object" && v && v.__krateHostSignal === true) {
    __h.registerSignal(id, v);
  }
}
`)
	for _, c := range calls {
		sb.WriteString(c + "\n")
	}
	return sb.String()
}

// childEntrySource generates the entry of a host child bundle: the Krate
// host child runtime first (it installs globalThis.__krateHost), then the host
// modules and their registrations, then the server start.
func childEntrySource(regs []regEntry) string {
	body := entrySource(regs)
	const head = "// generated host entry\n"
	return head + "import { start as __krateStart } from \"krate-hostchild:runtime\";\n" +
		strings.TrimPrefix(body, head) + "__krateStart();\n"
}

// typesSource writes the .d.ts of a host module. importPath is the specifier
// of the module as seen from the .d.ts file.
func typesSource(importPath string, exps []*export, build map[string]bool) string {
	mod := `typeof import(` + quote(importPath) + `)`
	var body strings.Builder
	needItem, needRemote := false, false
	for _, e := range exps {
		ref := mod + "[" + quote(e.Name) + "]"
		if e.Name != "default" && isPlainIdent(e.Name) {
			ref = mod + "." + e.Name
		}
		var t string
		switch {
		case e.Kind == kindValue:
			continue
		case build[e.Name]:
			t = "(...a: Parameters<" + ref + ">) => Awaited<ReturnType<" + ref + ">>"
		case e.Kind == KindFunc:
			t = "(...a: Parameters<" + ref + ">) => Promise<Awaited<ReturnType<" + ref + ">>>"
		case e.Kind == KindStream:
			t = "(...a: Parameters<" + ref + ">) => AsyncIterable<__Item<ReturnType<" + ref + ">>>"
			needItem = true
		case e.Kind == KindSignal:
			t = ref + ` extends import("@krate/app").HostSignal<infer T> ? import("@krate/app").ReadonlyHostSignal<T, T | undefined> : never`
		default:
			t = "__Remote<" + ref + ">"
			needRemote = true
		}
		if e.Name == "default" {
			body.WriteString("declare const __default: " + t + ";\nexport default __default;\n")
		} else {
			body.WriteString("export declare const " + e.Name + ": " + t + ";\n")
		}
	}
	var sb strings.Builder
	sb.WriteString("// generated: types of the host stub; calls are async\n")
	if needItem || needRemote {
		sb.WriteString("type __Item<R> = R extends AsyncIterable<infer T> ? T : R extends Iterable<infer T> ? T : unknown;\n")
	}
	if needRemote {
		sb.WriteString(`type __Remote<F> = F extends (...a: infer A) => infer R
  ? R extends AsyncIterable<infer T> ? (...a: A) => AsyncIterable<T> : (...a: A) => Promise<Awaited<R>>
  : F extends import("@krate/app").HostSignal<infer T> ? import("@krate/app").ReadonlyHostSignal<T, T | undefined> : never;
`)
	}
	sb.WriteString(body.String())
	return sb.String()
}

func isPlainIdent(s string) bool {
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdent(s[i]) {
			return false
		}
	}
	return true
}

// typesImportPath returns the specifier for the module at srcPath as seen from
// the .d.ts file at dts.
func typesImportPath(dts, srcPath string) string {
	r, err := filepath.Rel(filepath.Dir(dts), srcPath)
	if err != nil {
		return filepath.ToSlash(srcPath)
	}
	r = filepath.ToSlash(r)
	for _, ext := range []string{".ts", ".tsx", ".mts"} {
		r = strings.TrimSuffix(r, ext)
	}
	if !strings.HasPrefix(r, ".") {
		r = "./" + r
	}
	return r
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
