package diag

// Async server components and RSC boundaries (KR71xx, part of the KR7xxx
// server range).
const (
	// CodeAsyncLayout: an async layout used where it cannot render per request.
	CodeAsyncLayout Code = "KR7101"
	// CodeBuildTimeAwait: `await` reached the build-time evaluator.
	CodeBuildTimeAwait Code = "KR7102"
	// CodeServerComponentStaticOutput: server components with output "static".
	CodeServerComponentStaticOutput Code = "KR7103"
	// CodeServerComponentStreaming: a static page with server components streams.
	CodeServerComponentStreaming Code = "KR7104"
	// CodeServerComponentProps: a server component region needs build-time props.
	CodeServerComponentProps Code = "KR7105"
	// CodeClientAPIInServer: hooks, signals or event handlers in a server tree.
	CodeClientAPIInServer Code = "KR7110"
	// CodeClientPropNotSerializable: a function prop crosses to a client component.
	CodeClientPropNotSerializable Code = "KR7111"
	// CodeServerOnlyInClient: a client boundary imports a server-only module.
	CodeServerOnlyInClient Code = "KR7112"
	// CodeAsyncClientComponent: an async component in a "use client" module.
	CodeAsyncClientComponent Code = "KR7113"
	// CodeClientBoundaryExports: a "use client" module's exports cannot be listed.
	CodeClientBoundaryExports Code = "KR7114"
	// CodeTierDecorator: a tier decorator cannot apply where it is placed.
	CodeTierDecorator Code = "KR7115"
	// CodeExportCollision: a page imports one name from two modules.
	CodeExportCollision Code = "KR7116"
	// CodeAPIRouteInPages: a .ts or .js file under src/pages/api is not served.
	CodeAPIRouteInPages Code = "KR7120"
	// CodePagesEndpoint: a prerendered endpoint (src/pages/data.json.ts) failed.
	CodePagesEndpoint Code = "KR7121"
	// CodeMetadataNotStatic: a page's metadata export has no build-time value.
	CodeMetadataNotStatic Code = "KR7122"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeAPIRouteInPages,
			Title: "API route file in the pages directory",
			Explanation: "API routes are served from src/api, not from the pages directory. A `.ts` or `.js` " +
				"file under src/pages/api used to build into an empty page while /api/... answered 502. Move " +
				"the file to src/api/. This is a warning.",
		},
		CodeInfo{
			Code:  CodeMetadataNotStatic,
			Title: "Page metadata could not be evaluated at build time",
			Explanation: "A static page's `export const metadata` and `generateMetadata` (and those of the layouts above " +
				"it) run at build time, once per `generateStaticParams` entry, and become the document `<title>`, " +
				"description, canonical link, Open Graph and Twitter tags. This one could not run: it threw, it " +
				"needs something the build does not have (a browser global, a service that is down), or the module " +
				"could not be loaded. The segment's tags were left out and the page still builds. Fix the error, " +
				"or render the page per request (`export const dynamic = \"force-dynamic\"`, or `revalidate = N`) so " +
				"`generateMetadata` runs with the request. This is a warning.",
		},
		CodeInfo{
			Code:  CodePagesEndpoint,
			Title: "Prerendered endpoint failed",
			Explanation: "A file such as `src/pages/data.json.ts` is a prerendered endpoint: its `GET` export runs " +
				"once at build time and the response body is written to the output as a file (`data.json`). " +
				"The build fails when the module has no `GET` export, `GET` throws, answers a non-2xx status, or the " +
				"name has a dynamic segment. Endpoints run on Node (or the configured runtime) with the " +
				"project's environment. For a response that depends on the request, use an API route in `src/api`.",
		},
		CodeInfo{
			Code:  CodeExportCollision,
			Title: "One name imported from two modules",
			Explanation: "Two modules export the same name and a page imports it from both, for example " +
				"`import { Card as A } from \"./a\"` and `import { Card as B } from \"./b\"`. Modules on a page " +
				"share one namespace, so Krate gives one of the exports a private name and rewrites the " +
				"imports to match, and this is not reported. A named re-export (`export { Card } from`) is " +
				"rewritten to follow the rename too. It is reported when neither module can be " +
				"renamed because something reads export names from source: `export *` or `export * as`, " +
				"an import followed by an export, the page's own module, an `import()`, a \"use client\" or " +
				"\"use server\" module, or an alias export (`export { x as Card }`). Both imports " +
				"then resolve to one declaration and the page shows the wrong output. Rename one of the " +
				"exports. Private names and default exports do not have this problem. This is a warning.",
		},
		CodeInfo{
			Code:  CodeTierDecorator,
			Title: "Tier decorator cannot apply here, or two tiers conflict",
			Explanation: "`@server`, `@static`, `@runtime` and `@client` go on a function, class or const " +
				"declaration, once per declaration. An async component always renders per request, so only " +
				"`@runtime` fits it. A runtime component is bundled per file, so `@runtime` must be on the " +
				"default export or the only exported component of its file. A component also has one tier: the " +
				"leading `// @tier` comment, the `.server`/`.runtime`/`.static` file name, the `serverComponents`, " +
				"`runtimeComponents`, `serverDirs` and `runtimeDirs` config entries, a decorator and a `\"@tier\"` " +
				"string at the top of a function body must agree. A decorator on a component that is not the " +
				"file's default export or only exported component may override the file's tier (`@client` in a " +
				"`// @server` file); on the default export it must match. See \"Tiers that disagree\" in " +
				"Component Tiers.",
		},
		CodeInfo{
			Code:  CodeAsyncLayout,
			Title: "Async layout cannot be used here",
			Explanation: "An async layout renders per request in the SSR sidecar, with the page and the " +
				"layouts inside it, instead of being baked into the static shell. It can await data and " +
				"gate its subtree with redirect() or notFound(). An error.tsx below it is the error boundary " +
				"of the page and renders on the server when the page throws. It cannot sit above a " +
				"template.tsx in the same route (a template hydrates in the browser), and a layout that is " +
				"baked at build time for another reason (an intercepting or segment route) cannot be " +
				"async. Move the template above the async layout, or make the layout synchronous and " +
				"fetch data in an async child component.",
		},
		CodeInfo{
			Code:  CodeBuildTimeAwait,
			Title: "`await` cannot run at build time",
			Explanation: "Only async components (server components) can await: they render per request in the " +
				"SSR sidecar. Code that Krate evaluates at build time, such as module-level constants a " +
				"static component renders or a non-async component body, cannot await a Promise. Mark the " +
				"component `async` or compute the value without awaiting.",
		},
		CodeInfo{
			Code:  CodeServerComponentStaticOutput,
			Title: "Server components need request-time rendering",
			Explanation: "An async component renders per request, but `output: \"static\"` disables the SSR " +
				"sidecar, so the page cannot render it. Remove `output: \"static\"` or make the component " +
				"synchronous.",
		},
		CodeInfo{
			Code:  CodeServerComponentStreaming,
			Title: "Page with server components is rendered per request",
			Explanation: "A page that contains an async component cannot be fully baked at build time. Krate " +
				"bakes the rest of the page as a static shell and renders each server component per request " +
				"(streaming mode). This is informational.",
		},
		CodeInfo{
			Code:  CodeServerComponentProps,
			Title: "Server component props must be known at build time in a static page",
			Explanation: "A server component used from a synchronous page renders as its own request-time " +
				"region, so its props are fixed when the page shell is built: they must be literals, " +
				"module constants or route params, and it cannot receive children. Make the page itself " +
				"`async` (the whole page then renders per request) or pass constant props.",
		},
		CodeInfo{
			Code:  CodeClientAPIInServer,
			Title: "Client-only API used in a server component",
			Explanation: "Server, static and runtime components, and the components an async component renders, render " +
				"once (at build time or per request) and ship no JavaScript, so hooks such as useState/useEffect, " +
				"Krate signals, class state and lifecycles, and event handlers cannot work there; they used to build " +
				"and leave a page that did nothing. Remove the server tier from the component (the `// @server` " +
				"comment or decorator, the `.server` file name or the config entry) to make it a client component, or " +
				"move the interactive part into a client child component (one with no tier, or marked `@client`, or " +
				"a module that starts with `\"use client\"` under an async component) and render that from the server " +
				"component; it is server-rendered and then hydrated in the browser.",
		},
		CodeInfo{
			Code:  CodeClientPropNotSerializable,
			Title: "Function prop passed from a server component to a client component",
			Explanation: "Props crossing from a server component to a `\"use client\"` component are serialized " +
				"to JSON. Functions (other than server actions), class instances and symbols cannot be sent. " +
				"Define the handler inside the client component, or pass a server action. The same check runs " +
				"at request time for values computed while rendering.",
		},
		CodeInfo{
			Code:  CodeServerOnlyInClient,
			Title: "Client component imports a server-only module",
			Explanation: "A module that imports `server-only` must never reach the browser, but a " +
				"`\"use client\"` module (directly or through its imports) imports it, so it would be bundled " +
				"into the client chunk. Import it from a server component instead and pass the data as props.",
		},
		CodeInfo{
			Code:  CodeAsyncClientComponent,
			Title: "Async component in a client module",
			Explanation: "Components in a `\"use client\"` module run in the browser and cannot be async. " +
				"Move the async component to a module without the directive (it becomes a server component).",
		},
		CodeInfo{
			Code:  CodeClientBoundaryExports,
			Title: "Client module exports cannot be listed",
			Explanation: "Krate wraps each export of a `\"use client\"` module in a client boundary, so it must " +
				"know the export names. `export * from` cannot be enumerated: list the re-exported names " +
				"explicitly (`export { A, B } from \"./x\"`).",
		},
	)
}
