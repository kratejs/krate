package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// CompileServerBundles compiles SSR/ISR/streaming pages into self-contained ESM bundles
// that the Node.js renderer can import. Each bundle is compiled with:
//   - JSX automatic transform using @krate/runtime/jsx-runtime
//   - Head, Suspense, Script, Style imported from @krate/runtime/server
//   - All dependencies bundled (except node_modules externals)
func CompileServerBundles(results []*PageResult, root, outDir string) map[string]string {
	// Collect non-SSG pages
	var ssrPages []*PageResult
	for _, r := range results {
		if r.Mode != RenderSSG && r.SourcePath != "" {
			ssrPages = append(ssrPages, r)
		}
	}
	if len(ssrPages) == 0 {
		return nil
	}

	bundleDir := filepath.Join(outDir, ".krate", "server-bundles")
	_ = os.MkdirAll(bundleDir, 0755)

	bundles := make(map[string]string, len(ssrPages))

	for _, page := range ssrPages {
		absSource := filepath.Join(root, page.SourcePath)
		sourceData, err := os.ReadFile(absSource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %sWarning: cannot read %s for server bundle:%s %v\n", cYellow, page.SourcePath, cReset, err)
			continue
		}

		// Inject Head/Suspense/Script/Style imports at the top of the file
		// so pages can use them as globals (matching krate compiler behavior)
		injectedSource := injectServerGlobals(string(sourceData))

		// Compile with esbuild — use stdin so esbuild resolves relative imports
		// from the project root (AbsWorkingDir) rather than a temp file location
		route := page.OutName
		if route == "." || route == "" {
			route = "index"
		}
		outName := strings.TrimPrefix(route, "/") + ".server.mjs"
		outPath := filepath.Join(bundleDir, outName)

		result := api.Build(api.BuildOptions{
			AbsWorkingDir:   root,
			Bundle:          true,
			Format:          api.FormatESModule,
			Platform:        api.PlatformNode,
			Outfile:         outPath,
			Write:           true,
			JSX:             api.JSXAutomatic,
			JSXSideEffects:  false,
			JSXImportSource: "@krate/runtime/server",
			TsconfigRaw:     `{ "compilerOptions": { "jsx": "react-jsx", "jsxImportSource": "@krate/runtime/server" } }`,
			Packages:        api.PackagesExternal,
			External: []string{
				"@krate/runtime",
				"@krate/runtime/*",
			},
			Stdin: &api.StdinOptions{
				Loader:     api.LoaderTSX,
				Contents:   injectedSource,
				ResolveDir: filepath.Dir(absSource),
				Sourcefile: filepath.Base(absSource),
			},
		})

		if len(result.Errors) > 0 {
			for _, e := range result.Errors {
				fmt.Fprintf(os.Stderr, "  %sServer bundle error (%s):%s %s\n", cYellow, page.SourcePath, cReset, e.Text)
			}
			continue
		}

		relBundle, _ := filepath.Rel(outDir, outPath)
		bundles[page.SourcePath] = relBundle
	}

	return bundles
}

// injectServerGlobals prepends imports for Head, Suspense, Script, Style, Link, Image, Icon
// so TSX pages can use them as globals (matching krate's compiler behavior).
func injectServerGlobals(source string) string {
	needsHead := strings.Contains(source, "<Head") || strings.Contains(source, "Head>")
	needsSuspense := strings.Contains(source, "<Suspense") || strings.Contains(source, "Suspense>")
	needsScript := strings.Contains(source, "<Script") || strings.Contains(source, "Script>")
	needsStyle := strings.Contains(source, "<Style") || strings.Contains(source, "Style>")
	needsLink := strings.Contains(source, "<Link") || strings.Contains(source, "Link>")
	needsImage := strings.Contains(source, "<Image") || strings.Contains(source, "Image>")
	needsIcon := strings.Contains(source, "<Icon") || strings.Contains(source, "Icon>")

	var names []string
	if needsHead {
		names = append(names, "Head")
	}
	if needsSuspense {
		names = append(names, "Suspense")
	}
	if needsScript {
		names = append(names, "Script")
	}
	if needsStyle {
		names = append(names, "Style")
	}
	if needsLink {
		names = append(names, "Link")
	}
	if needsImage {
		names = append(names, "Image")
	}
	if needsIcon {
		names = append(names, "Icon")
	}

	if len(names) == 0 {
		return source
	}

	importLine := fmt.Sprintf("import { %s } from '@krate/runtime/server';", strings.Join(names, ", "))
	return importLine + "\n" + source
}
