package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"krate-compiler/internal/bundler"
)

// RuntimeComponentBundle represents a compiled runtime server component
// that can be executed at serve time via the embedded quickjs runtime.
type RuntimeComponentBundle struct {
	Name       string // Component name derived from filename (e.g. "Counter")
	SourcePath string // Absolute path to original source file
	BundlePath string // Path to compiled JS relative to outDir (e.g. "server-components/Counter.runtime.js")
}

// jsxShimCode is the server-side JSX runtime that serializes JSX to HTML strings.
// It replaces @krate/runtime/jsx-runtime and all @krate/runtime/* imports
// with lightweight implementations suitable for quickjs execution.
const jsxShimCode = escapeHTMLShimJS + `function __renderChildren(c){
if(c==null||c===false||c===true)return'';
if(Array.isArray(c)){var h='';for(var i=0;i<c.length;i++)h+=__renderChildren(c[i]);return h;}
if(typeof c==='object'){
if(typeof c.__html==='string')return c.__html;
if(typeof c==='function')return c();
return'';
}
return String(c);
}
function __renderProps(p){
var parts=[];var keys=Object.keys(p);
for(var i=0;i<keys.length;i++){
var k=keys[i];if(k==='children'||k==='key'||k==='ref')continue;
var v=p[k];if(v==null||v===false)continue;
if(k==='className')k='class';
else if(k==='htmlFor')k='for';
else if(k==='tabIndex')k='tabindex';
else if(k==='style'&&typeof v==='object'){
var sp=[];var sk=Object.keys(v);for(var j=0;j<sk.length;j++)sp.push(sk[j]+':'+v[sk[j]]);v=sp.join(';');
}
if(v===true)parts.push(' '+k);
else parts.push(' '+k+'="'+__escapeHtml(v)+'"');
}
return parts.join('');
}
function jsx(tag,props,key){
if(typeof tag==='function')return tag(props||{});
props=props||{};var ch=props.children;
var a=__renderProps(props);var c=__renderChildren(ch);
return '<'+tag+a+'>'+c+'</'+tag+'>';
}
function jsxs(tag,props,key){return jsx(tag,props,key);}
var Fragment={__fragment:true};
function createSignal(initial){var v=initial;return[function(){return v;},function(n){v=n;}];}
function createEffect(){}
function createMemo(fn){return fn;}
function onMount(){}
function onCleanup(){}
function createContext(def){return{Provider:function(p){return p.children;},useContext:function(){return def;},defaultValue:def};}
function createResource(){return[function(){return undefined;},{mutate:function(){},refetch:function(){}}];}
function h(tag,props){var ch=[];for(var i=2;i<arguments.length;i++)ch.push(arguments[i]);props=props||{};props.children=ch.length<=1?ch[0]:ch;return jsx(tag,props);}
function mount(){}
function hydrate(){}
function Head(p){return jsx('head',p);}
function Suspense(p){return p.children;}
function Script(){return'';}
function Style(){return'';}
function Link(p){p=p||{};var attrs={};for(var k in p)attrs[k]=p[k];var href=p.href||'';var ext=/^(https?:|mailto:|tel:|#|javascript:|\/\/)/.test(href)||p.target==='_blank'||p.download!==undefined;if(ext){attrs['data-krate-external']='';}else{attrs['data-krate-link']='';if(p.prefetch!==false)attrs['data-prefetch']='';if(p.replace)attrs['data-krate-replace']='';if(p.scroll===false)attrs['data-krate-scroll']='false';}if(p.target==='_blank'&&!p.rel)attrs.rel='noopener noreferrer';return jsx('a',attrs);}
function Image(p){return jsx('img',p);}
function Icon(){return'';}
function useRef(initial){return{current:initial===undefined?null:initial};}
function useCallback(fn){return fn;}
function forwardRef(fn){return fn;}
function createElement(tag,props){var ch=[];for(var i=2;i<arguments.length;i++)ch.push(arguments[i]);props=props||{};props.children=ch.length<=1?ch[0]:ch;return jsx(tag,props);}
if(typeof globalThis!=='undefined'){
globalThis.__krate_jsx={jsx:jsx,jsxs:jsxs,Fragment:Fragment};
globalThis.__krate_runtime={createSignal:createSignal,createEffect:createEffect,createMemo:createMemo,onMount:onMount,onCleanup:onCleanup,createContext:createContext,createResource:createResource,h:h,mount:mount,hydrate:hydrate,useRef:useRef,useCallback:useCallback,forwardRef:forwardRef,createElement:createElement};
globalThis.__krate_server={Head:Head,Suspense:Suspense,Script:Script,Style:Style,Link:Link,Image:Image,Icon:Icon};
}
export {jsx,jsxs,Fragment,createSignal,createEffect,createMemo,onMount,onCleanup,createContext,createResource,h,mount,hydrate,Head,Suspense,Script,Style,Link,Image,Icon,useRef,useCallback,forwardRef,createElement};
export default jsx;
`

// CompileRuntimeComponents finds and compiles all runtime component files
// (by *.runtime.tsx convention, // @runtime directive, or runtimeDirs config)
// into standalone JS bundles executable via quickjs at serve time.
func CompileRuntimeComponents(root, outDir string, serverComponents, runtimeComponents []string, runtimeDirs []string) []RuntimeComponentBundle {
	files := findRuntimeComponentFiles(root, serverComponents, runtimeComponents, runtimeDirs)
	if len(files) == 0 {
		return nil
	}

	bundleDir := filepath.Join(outDir, "server-components")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "  %s✗ Runtime components: cannot create output dir:%s %v\n", cRed, cReset, err)
		return nil
	}

	var bundles []RuntimeComponentBundle
	for _, file := range files {
		name := extractBuildComponentName(file)
		fmt.Printf("  %s▸ Runtime%s %s\n", cCyan, cReset, filepath.Base(file))
		bundle, err := compileSingleRuntimeComponent(root, outDir, file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s✗ Runtime component error (%s):%s %v\n", cRed, name, cReset, err)
			continue
		}
		bundles = append(bundles, *bundle)
	}

	return bundles
}

// findRuntimeComponentFiles walks src/ (and pagesDir) to find files that are
// runtime components by convention (*.runtime.tsx), directive (// @runtime),
// config list (runtimeComponents), or directory membership (runtimeDirs).
func findRuntimeComponentFiles(root string, serverComponents, runtimeComponents []string, runtimeDirs []string) []string {
	dirs := []string{filepath.Join(root, "src")}
	seen := make(map[string]bool)
	var files []string

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".tsx" && ext != ".ts" && ext != ".jsx" && ext != ".js" {
				return nil
			}
			base := filepath.Base(path)
			if strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
				return nil
			}
			if seen[path] {
				return nil
			}

			// 1. File convention check
			if bundler.IsRuntimeComponentFile(path) {
				seen[path] = true
				files = append(files, path)
				return nil
			}

			// 2. Directive check
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			src := string(data)
			if bundler.HasRuntimeDirective(src) {
				seen[path] = true
				files = append(files, path)
				return nil
			}

			// 3. Config list check (component name match)
			compName := extractBuildComponentName(path)
			for _, name := range runtimeComponents {
				if name == compName || name == path {
					seen[path] = true
					files = append(files, path)
					return nil
				}
			}

			// 4. Directory membership check
			if isPathInDirs(path, runtimeDirs) {
				seen[path] = true
				files = append(files, path)
				return nil
			}

			return nil
		})
	}

	return files
}

// isPathInDirs checks if a file path is contained within any of the given directories.
func isPathInDirs(filePath string, dirs []string) bool {
	if len(dirs) == 0 {
		return false
	}
	normalized := filepath.ToSlash(filePath)
	for _, dir := range dirs {
		d := filepath.ToSlash(filepath.Clean(dir))
		if !strings.HasSuffix(d, "/") {
			d += "/"
		}
		if strings.HasPrefix(normalized, d) {
			return true
		}
	}
	return false
}

// compileSingleRuntimeComponent compiles one runtime component source file into
// a self-contained IIFE bundle that defines globalThis.__krate_render(propsJSON).
func compileSingleRuntimeComponent(root, outDir, sourcePath string) (*RuntimeComponentBundle, error) {
	bundleDir := filepath.Join(outDir, "server-components")
	componentName := extractBuildComponentName(sourcePath)
	outPath := filepath.Join(bundleDir, componentName+".runtime.js")

	// Read source and inject server component globals (Head, Suspense, etc.)
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("reading source: %w", err)
	}
	injectedSource := injectServerGlobals(string(sourceData))

	// Write injected source to temp file for esbuild
	tmpSource := filepath.Join(bundleDir, "_tmp_"+filepath.Base(sourcePath))
	if err := os.WriteFile(tmpSource, []byte(injectedSource), 0644); err != nil {
		return nil, fmt.Errorf("writing temp source: %w", err)
	}
	defer os.Remove(tmpSource)

	// Create a wrapper entry that imports the component and exposes __krate_render.
	// The wrapper handles both default exports and named exports.
	wrapperCode := fmt.Sprintf(`import * as __mod from %q;
var __Comp = __mod.default;
if (!__Comp) {
  var __keys = Object.keys(__mod);
  for (var i = 0; i < __keys.length; i++) {
    if (typeof __mod[__keys[i]] === 'function') { __Comp = __mod[__keys[i]]; break; }
  }
}
if (!__Comp) throw new Error('Runtime component must export a default function or a named function');
globalThis.__krate_render = function(__propsJSON) {
  var __props = typeof __propsJSON === 'string' ? JSON.parse(__propsJSON) : (__propsJSON || {});
  var __result = __Comp(__props);
  if (typeof __result === 'string') return __result;
  if (__result && typeof __result.__html === 'string') return __result.__html;
  return '';
};
`, tmpSource)

	wrapperPath := filepath.Join(bundleDir, "_tmp_wrapper_"+componentName+".mjs")
	if err := os.WriteFile(wrapperPath, []byte(wrapperCode), 0644); err != nil {
		return nil, fmt.Errorf("writing wrapper: %w", err)
	}
	defer os.Remove(wrapperPath)

	// Write the shim to a temp file for the esbuild plugin
	shimPath := filepath.Join(bundleDir, "_tmp_shim.js")
	if err := os.WriteFile(shimPath, []byte(jsxShimCode), 0644); err != nil {
		return nil, fmt.Errorf("writing shim: %w", err)
	}
	defer os.Remove(shimPath)

	// Compile with esbuild: IIFE format, all deps bundled, JSX automatic transform
	result := api.Build(api.BuildOptions{
		EntryPoints:       []string{wrapperPath},
		Bundle:            true,
		Format:            api.FormatIIFE,
		Platform:          api.PlatformBrowser,
		Outfile:           outPath,
		Write:             true,
		LogLevel:          api.LogLevelError,
		JSX:               api.JSXAutomatic,
		JSXSideEffects:    false,
		JSXImportSource:   "@krate/runtime",
		TsconfigRaw:       `{ "compilerOptions": { "jsx": "react-jsx", "jsxImportSource": "@krate/runtime" } }`,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Plugins: []api.Plugin{
			{
				Name: "krate-runtime-shim",
				Setup: func(build api.PluginBuild) {
					// Redirect all @krate/runtime imports to our HTML-serializing shim
					build.OnResolve(api.OnResolveOptions{Filter: `^@krate/runtime`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
						return api.OnResolveResult{
							Path:      shimPath,
							Namespace: "krate-shim",
						}, nil
					})
					build.OnLoad(api.OnLoadOptions{Filter: `\.`, Namespace: "krate-shim"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
						data, err := os.ReadFile(args.Path)
						if err != nil {
							return api.OnLoadResult{}, err
						}
						content := string(data)
						return api.OnLoadResult{
							Contents: &content,
							Loader:   api.LoaderJS,
						}, nil
					})
				},
			},
		},
	})

	os.Remove(shimPath)

	if len(result.Errors) > 0 {
		var msgs []string
		for _, e := range result.Errors {
			msgs = append(msgs, e.Text)
		}
		return nil, fmt.Errorf("esbuild: %s", strings.Join(msgs, "; "))
	}

	// Verify the output file exists and has content
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		return nil, fmt.Errorf("bundle output missing or empty")
	}

	relPath, _ := filepath.Rel(outDir, outPath)
	return &RuntimeComponentBundle{
		Name:       componentName,
		SourcePath: sourcePath,
		BundlePath: relPath,
	}, nil
}
