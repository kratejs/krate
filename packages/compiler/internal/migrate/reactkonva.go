package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// react-konva.
//
// @krate/konva has react-konva's API: <Stage> compiles with a Krate page, and in
// a "use client" component (a Preact island) the build resolves it to an island
// build rendered by Preact. A module that imports react-konva otherwise runs in
// a real-React island, which ships React, react-dom and a second copy of every
// React library it renders. When every react-konva API the project's modules
// use is provided, the imports are pointed at @krate/konva. One unsupported API
// anywhere keeps all of them on react-konva: the modules render each other's
// shapes inside one <Stage>, so they must share a renderer. Type imports from
// konva itself are left alone; konva is not a React library.

const (
	reactKonvaModule = "react-konva"
	krateKonvaModule = "@krate/konva"
)

// reactKonvaCoreModules are react-konva entry points with the same API as the
// package root (the core build leaves registering konva's shapes to the app).
var reactKonvaCoreModules = []string{"react-konva/lib/ReactKonvaCore", "react-konva/es/ReactKonvaCore", "react-konva/lib/ReactKonvaCore.js", "react-konva/es/ReactKonvaCore.js"}

// krateKonvaNames are the react-konva exports @krate/konva provides, values and
// types. TestKrateKonvaNamesExist keeps the list in step with packages/konva.
var krateKonvaNames = setOf(
	// values
	"Stage", "Layer", "FastLayer", "Group", "Label", "Rect", "Circle", "Ellipse", "Wedge", "Line",
	"Sprite", "Image", "Text", "TextPath", "Star", "Ring", "Arc", "Tag", "Path", "RegularPolygon",
	"Arrow", "Shape", "Transformer", "useStrictMode", "useContextBridge", "version",
	// types
	"KonvaNodeEvents", "KonvaNodeComponent", "StageProps",
)

// reactKonvaUnsupported explains the react-konva exports @krate/konva does not
// have.
var reactKonvaUnsupported = map[string]string{
	"KonvaRenderer": "@krate/konva has no React reconciler",
}

// konvaBareImport matches side-effect imports, dynamic imports and require()
// calls; group 1 is the specifier.
var konvaBareImport = regexp.MustCompile(`(?:\bimport\s*\(?|\brequire\s*\()\s*["']([^"'\n]+)["']`)

// konvaModuleScan is what one source module uses from react-konva and the
// packages built on it.
type konvaModuleScan struct {
	file     *PlannedFile
	names    []string
	blockers []string
}

func isReactKonvaSpecifier(spec string) bool {
	if spec == reactKonvaModule {
		return true
	}
	for _, m := range reactKonvaCoreModules {
		if spec == m {
			return true
		}
	}
	return false
}

// scanKonvaModule records the react-konva names a module uses and the
// react-konva subpaths and react-konva-based packages @krate/konva cannot
// replace.
func scanKonvaModule(f *PlannedFile, src string, konvaBased func(pkg string) bool) *konvaModuleScan {
	scan := &konvaModuleScan{file: f}
	uses := false
	for _, m := range threeImportStmt.FindAllStringSubmatch(src, -1) {
		kind, clause, spec := m[1], m[2], m[3]
		if kind == "export" && !strings.Contains(clause, "{") && !strings.Contains(clause, "*") {
			continue
		}
		switch {
		case isReactKonvaSpecifier(spec):
			uses = true
			names, all := threeClauseNames(src, clause)
			if all {
				continue
			}
			scan.names = append(scan.names, names...)
		case strings.HasPrefix(spec, reactKonvaModule+"/"):
			uses = true
			scan.blockers = append(scan.blockers, "imports "+spec+"; @krate/konva provides the react-konva package root")
		default:
			if pkg := bareSpecifierPackage(spec); pkg != "" && pkg != reactKonvaModule && konvaBased(pkg) {
				scan.blockers = append(scan.blockers, "imports "+pkg+", which is built on react-konva")
			}
		}
	}
	// A side-effect import (`import "react-konva"`) and require() also tie the
	// module to react-konva.
	for _, m := range konvaBareImport.FindAllStringSubmatch(src, -1) {
		if isReactKonvaSpecifier(m[1]) {
			uses = true
		}
	}
	for _, n := range scan.names {
		if krateKonvaNames[n] {
			continue
		}
		reason := "@krate/konva does not export it"
		if why, ok := reactKonvaUnsupported[n]; ok {
			reason = why
		}
		scan.blockers = append(scan.blockers, "uses "+n+" from react-konva: "+reason)
	}
	if !uses && len(scan.blockers) == 0 {
		return nil
	}
	return scan
}

// konvaBasedPackages reports packages whose package.json depends on
// react-konva (directly or as a peer), found from root upwards.
func konvaBasedPackages(root string) func(pkg string) bool {
	cache := map[string]bool{}
	return func(pkg string) bool {
		if v, ok := cache[pkg]; ok {
			return v
		}
		found := false
		for dir := root; ; dir = filepath.Dir(dir) {
			data, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
			if err == nil {
				var manifest struct {
					Dependencies     map[string]string `json:"dependencies"`
					PeerDependencies map[string]string `json:"peerDependencies"`
				}
				if json.Unmarshal(data, &manifest) == nil {
					_, dep := manifest.Dependencies[reactKonvaModule]
					_, peer := manifest.PeerDependencies[reactKonvaModule]
					found = dep || peer
				}
				break
			}
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
		cache[pkg] = found
		return found
	}
}

// migrateReactKonva points react-konva imports at @krate/konva when every module
// that uses react-konva only uses what @krate/konva provides, and reports what
// kept them on react-konva otherwise.
func migrateReactKonva(rep *Report, root string) {
	konvaBased := konvaBasedPackages(root)
	var scans []*konvaModuleScan
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 || f.Kind == KindPublic || f.Kind == KindPackage || !isModuleSource(fileLabel(f)) {
			continue
		}
		src := string(f.Content)
		if !strings.Contains(src, reactKonvaModule) && !mentionsKonvaBased(src, konvaBased) {
			continue
		}
		if scan := scanKonvaModule(f, src, konvaBased); scan != nil {
			scans = append(scans, scan)
		}
	}
	usesReactKonva := false
	var blocked []*konvaModuleScan
	for _, s := range scans {
		if len(s.blockers) > 0 {
			blocked = append(blocked, s)
		}
		if len(s.names) > 0 || strings.Contains(string(s.file.Content), reactKonvaModule) {
			usesReactKonva = true
		}
	}
	if !usesReactKonva {
		return
	}
	if len(blocked) > 0 {
		for _, s := range blocked {
			for _, b := range s.blockers {
				rep.Manual = append(rep.Manual, ManualItem{
					Source: fileLabel(s.file),
					Reason: "react-konva stays (a real-React island that ships React and react-dom): this module " + b,
					Hint:   "replace that API (konva's own classes work through refs in @krate/konva), then re-run the migration to move the canvas modules to @krate/konva; see https://krate.js.org/docs/features/konva/",
				})
			}
		}
		rep.Warnings = append(rep.Warnings, "kept react-konva in "+plural(len(scans), "module")+": "+plural(len(blocked), "module")+" use APIs @krate/konva does not provide (listed under manual items)")
		return
	}
	var all []string
	for _, s := range scans {
		src := string(s.file.Content)
		src = replaceModule(src, reactKonvaModule, krateKonvaModule)
		for _, m := range reactKonvaCoreModules {
			src = replaceModule(src, m, krateKonvaModule)
		}
		s.file.Content = []byte(src)
		names := uniqueSorted(s.names)
		all = append(all, names...)
		note := "rewrote react-konva imports to @krate/konva"
		if len(names) > 0 {
			note += " (" + strings.Join(names, ", ") + ")"
		}
		s.file.Rewrites = append(s.file.Rewrites, note+": the canvas renders without React (as a Preact island in a \"use client\" component) instead of running in a real-React island")
	}
	all = uniqueSorted(all)
	sort.Strings(all)
	msg := "moved " + plural(len(scans), "module") + " from react-konva to @krate/konva"
	if len(all) > 0 {
		msg += " (" + strings.Join(all, ", ") + ")"
	}
	rep.Warnings = append(rep.Warnings, msg+"; konva stays a dependency, and keep react-konva installed while a real-React island renders a canvas module: the build runs @krate/konva there as react-konva")
	addKrateKonvaDependency(rep, root)
}

func mentionsKonvaBased(src string, konvaBased func(string) bool) bool {
	for _, m := range threeImportStmt.FindAllStringSubmatch(src, -1) {
		if pkg := bareSpecifierPackage(m[3]); pkg != "" && pkg != "konva" && konvaBased(pkg) {
			return true
		}
	}
	return false
}

// addKrateKonvaDependency adds @krate/konva to the planned package.json, or asks
// for it when the migration does not update package.json.
func addKrateKonvaDependency(rep *Report, root string) {
	for i := range rep.Files {
		f := &rep.Files[i]
		if f.Kind != KindPackage || len(f.Content) == 0 {
			continue
		}
		var pkg map[string]any
		if json.Unmarshal(f.Content, &pkg) != nil {
			break
		}
		deps, _ := pkg["dependencies"].(map[string]any)
		if deps == nil {
			deps = map[string]any{}
		}
		if _, ok := deps[krateKonvaModule]; ok {
			return
		}
		spec := "latest"
		if v, ok := deps["@krate/runtime"].(string); ok && v != "" {
			spec = v
		}
		deps[krateKonvaModule] = spec
		pkg["dependencies"] = deps
		data, err := json.MarshalIndent(pkg, "", "  ")
		if err != nil {
			break
		}
		f.Content = append(data, '\n')
		f.Rewrites = append(f.Rewrites, "dependencies added: "+krateKonvaModule+"@"+spec+" (replaces react-konva)")
		return
	}
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil && strings.Contains(string(data), `"`+krateKonvaModule+`"`) {
		return
	}
	rep.Manual = append(rep.Manual, ManualItem{
		Source: "package.json",
		Reason: "the canvas modules now import @krate/konva, which is not a dependency yet",
		Hint:   "pnpm add @krate/konva (or re-run with --update-package)",
	})
}
