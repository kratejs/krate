package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// React Three Fiber.
//
// @krate/three has the @react-three/fiber API and compiles with the page, and
// @krate/three/drei provides the @react-three/drei helpers most scenes use. A
// module that imports fiber otherwise runs in a real-React island, which ships
// React, react-dom and a second copy of every React library it renders. When
// every fiber and drei API the project's modules use is provided, the imports
// are pointed at @krate/three. One unsupported API anywhere keeps all of them on
// fiber: the modules render each other's components inside one <Canvas>, so they
// must share a renderer.

const (
	fiberModule      = "@react-three/fiber"
	dreiModule       = "@react-three/drei"
	krateThreeModule = "@krate/three"
	krateDreiModule  = "@krate/three/drei"
)

// krateThreeFiberNames are the @react-three/fiber exports @krate/three
// provides, values and types. TestKrateThreeFiberNamesExist keeps the list in
// step with packages/three.
var krateThreeFiberNames = setOf(
	// values
	"Canvas", "useThree", "useFrame", "useLoader", "useGraph", "useStore", "useInstanceHandle",
	"extend", "createPortal", "createRoot", "unmountComponentAtNode", "applyProps", "getRootState",
	"dispose", "buildGraph", "flushSync", "act", "context", "_roots", "invalidate", "advance",
	"addEffect", "addAfterEffect", "addTail", "flushGlobalEffects", "events", "createEvents",
	"createPointerEvents",
	// types
	"ReactThreeFiber", "ThreeElements", "ThreeElement", "ThreeToJSXElements", "ThreeEvent", "CanvasProps",
	"Intersection", "DomEvent", "Events", "EventHandlers", "FilterFunction", "ComputeFunction",
	"EventManager", "GlobalRenderCallback", "GlobalEffectType", "AttachFnType", "AttachType", "Args",
	"InstanceProps", "Instance", "ConstructorRepresentation", "Catalogue", "GLProps", "CameraProps",
	"RenderProps", "ReconcilerRoot", "Subscription", "Dpr", "Size", "Viewport", "RenderCallback",
	"Frameloop", "Performance", "Renderer", "XRManager", "RootState", "RootStore", "ObjectMap", "Camera",
	"Disposable", "TrackedPromise", "LoaderResult", "Extensions", "MathRepresentation",
	"VectorRepresentation", "MathTypes", "MathType", "MathProps", "Vector2", "Vector3", "Vector4", "Color",
	"Layers", "Quaternion", "Euler", "Matrix3", "Matrix4", "RaycastableRepresentation", "EventProps",
	"ReactProps", "ElementProps",
)

// fiberUnsupported explains the fiber exports @krate/three does not have.
var fiberUnsupported = map[string]string{
	"reconciler": "@krate/three has no React reconciler",
}

// krateDreiNames are the @react-three/drei exports @krate/three/drei provides.
var krateDreiNames = setOf("OrbitControls", "Environment", "Lightformer", "useGLTF",
	"OrbitControlsProps", "EnvironmentProps", "LightformerProps")

// dreiEnvironmentUnsupported matches an <Environment> whose props
// @krate/three/drei does not implement (HDR files, presets, ground projection).
var dreiEnvironmentUnsupported = regexp.MustCompile(`<Environment\b[^>]*?\s(preset|files|ground|path|extensions)\s*=`)

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// threeImportStmt matches import and export-from statements (multi-line) of
// any module. Group 1 is `import` or `export`, 2 the clause, 3 the specifier.
var threeImportStmt = regexp.MustCompile(`(?m)^[ \t]*(import|export)\s+((?:type\s+)?[\w$*{}\s,]*?)\s*from\s*["']([^"'\n]+)["']`)

// threeImportUse is one name a module imports from fiber or drei.
type threeImportUse struct {
	module string
	name   string
}

// threeModuleScan is what one source module uses from fiber, drei and the
// other fiber-based packages.
type threeModuleScan struct {
	file     *PlannedFile
	uses     []threeImportUse
	blockers []string
}

// threeClauseNames returns the value names of an import or export clause.
// Type-only clauses and `type` specifiers name nothing that runs. A namespace
// import (`* as F`) returns the members read through it.
func threeClauseNames(src, clause string) (names []string, all bool) {
	clause = strings.TrimSpace(clause)
	if strings.HasPrefix(clause, "type ") || strings.HasPrefix(clause, "type{") {
		return nil, false
	}
	if clause == "*" {
		return nil, true
	}
	if i := strings.Index(clause, "* as "); i >= 0 {
		ns := strings.TrimSpace(strings.Trim(strings.TrimSpace(clause[i+5:]), ","))
		if j := strings.IndexAny(ns, " ,{"); j >= 0 {
			ns = ns[:j]
		}
		member := regexp.MustCompile(`\b` + regexp.QuoteMeta(ns) + `\s*\.\s*([A-Za-z_$][\w$]*)`)
		seen := map[string]bool{}
		for _, m := range member.FindAllStringSubmatch(src, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				names = append(names, m[1])
			}
		}
		return names, false
	}
	open, close := strings.Index(clause, "{"), strings.LastIndex(clause, "}")
	if open < 0 || close < open {
		// A default import: fiber and drei have none.
		if clause != "" {
			names = append(names, "default")
		}
		return names, false
	}
	if head := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(clause[:open]), ",")); head != "" {
		names = append(names, "default")
	}
	for _, spec := range strings.Split(clause[open+1:close], ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" || strings.HasPrefix(spec, "type ") {
			continue
		}
		if i := strings.Index(spec, " as "); i >= 0 {
			spec = strings.TrimSpace(spec[:i])
		}
		names = append(names, spec)
	}
	return names, false
}

// scanThreeModule records the fiber and drei names a module uses and the
// fiber-based packages it imports that @krate/three cannot replace.
func scanThreeModule(f *PlannedFile, src string, fiberBased func(pkg string) bool) *threeModuleScan {
	scan := &threeModuleScan{file: f}
	for _, m := range threeImportStmt.FindAllStringSubmatch(src, -1) {
		kind, clause, spec := m[1], m[2], m[3]
		if kind == "export" && !strings.Contains(clause, "{") && !strings.Contains(clause, "*") {
			continue
		}
		switch spec {
		case fiberModule, dreiModule:
			names, all := threeClauseNames(src, clause)
			if all {
				if spec == dreiModule {
					scan.blockers = append(scan.blockers, "re-exports all of @react-three/drei; @krate/three/drei has only "+strings.Join(sortedNames(krateDreiNames, true), ", "))
				}
				continue
			}
			for _, n := range names {
				scan.uses = append(scan.uses, threeImportUse{module: spec, name: n})
			}
		default:
			pkg := bareSpecifierPackage(spec)
			if pkg != "" && pkg != fiberModule && pkg != dreiModule && (strings.HasPrefix(pkg, "@react-three/") || fiberBased(pkg)) {
				scan.blockers = append(scan.blockers, "imports "+pkg+", which is built on @react-three/fiber")
			}
		}
	}
	if regexp.MustCompile(`["']` + regexp.QuoteMeta(dreiModule) + `/`).MatchString(src) {
		scan.blockers = append(scan.blockers, "imports a @react-three/drei subpath; @krate/three/drei provides only the package root")
	}
	for _, u := range scan.uses {
		switch u.module {
		case fiberModule:
			if !krateThreeFiberNames[u.name] {
				reason := "@krate/three does not export it"
				if why, ok := fiberUnsupported[u.name]; ok {
					reason = why
				}
				scan.blockers = append(scan.blockers, "uses "+u.name+" from @react-three/fiber: "+reason)
			}
		case dreiModule:
			if !krateDreiNames[u.name] {
				scan.blockers = append(scan.blockers, "uses "+u.name+" from @react-three/drei, which @krate/three/drei does not provide (it has "+strings.Join(sortedNames(krateDreiNames, true), ", ")+")")
			} else if u.name == "Environment" {
				if m := dreiEnvironmentUnsupported.FindStringSubmatch(src); m != nil {
					scan.blockers = append(scan.blockers, "uses <Environment "+m[1]+">; @krate/three/drei's Environment renders children (Lightformers) or a map, without HDR files, presets or ground projection")
				}
			}
		}
	}
	return scan
}

// sortedNames lists a name set; values only drops the Props types.
func sortedNames(set map[string]bool, values bool) []string {
	out := make([]string, 0, len(set))
	for n := range set {
		if values && strings.HasSuffix(n, "Props") {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// fiberBasedPackages reports packages whose package.json depends on
// @react-three/fiber (directly or as a peer), found from root upwards.
func fiberBasedPackages(root string) func(pkg string) bool {
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
					_, dep := manifest.Dependencies[fiberModule]
					_, peer := manifest.PeerDependencies[fiberModule]
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

// migrateReactThree points @react-three/fiber and @react-three/drei imports at
// @krate/three when every module that uses them only uses what @krate/three
// provides, and reports what kept them on fiber otherwise.
func migrateReactThree(rep *Report, root string) {
	fiberBased := fiberBasedPackages(root)
	var scans []*threeModuleScan
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 || f.Kind == KindPublic || f.Kind == KindPackage || !isModuleSource(fileLabel(f)) {
			continue
		}
		src := string(f.Content)
		if !strings.Contains(src, "@react-three/") && !mentionsFiberBased(src, fiberBased) {
			continue
		}
		scan := scanThreeModule(f, src, fiberBased)
		if len(scan.uses) == 0 && len(scan.blockers) == 0 {
			continue
		}
		scans = append(scans, scan)
	}
	if len(scans) == 0 {
		return
	}
	usesFiber := false
	var blocked []*threeModuleScan
	for _, s := range scans {
		if len(s.uses) > 0 {
			usesFiber = true
		}
		if len(s.blockers) > 0 {
			blocked = append(blocked, s)
		}
	}
	if !usesFiber {
		return
	}
	if len(blocked) > 0 {
		for _, s := range blocked {
			for _, b := range s.blockers {
				rep.Manual = append(rep.Manual, ManualItem{
					Source: fileLabel(s.file),
					Reason: "@react-three/fiber stays (a real-React island that ships React and react-dom): this module " + b,
					Hint:   "replace that API (three.js classes work with extend() in @krate/three), then re-run the migration to move the 3D modules to @krate/three; see https://krate.js.org/docs/features/three/",
				})
			}
		}
		rep.Warnings = append(rep.Warnings, "kept @react-three/fiber in "+plural(len(scans), "module")+": "+plural(len(blocked), "module")+" use APIs @krate/three does not provide (listed under manual items)")
		return
	}
	var dreiUsed []string
	seenDrei := map[string]bool{}
	for _, s := range scans {
		var names []string
		for _, u := range s.uses {
			if u.module == dreiModule && !seenDrei[u.name] {
				seenDrei[u.name] = true
				dreiUsed = append(dreiUsed, u.name)
			}
			names = append(names, u.name)
		}
		src := string(s.file.Content)
		src = replaceModule(src, fiberModule, krateThreeModule)
		src = replaceModule(src, dreiModule, krateDreiModule)
		s.file.Content = []byte(src)
		s.file.Rewrites = append(s.file.Rewrites, "rewrote @react-three/fiber and @react-three/drei imports to @krate/three ("+strings.Join(uniqueSorted(names), ", ")+"): the scene compiles with the page instead of running in a real-React island")
	}
	sort.Strings(dreiUsed)
	msg := "moved " + plural(len(scans), "module") + " from @react-three/fiber to @krate/three"
	if len(dreiUsed) > 0 {
		msg += " and " + strings.Join(dreiUsed, ", ") + " from @react-three/drei to @krate/three/drei"
	}
	rep.Warnings = append(rep.Warnings, msg+"; keep @react-three/fiber and @react-three/drei installed while a real-React island renders a 3D module: the build runs @krate/three there as fiber")
	addKrateThreeDependency(rep, root)
}

func mentionsFiberBased(src string, fiberBased func(string) bool) bool {
	for _, m := range threeImportStmt.FindAllStringSubmatch(src, -1) {
		if pkg := bareSpecifierPackage(m[3]); pkg != "" && fiberBased(pkg) {
			return true
		}
	}
	return false
}

func fileLabel(f *PlannedFile) string {
	if f.Source != "" {
		return f.Source
	}
	return f.Target
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// addKrateThreeDependency adds @krate/three to the planned package.json, or
// asks for it when the migration does not update package.json.
func addKrateThreeDependency(rep *Report, root string) {
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
		if _, ok := deps[krateThreeModule]; ok {
			return
		}
		spec := "latest"
		if v, ok := deps["@krate/runtime"].(string); ok && v != "" {
			spec = v
		}
		deps[krateThreeModule] = spec
		pkg["dependencies"] = deps
		data, err := json.MarshalIndent(pkg, "", "  ")
		if err != nil {
			break
		}
		f.Content = append(data, '\n')
		f.Rewrites = append(f.Rewrites, "dependencies added: "+krateThreeModule+"@"+spec+" (replaces @react-three/fiber and @react-three/drei)")
		return
	}
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil && strings.Contains(string(data), `"`+krateThreeModule+`"`) {
		return
	}
	rep.Manual = append(rep.Manual, ManualItem{
		Source: "package.json",
		Reason: "the 3D modules now import @krate/three, which is not a dependency yet",
		Hint:   "pnpm add @krate/three (or re-run with --update-package)",
	})
}

// bareSpecifierPackage returns the package a bare import specifier names
// (`@scope/pkg/sub` -> `@scope/pkg`), or "" for relative, absolute, aliased
// (`@/x`, `~/x`) and node: specifiers.
func bareSpecifierPackage(spec string) string {
	if spec == "" || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "~") || strings.Contains(spec, ":") {
		return ""
	}
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") {
		if len(parts) < 2 || parts[0] == "@" {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}
