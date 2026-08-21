package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// CompileSSRPageBundles compiles SSR/ISR/streaming pages into self-contained IIFE bundles
// for the embedded QuickJS runtime. Unlike CompileServerBundles (which produces ESM with
// @krate/runtime externals for Node.js), these bundles include renderToString and all
// server runtime functions inline so they can execute in QuickJS without module resolution.
//
// Each bundle defines:
//   - __krate_renderPage(propsJSON) → HTML string
//   - __krate_resetBoundaryCounter / __krate_setStreamingResolved (streaming SSR)
func CompileSSRPageBundles(results []*PageResult, root, outDir string) map[string]string {
	var ssrPages []*PageResult
	for _, r := range results {
		if r.Mode != RenderSSG && r.SourcePath != "" {
			ssrPages = append(ssrPages, r)
		}
	}
	if len(ssrPages) == 0 {
		return nil
	}

	bundleDir := filepath.Join(outDir, ".krate", "ssr-bundles")
	_ = os.MkdirAll(bundleDir, 0755)

	bundles := make(map[string]string, len(ssrPages))

	for _, page := range ssrPages {
		absSource := filepath.Join(root, page.SourcePath)
		sourceData, err := os.ReadFile(absSource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %sWarning: cannot read %s for SSR bundle:%s %v\n", cYellow, page.SourcePath, cReset, err)
			continue
		}

		injectedSource := injectServerGlobals(string(sourceData))

		route := page.OutName
		if route == "." || route == "" {
			route = "index"
		}
		outName := strings.TrimPrefix(route, "/") + ".ssr.js"
		outPath := filepath.Join(bundleDir, outName)

		// Write shim to a temp file for the esbuild plugin
		shimPath := filepath.Join(bundleDir, "_tmp_shim.js")
		if err := os.WriteFile(shimPath, []byte(ssrPageShimCode), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  %sSSR bundle shim write error:%s %v\n", cRed, cReset, err)
			continue
		}

		// Write temp source NEXT TO the original source file so relative imports resolve correctly.
		// e.g. src/pages/foo.tsx → src/pages/_tmp_ssr_foo.tsx
		sourceDir := filepath.Dir(absSource)
		tmpBase := "_tmp_ssr_" + filepath.Base(absSource)
		tmpSource := filepath.Join(sourceDir, tmpBase)
		if err := os.WriteFile(tmpSource, []byte(injectedSource), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  %sSSR bundle source write error:%s %v\n", cRed, cReset, err)
			continue
		}

		// Create wrapper entry that imports the temp source and exposes __krate_* globals.
		// The wrapper is passed via Stdin with ResolveDir = sourceDir so all relative
		// imports resolve from the source file's directory.
		wrapperCode := fmt.Sprintf(`import * as __mod from './%s';
var __Comp = __mod.default;
if (!__Comp) {
  var __keys = Object.keys(__mod);
  for (var i = 0; i < __keys.length; i++) {
    if (typeof __mod[__keys[i]] === 'function') { __Comp = __mod[__keys[i]]; break; }
  }
}
if (!__Comp) throw new Error('Page must export a default component function');

globalThis.__krate_resetBoundaryCounter = function() { globalThis.__krate_boundary_counter = 0; };
globalThis.__krate_setStreamingResolved = function(v) { globalThis.__krate_streaming_resolved = v; };

globalThis.__krate_renderPage = function(__propsJSON) {
  var __props = typeof __propsJSON === 'string' ? JSON.parse(__propsJSON) : (__propsJSON || {});
  var __result = __Comp(__props);
  if (typeof __result === 'string') return __result;
  if (__result && typeof __result.__html === 'string') return __result.__html;
  return '';
};
`, tmpBase)

		// Compile with esbuild: IIFE format, all deps bundled, JSX automatic transform.
		// Stdin + ResolveDir ensures relative imports resolve from the source directory.
		result := api.Build(api.BuildOptions{
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
			Stdin: &api.StdinOptions{
				Contents:   wrapperCode,
				ResolveDir: sourceDir,
				Sourcefile: "_tmp_wrapper.mjs",
				Loader:     api.LoaderJS,
			},
			Plugins: []api.Plugin{
				{
					Name: "krate-ssr-shim",
					Setup: func(build api.PluginBuild) {
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

		os.Remove(tmpSource)

		if len(result.Errors) > 0 {
			for _, e := range result.Errors {
				fmt.Fprintf(os.Stderr, "  %sSSR bundle error (%s):%s %s\n", cYellow, page.SourcePath, cReset, e.Text)
			}
			continue
		}

		info, err := os.Stat(outPath)
		if err != nil || info.Size() == 0 {
			fmt.Fprintf(os.Stderr, "  %sSSR bundle output missing or empty (%s):%s\n", cYellow, page.SourcePath, cReset)
			continue
		}

		relBundle, _ := filepath.Rel(outDir, outPath)
		bundles[page.SourcePath] = relBundle
	}

	os.Remove(filepath.Join(bundleDir, "_tmp_shim.js"))

	return bundles
}

// ssrPageShimCode is the server-side runtime for QuickJS-rendered pages.
// It provides renderToString (JSX → HTML), server component stubs (Head, Suspense, etc.),
// streaming SSR markers, and stubs for client-only APIs.
// This is bundled INTO each SSR page bundle so QuickJS needs no module resolution.
const ssrPageShimCode = escapeHTMLShimJS + `function __renderChildren(c){
if(c==null||c===false||c===true)return'';
if(Array.isArray(c)){var h='';for(var i=0;i<c.length;i++)h+=__renderChildren(c[i]);return h;}
if(typeof c==='object'){
if(typeof c.__html==='string')return c.__html;
if(typeof c.__raw==='string')return c.__raw;
if(typeof c.type==='string'||typeof c.type==='function')return __renderElement(c);
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
var __VOID=new Set(["area","base","br","col","embed","hr","img","input","link","meta","param","source","track","wbr"]);
function __renderElement(el){
var type=el.type,props=el.props||{},children=el.children||[];
if(typeof type==='function')return __renderChildren(type(Object.assign({},props,{children:children})));
var tag=type;var attrs=__renderProps(props);
if(__VOID.has(tag))return'<'+tag+attrs+'>';
var inner='';for(var i=0;i<children.length;i++)inner+=__renderChildren(children[i]);
return'<'+tag+attrs+'>'+inner+'</'+tag+'>';
}
function jsx(tag,props,key){
props=props||{};var ch=props.children!=null?(Array.isArray(props.children)?props.children:[props.children]):[];
return{type:tag,props:props,children:ch};
}
function jsxs(tag,props,key){return jsx(tag,props,key);}
var Fragment={__fragment:true};
function renderToString(node){
if(node==null||node===false)return'';
if(typeof node==='string')return __escapeHtml(node);
if(typeof node==='number')return String(node);
if(typeof node==='boolean')return'';
if(Array.isArray(node)){var h='';for(var i=0;i<node.length;i++)h+=renderToString(node[i]);return h;}
if(typeof node==='object'){
if(typeof node.__html==='string')return node.__html;
if(typeof node.__raw==='string')return node.__raw;
if(typeof node.type==='string'||typeof node.type==='function')return __renderElement(node);
return'';
}
return'';
}
function createSignal(initial){var v=initial;return[function(){return v;},function(n){v=n;}];}
function createEffect(fn){return function(){};}
function createMemo(fn){return fn;}
function onMount(fn){try{fn();}catch(e){}}
function onCleanup(fn){}
function createContext(def){return{Provider:function(p){return p.children;},useContext:function(){return def;},defaultValue:def};}
function createResource(){return[function(){return undefined;},{mutate:function(){},refetch:function(){}}];}
function h(tag,props){var ch=[];for(var i=2;i<arguments.length;i++)ch.push(arguments[i]);props=props||{};props.children=ch.length<=1?ch[0]:ch;return jsx(tag,props);}
function mount(){}
function hydrate(){}
function useRef(){return{current:null};}
function useCallback(fn){return fn;}
function forwardRef(fn){return fn;}
function createElement(tag,props){var ch=[];for(var i=2;i<arguments.length;i++)ch.push(arguments[i]);props=props||{};props.children=ch.length<=1?ch[0]:ch;return jsx(tag,props);}
function Head(p){return{type:'head',props:p||{},children:p&&p.children?[p.children]:[]};}
function Suspense(props){
var id=0;
if(typeof globalThis.__krate_boundary_counter==='undefined')globalThis.__krate_boundary_counter=0;
id=globalThis.__krate_boundary_counter++;
if(globalThis.__krate_streaming_resolved){
var inner='';var ch=props&&props.children!=null?(Array.isArray(props.children)?props.children:[props.children]):[];
for(var i=0;i<ch.length;i++)inner+=renderToString(ch[i]);
return{type:'raw',props:{html:'<!--suspense-resolved:'+id+'-->'+inner+'<!--/suspense-resolved:'+id+'-->'}};
}
var inner='';var ch=props&&props.children!=null?(Array.isArray(props.children)?props.children:[props.children]):[];
for(var i=0;i<ch.length;i++)inner+=renderToString(ch[i]);
var fallback='';var fb=props&&props.fallback!=null?(Array.isArray(props.fallback)?props.fallback:[props.fallback]):[];
for(var i=0;i<fb.length;i++)fallback+=renderToString(fb[i]);
return{type:'raw',props:{html:'<span data-suspense="'+id+'">'+fallback+'</span><template data-suspense="'+id+'">'+inner+'</template>'}};
}
function Script(p){if(p&&p.src)return{type:'raw',props:{html:'<script src="'+__escapeHtml(p.src)+'"></script>'}};return{type:'raw',props:{html:'<script>'+(p&&p.children||'')+'</script>'}};}
function Style(p){return{type:'raw',props:{html:'<style>'+(p&&p.children||'')+'</style>'}};}
function Link(p){var href=p&&p.href||'';var ch=p&&p.children!=null?(Array.isArray(p.children)?p.children:[p.children]):[];var inner='';for(var i=0;i<ch.length;i++)inner+=renderToString(ch[i]);var attrs=['<a href="'+__escapeHtml(href)+'"'];var ext=/^(https?:|mailto:|tel:|#|javascript:|\/\/)/.test(href)||(p&&p.target==='_blank')||(p&&p.download!==undefined);if(ext){attrs.push('data-krate-external');}else{attrs.push('data-krate-link');if(p&&p.prefetch!==false)attrs.push('data-prefetch');if(p&&p.replace)attrs.push('data-krate-replace');if(p&&p.scroll===false)attrs.push('data-krate-scroll="false"');}if(p&&p.target)attrs.push('target="'+__escapeHtml(p.target)+'"');if(p&&p.target==='_blank'&&!p.rel)attrs.push('rel="noopener noreferrer"');if(p&&p.rel)attrs.push('rel="'+__escapeHtml(p.rel)+'"');if(p&&p.className)attrs.push('class="'+__escapeHtml(p.className)+'"');return{type:'raw',props:{html:attrs.join(' ')+'>'+inner+'</a>'}};}
function Image(p){var src=p&&p.src||'';var alt=p&&p.alt||'';return{type:'raw',props:{html:'<img src="'+__escapeHtml(src)+'" alt="'+__escapeHtml(alt)+'" />'}};}
function Icon(){return{type:'raw',props:{html:''}};}
if(typeof globalThis!=='undefined'){
globalThis.__krate_jsx={jsx:jsx,jsxs:jsxs,Fragment:Fragment};
globalThis.__krate_runtime={createSignal:createSignal,createEffect:createEffect,createMemo:createMemo,onMount:onMount,onCleanup:onCleanup,createContext:createContext,createResource:createResource,h:h,mount:mount,hydrate:hydrate,useRef:useRef,useCallback:useCallback,forwardRef:forwardRef,createElement:createElement};
globalThis.__krate_server={Head:Head,Suspense:Suspense,Script:Script,Style:Style,Link:Link,Image:Image,Icon:Icon,renderToString:renderToString};
}
export {jsx,jsxs,Fragment,createSignal,createEffect,createMemo,onMount,onCleanup,createContext,createResource,h,mount,hydrate,Head,Suspense,Script,Style,Link,Image,Icon,useRef,useCallback,forwardRef,createElement,renderToString};
export default jsx;
`
