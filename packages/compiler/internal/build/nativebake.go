package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/bake"
	"github.com/kratejs/krate/packages/compiler/internal/native/gobind"
	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
)

// bakeGoBuilds evaluates the //krate:build functions the UI calls and returns
// their results by function id and canonical argument key. The functions run
// in a generated Go program (see gobind.Evaluate), which is only built when
// the UI uses at least one of them.
func (b *Builder) bakeGoBuilds(res *gobind.Result, imps []hostts.GoImport) (bake.Table, []hostts.BakedCall, []diag.Diagnostic, error) {
	buildNames := map[string]map[string]bool{} // source path -> name -> true
	for _, m := range res.Modules {
		if len(m.Build) == 0 {
			continue
		}
		set := map[string]bool{}
		for _, n := range m.Build {
			set[n] = true
		}
		buildNames[filepath.Clean(m.Source)] = set
	}
	if len(buildNames) == 0 {
		return nil, nil, nil, nil
	}
	idOf := map[string]string{}
	for _, m := range res.Modules {
		idOf[filepath.Clean(m.Source)] = m.ID
	}

	var ds []diag.Diagnostic
	var goCalls []hostts.BakedCall
	type request struct {
		id, key string
		args    []any
		use     hostts.Use
	}
	var reqs []request
	seen := map[string]bool{}
	for _, imp := range imps {
		set := buildNames[filepath.Clean(imp.File)]
		if set == nil {
			continue
		}
		if imp.Namespace {
			continue // already KR7201
		}
		for _, u := range imp.Uses {
			if !set[u.Name] {
				continue
			}
			add := func(code diag.Code, msg, hint string) {
				ds = append(ds, diag.New(u.File, u.Line, u.Col, msg, hint, u.Text).WithCode(code))
			}
			switch {
			case !u.Call:
				add(diag.CodeBakeRef, u.Name+" is a build function and must be called directly",
					"its value is looked up by the literal arguments of each call: write "+u.Name+"(...) where the value is needed")
			case !u.Literal:
				add(diag.CodeBakeArgs, fmt.Sprintf("argument %d of the call to build function %s is not a literal", u.BadArg, u.Name),
					"build functions run once during the build, so every argument must be a literal (string, number, boolean, null, or an array or object of those)")
			default:
				id := idOf[filepath.Clean(imp.File)] + "." + u.Name
				key := bake.Key(u.Args)
				goCalls = append(goCalls, hostts.BakedCall{File: u.File, Offset: u.Offset, Local: u.Local, ID: id, Key: key})
				if !seen[id+"\x00"+key] {
					seen[id+"\x00"+key] = true
					reqs = append(reqs, request{id, key, u.Args, u})
				}
			}
		}
	}
	if len(ds) > 0 {
		return nil, nil, ds, nil
	}
	if len(reqs) == 0 {
		return nil, nil, nil, nil
	}
	sort.SliceStable(reqs, func(i, j int) bool {
		if reqs[i].id != reqs[j].id {
			return reqs[i].id < reqs[j].id
		}
		return reqs[i].key < reqs[j].key
	})
	calls := make([]gobind.BakeCall, len(reqs))
	for i, r := range reqs {
		calls[i] = gobind.BakeCall{ID: r.id, Args: bake.ArgsJSON(r.args)}
	}
	printf("  %s>%s Evaluating %d Go build function call(s)\n", cGreen, cReset, len(calls))
	out, err := gobind.Evaluate(b.Root, b.nativeOutDir(), res, calls)
	if err != nil {
		var be *gobind.BakeError
		if errors.As(err, &be) {
			return nil, nil, []diag.Diagnostic{diag.New("go.mod", 1, 1, be.Msg+":\n"+indent(trimTail(be.Output, 20)),
				"the packages the UI imports must compile, and the go command must be on PATH; run go build ./... in the project for the full output", "").WithCode(diag.CodeBakeGo)}, nil
		}
		return nil, nil, nil, err
	}
	table := bake.Table{}
	for i, r := range reqs {
		call := r.id + "(" + strings.Trim(bake.ArgsJSON(r.args), "[]") + ")"
		at := func(code diag.Code, msg, hint string) {
			ds = append(ds, diag.New(r.use.File, r.use.Line, r.use.Col, msg, hint, r.use.Text).WithCode(code))
		}
		o := out[i]
		switch {
		case o.Err != "":
			at(diag.CodeBakeEval, "build function "+call+" failed: "+o.Err+" ("+o.Code+")",
				"a build function runs during the build; fix the error or drop //krate:build to call it at run time")
		default:
			if err := bake.Validate(o.OK); err != nil {
				at(diag.CodeBakeValue, "the result of build function "+call+" cannot be baked: "+err.Error(), "return less or plainer data")
				continue
			}
			table.Set(r.id, r.key, json.RawMessage(o.OK))
		}
	}
	return table, goCalls, ds, nil
}
