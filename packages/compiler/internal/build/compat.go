package build

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/csssignals"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// splitVariantCSS splits the hybrid stylesheet into modern-only and legacy-only
// variants at the sentinel-carrying @supports blocks, so UA-classified visitors
// download only their half.
func splitVariantCSS(pageCss string) (modern, legacy string) {
	modern = css.UnwrapAtRuleBlock(css.RemoveAtRuleBlock(pageCss, csssignals.CompatLegacyPrefix), csssignals.CompatModernPrefix)
	legacy = css.UnwrapAtRuleBlock(css.RemoveAtRuleBlock(pageCss, csssignals.CompatModernPrefix), csssignals.CompatLegacyPrefix)
	return modern, legacy
}

// noJSFallbackHTML wraps the reveal-all rule in <noscript> so it applies only
// when scripting is disabled. The inner rule is @supports-gated on :has(), so a
// modern browser with JS disabled keeps its native :has() behavior; only engines
// that lack BOTH :has() and JS fall back to revealing all panels.
func noJSFallbackHTML(revealCSS string) string {
	if revealCSS == "" {
		return ""
	}
	return "<noscript><style>" + revealCSS + "</style></noscript>"
}

// needsNoJSFallback reports whether the configured mode relies on the shim for
// interactivity (legacy/hybrid), in which case no-JS legacy engines need the
// reveal-all fallback. Modern mode emits the fallback inline already.
func (b *Builder) needsNoJSFallback() bool {
	return b.Cfg.Compat.ModeOrDefault() != "modern"
}

// compatStyleMode returns the configured CSS-signal stylesheet mode.
func (b *Builder) compatStyleMode() csssignals.StyleMode {
	return csssignals.ParseStyleMode(b.Cfg.Compat.ModeOrDefault())
}

// irBuildOptions returns the standard irtree build options for this build,
// including the compat style mode.
func (b *Builder) irBuildOptions() irtree.BuildOptions {
	return irtree.BuildOptions{
		CodeTheme:     b.Cfg.Markdown.CodeTheme,
		AggressiveDCE: b.aggressiveDCE(),
		Compat:        b.compatStyleMode(),
		EvalJS:        b.jsExprEvaluator(),
		TransformIcon: func(el *ast.JSXElement) *ast.JSXElement {
			if el == nil || el.Opening == nil || !iconNameIsLiteral(el) {
				return nil
			}
			return b.compileIconToSVG(el)
		},
		TransformImage: func(el *ast.JSXElement) *ast.JSXElement {
			if !imageAttrsAreLiteral(el) {
				return nil
			}
			return b.compileImageToPicture(el)
		},
	}
}

// legacyDocEnabled reports whether per-page UA sibling documents should be
// emitted (hybrid + compat.userAgent). The default document stays the full
// hybrid (works on any host); the siblings let `krate serve` send a smaller
// variant to visitors it can classify.
func (b *Builder) legacyDocEnabled() bool {
	return b.Cfg.Compat.ModeOrDefault() == "hybrid" && b.Cfg.Compat.UserAgent
}

// compatShimHTML returns the inline script that drives class-based CSS signals
// for the configured compat mode:
//   - modern: none (zero JS).
//   - legacy: unconditional (every browser uses the class stylesheet).
//   - hybrid: feature-gated (only browsers without :has() run it).
func (b *Builder) compatShimHTML() string {
	switch b.Cfg.Compat.ModeOrDefault() {
	case "legacy":
		return "<script>" + renderer.LegacyHasJS + "</script>"
	case "hybrid":
		return hybridShimScriptTag()
	}
	return ""
}

// hybridShimScriptTag is the feature-gated shim used by the default hybrid
// document. The UA siblings swap it for the unconditional shim (legacy) or drop
// it (modern).
func hybridShimScriptTag() string {
	return "<script>if(!(window.CSS&&CSS.supports&&CSS.supports('selector(:has(*))'))){" + renderer.LegacyHasJS + "}</script>"
}

// legacyShimScriptTag is the unconditional shim script for the legacy sibling.
func legacyShimScriptTag() string {
	return "<script>" + renderer.LegacyHasJS + "</script>"
}
