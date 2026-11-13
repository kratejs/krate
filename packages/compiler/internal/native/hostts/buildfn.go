package hostts

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/bake"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// buildFn is a `@build` function the UI uses.
type buildFn struct {
	ID   string
	Name string
	Rel  string
	Line int
	// path is the host module; gen the generated module of an inline
	// function. Exactly one is set.
	path string
	gen  *genModule
	// useKey indexes builder.uses.
	useKey string
	// requests are the distinct literal argument lists the UI calls it with.
	requests map[string][]any
}

// buildNames lists the exports of the host module at path that are `@build`.
func (b *builder) buildNames(path string) map[string]bool {
	m := b.hostMod[path]
	if m == nil {
		return nil
	}
	var out map[string]bool
	for _, f := range m.Funcs {
		if f.Build && !f.Inline {
			if out == nil {
				out = map[string]bool{}
			}
			out[f.Name] = true
		}
	}
	return out
}

// addBuild records a build function the UI uses and checks how the UI uses it.
func (b *builder) addBuild(bf *buildFn) {
	bf.requests = map[string][]any{}
	b.builds = append(b.builds, *bf)
	built := &b.builds[len(b.builds)-1]
	for _, u := range b.uses[bf.useKey] {
		if u.Name != bf.Name {
			continue
		}
		at := func(code diag.Code, msg, hint string) {
			b.diags = append(b.diags, diag.New(u.File, u.Line, u.Col, msg, hint, u.Text).WithCode(code))
		}
		switch {
		case !u.Call:
			at(diag.CodeBakeRef, bf.Name+" is a build function and must be called directly",
				"its value is looked up by the literal arguments of each call: write "+bf.Name+"(...) where the value is needed")
		case !u.Literal:
			at(diag.CodeBakeArgs, fmt.Sprintf("argument %d of the call to build function %s is not a literal", u.BadArg, bf.Name),
				"build functions run once during the build, so every argument must be a literal (string, number, boolean, null, or an array or object of those)")
		default:
			key := bake.Key(u.Args)
			built.requests[key] = u.Args
			b.res.Calls = append(b.res.Calls, BakedCall{File: u.File, Offset: u.Offset, Local: u.Local, ID: bf.ID, Key: key})
		}
	}
}

// bakeBuilds evaluates the TypeScript build functions the UI calls and
// stores the results in the build result.
func (b *builder) bakeBuilds() error {
	b.res.Baked = bake.Table{}
	type req struct {
		fn   *buildFn
		key  string
		args []any
	}
	var reqs []req
	for i := range b.builds {
		bf := &b.builds[i]
		keys := make([]string, 0, len(bf.requests))
		for k := range bf.requests {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			reqs = append(reqs, req{bf, k, bf.requests[k]})
		}
	}
	if len(reqs) == 0 {
		return nil
	}
	// One entry module imports every build function.
	var sb strings.Builder
	sb.WriteString("// generated build entry\n")
	var table []string
	seen := map[string]bool{}
	for i := range b.builds {
		bf := &b.builds[i]
		if len(bf.requests) == 0 {
			continue
		}
		from := bf.path
		if bf.gen != nil {
			from = "krate-host:inline/" + bf.gen.modID
		}
		fmt.Fprintf(&sb, "import * as __m%d from %s;\n", i, quote(filepath.ToSlash(from)))
		if !seen[bf.ID] {
			seen[bf.ID] = true
			table = append(table, fmt.Sprintf("%s: __m%d[%s]", quote(bf.ID), i, quote(bf.Name)))
		}
	}
	sb.WriteString("globalThis.__krateBuildFns = {" + strings.Join(table, ", ") + "};\n")
	code, errs, err := b.bundleBuild(sb.String())
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		for _, e := range errs {
			b.diags = append(b.diags, b.bundleDiag(e, b.genByID()))
		}
		return nil
	}

	ws := make([]bakeCall, len(reqs))
	for i, r := range reqs {
		ws[i] = bakeCall{ID: r.fn.ID, Args: bake.ArgsJSON(r.args)}
	}
	results, err := runBuildFns(code, ws)
	if err != nil {
		b.diags = append(b.diags, diag.New(b.builds[0].Rel, b.builds[0].Line, 1, "cannot run the build functions: "+err.Error(),
			"the embedded JavaScript runtime failed to start", "").WithCode(diag.CodeBakeEval))
		return nil
	}
	for i, r := range reqs {
		res := results[i]
		loc := func(code diag.Code, msg, hint string) {
			b.diags = append(b.diags, diag.New(r.fn.Rel, max(r.fn.Line, 1), 1, msg, hint, "").WithCode(code))
		}
		call := r.fn.Name + "(" + strings.Trim(bake.ArgsJSON(r.args), "[]") + ")"
		switch {
		case res.Err != "":
			loc(diag.CodeBakeEval, "build function "+call+" failed: "+res.Err,
				"build functions run in a bare JavaScript VM during the build: no fetch, timers, file system or locale-dependent methods")
		case res.Bad != "":
			loc(diag.CodeBakeValue, "the result of build function "+call+" cannot be baked: "+res.Bad,
				"return plain data: strings, finite numbers, booleans, null, arrays and plain objects")
		default:
			if err := bake.Validate([]byte(res.OK)); err != nil {
				loc(diag.CodeBakeValue, "the result of build function "+call+" cannot be baked: "+err.Error(), "return less or plainer data")
				continue
			}
			b.res.Baked.Set(r.fn.ID, r.key, json.RawMessage(res.OK))
		}
	}
	return nil
}

func (b *builder) genByID() map[string]*genModule {
	gens := map[string]*genModule{}
	for _, g := range b.genList {
		gens[g.modID] = g
	}
	return gens
}

// bundleBuild bundles the build entry into one IIFE, reusing the host
// bundle's resolution of inline modules.
func (b *builder) bundleBuild(entry string) (string, []api.Message, error) {
	opts := api.BuildOptions{
		EntryPoints:   []string{virtualEntry},
		AbsWorkingDir: b.root,
		Bundle:        true,
		Format:        api.FormatIIFE,
		Target:        api.ES2020,
		Platform:      api.PlatformNeutral,
		MainFields:    []string{"module", "main"},
		Conditions:    []string{"import", "default"},
		External:      append([]string{"node:*"}, nodeBuiltins...),
		Outfile:       filepath.Join(b.opts.OutDir, "host", "build.js"),
		Write:         false,
		LogLevel:      api.LogLevelSilent,
		Define:        map[string]string{"process.env.NODE_ENV": `"production"`},
		Plugins:       []api.Plugin{b.virtualPlugin(entry, b.genByID(), false)},
	}
	tsstrip.HandleTierDecorators(&opts)
	r := api.Build(opts)
	if len(r.Errors) > 0 {
		return "", r.Errors, nil
	}
	for _, f := range r.OutputFiles {
		if strings.HasSuffix(f.Path, ".js") {
			return string(f.Contents), nil, nil
		}
	}
	return "", nil, fmt.Errorf("esbuild produced no output for the build functions")
}

// bakeCall is one call of a build function: its id and JSON argument list.
type bakeCall struct {
	ID   string `json:"id"`
	Args string `json:"args"`
}

// bakeOutcome is the result of one call.
type bakeOutcome struct {
	OK  string `json:"ok"`
	Err string `json:"err"`
	Bad string `json:"bad"`
}

// runBuildFns runs the calls in a bare VM: ECMAScript built-ins only, no
// fetch, timers, file system, or locale-dependent methods.
func runBuildFns(code string, calls []bakeCall) ([]bakeOutcome, error) {
	rt, err := jsruntime.NewBare()
	if err != nil {
		return nil, err
	}
	defer rt.Close()
	if _, err := rt.Execute(code); err != nil {
		out := make([]bakeOutcome, len(calls))
		for i := range out {
			out[i].Err = "loading the module failed: " + err.Error()
		}
		return out, nil
	}
	in, _ := json.Marshal(calls)
	if _, err := rt.Execute("globalThis.__krateBakeIn = " + string(in) + ";\n" + bakeDriver); err != nil {
		return nil, err
	}
	rt.DrainJobs()
	v, err := rt.Execute("JSON.stringify(globalThis.__krateBakeOut)")
	if err != nil {
		return nil, err
	}
	s, _ := v.(string)
	var out []bakeOutcome
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("reading the build results: %w", err)
	}
	if len(out) != len(calls) {
		return nil, fmt.Errorf("the build driver returned %d results for %d calls", len(out), len(calls))
	}
	for i := range out {
		if out[i].OK == "" && out[i].Err == "" && out[i].Bad == "" {
			out[i].Err = bake.ErrUnsettled.Error()
		}
	}
	return out, nil
}

// bakeDriver runs globalThis.__krateBakeIn against __krateBuildFns and leaves
// the outcomes in __krateBakeOut. A value is checked while it is converted to
// plain data, so the VM can name what is wrong.
const bakeDriver = `(function () {
  var MAX_DEPTH = 512, MAX_LEN = 1000000, MAX_BYTES = 8388608;
  var fns = globalThis.__krateBuildFns, calls = globalThis.__krateBakeIn;
  var out = new Array(calls.length);
  globalThis.__krateBakeOut = out;
  function bad(path, msg) { var e = new Error((path || 'the result') + ' ' + msg); e.bad = true; throw e; }
  function norm(v, depth, path, stack) {
    if (depth > MAX_DEPTH) bad(path, 'is nested deeper than ' + MAX_DEPTH + ' levels');
    switch (typeof v) {
      case 'string': case 'boolean': return v;
      case 'number':
        if (!isFinite(v)) bad(path, 'is ' + String(v) + ', which JSON cannot hold');
        return v;
      case 'undefined': return undefined;
      case 'bigint': return bad(path, 'is a bigint');
      case 'function': return bad(path, 'is a function');
      case 'symbol': return bad(path, 'is a symbol');
    }
    if (v === null) return null;
    if (typeof v.then === 'function') bad(path, 'is a promise nested in the result');
    if (stack.indexOf(v) >= 0) bad(path, 'is a circular structure');
    var res;
    stack.push(v);
    if (Array.isArray(v)) {
      if (v.length > MAX_LEN) bad(path, 'is an array of ' + v.length + ' elements (limit ' + MAX_LEN + ')');
      res = new Array(v.length);
      for (var i = 0; i < v.length; i++) {
        var x = norm(v[i], depth + 1, path + '[' + i + ']', stack);
        res[i] = x === undefined ? null : x;
      }
    } else {
      var proto = Object.getPrototypeOf(v);
      if (proto !== Object.prototype && proto !== null) {
        var name = (proto.constructor && proto.constructor.name) || 'an unknown class';
        bad(path, 'is ' + name + ', not plain data (only strings, numbers, booleans, null, arrays and plain objects)');
      }
      res = {};
      var keys = Object.keys(v);
      for (var j = 0; j < keys.length; j++) {
        var y = norm(v[keys[j]], depth + 1, path + '.' + keys[j], stack);
        if (y !== undefined) Object.defineProperty(res, keys[j], { value: y, enumerable: true, writable: true, configurable: true });
      }
    }
    stack.pop();
    return res;
  }
  calls.forEach(function (c, i) {
    var p;
    try {
      var f = fns[c.id];
      if (typeof f !== 'function') throw new Error(c.id + ' is not a function');
      p = Promise.resolve(f.apply(undefined, JSON.parse(c.args)));
    } catch (e) { p = Promise.reject(e); }
    p.then(function (v) {
      try {
        var n = norm(v, 1, '', []);
        if (n === undefined) bad('', 'is undefined');
        var s = JSON.stringify(n);
        if (s.length > MAX_BYTES) bad('', 'is ' + s.length + ' bytes of JSON (limit ' + MAX_BYTES + ')');
        out[i] = { ok: s };
      } catch (e) {
        out[i] = e && e.bad ? { bad: String(e.message) } : { err: String((e && e.message) || e) };
      }
    }, function (e) { out[i] = { err: String((e && e.message) || e) }; });
  });
})();`
