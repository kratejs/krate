package renderer

import (
	"strings"

	"krate-compiler/internal/ast"
)

// ComponentTier classifies components into rendering strategies.
type ComponentTier int

const (
	TierClient   ComponentTier = iota // Default: full SSR + client hydration
	TierStatic                        // Compile-time only, no client JS
	TierServer                        // Build-time evaluation, HTML output only
	TierRuntime                       // Serve-time evaluation via QuickJS, streamed to client
)

// TierCommentPrefixes maps tier annotation comments to their tier.
var TierCommentPrefixes = map[string]ComponentTier{
	"@client":   TierClient,
	"@static":   TierStatic,
	"@server":   TierServer,
	"@runtime":  TierRuntime,
}

// ClassifyComponent determines a component's tier from its leading comments.
// The AST doesn't store comments directly, so this scans the first statement
// of the body for a string literal containing a tier annotation (e.g. "@static").
// For production use, the build system should call TierClassifier.Classify()
// based on config or file-level annotations.
func ClassifyComponent(fn *ast.FnDecl) ComponentTier {
	// Look for a string literal expression statement at the start of the body
	// as a pseudo-comment mechanism: `("@static")` or `("@server")`
	if len(fn.Body) > 0 {
		if exprStmt, ok := fn.Body[0].(*ast.ExprStmt); ok {
			if lit, ok := exprStmt.Expression.(*ast.Literal); ok && lit.Kind == ast.StringLit {
				val := strings.TrimSpace(lit.Value)
				for prefix, tier := range TierCommentPrefixes {
					if strings.Contains(val, prefix) {
						return tier
					}
				}
			}
		}
	}
	return TierClient
}

// IsStatic returns true if the component is compile-time only.
func (t ComponentTier) IsStatic() bool { return t == TierStatic }

// IsServer returns true if the component is evaluated at build time.
func (t ComponentTier) IsServer() bool { return t == TierServer }

// IsRuntime returns true if the component is evaluated at serve time via QuickJS.
func (t ComponentTier) IsRuntime() bool { return t == TierRuntime }

// IsClient returns true if the component needs client hydration.
func (t ComponentTier) IsClient() bool { return t == TierClient }

// String returns the tier name for debugging.
func (t ComponentTier) String() string {
	switch t {
	case TierStatic:
		return "static"
	case TierServer:
		return "server"
	case TierRuntime:
		return "runtime"
	default:
		return "client"
	}
}

// TierInfo holds classification metadata for a component during rendering.
type TierInfo struct {
	Tier       ComponentTier
	SourceFile string // file where the component is defined
	ComponentName string
}

// TierClassifier tracks component tier classifications across the build.
type TierClassifier struct {
	components map[string]*TierInfo
}

// NewTierClassifier creates a new classifier.
func NewTierClassifier() *TierClassifier {
	return &TierClassifier{
		components: make(map[string]*TierInfo),
	}
}

// Classify records a component's tier.
func (tc *TierClassifier) Classify(name string, tier ComponentTier, sourceFile string) {
	tc.components[name] = &TierInfo{
		Tier:       tier,
		SourceFile: sourceFile,
		ComponentName: name,
	}
}

// Get returns a component's tier info, or nil if not classified.
func (tc *TierClassifier) Get(name string) *TierInfo {
	return tc.components[name]
}

// GetTier returns a component's tier, defaulting to TierClient.
func (tc *TierClassifier) GetTier(name string) ComponentTier {
	if info, ok := tc.components[name]; ok {
		return info.Tier
	}
	return TierClient
}

// IsStatic checks if a named component is static.
func (tc *TierClassifier) IsStatic(name string) bool {
	return tc.GetTier(name) == TierStatic
}

// IsServer checks if a named component is build-time server.
func (tc *TierClassifier) IsServer(name string) bool {
	return tc.GetTier(name) == TierServer
}

// IsRuntime checks if a named component is serve-time runtime.
func (tc *TierClassifier) IsRuntime(name string) bool {
	return tc.GetTier(name) == TierRuntime
}

// All returns all classified components.
func (tc *TierClassifier) All() map[string]*TierInfo {
	return tc.components
}

// HasRuntimeComponents returns true if any component uses the runtime tier.
func (tc *TierClassifier) HasRuntimeComponents() bool {
	for _, info := range tc.components {
		if info.Tier == TierRuntime {
			return true
		}
	}
	return false
}

// HasServerComponents returns true if any component uses the server tier.
func (tc *TierClassifier) HasServerComponents() bool {
	for _, info := range tc.components {
		if info.Tier == TierServer {
			return true
		}
	}
	return false
}

// StaticComponentNames returns all static component names.
func (tc *TierClassifier) StaticComponentNames() []string {
	var names []string
	for name, info := range tc.components {
		if info.Tier == TierStatic {
			names = append(names, name)
		}
	}
	return names
}

// ServerComponentNames returns all build-time server component names.
func (tc *TierClassifier) ServerComponentNames() []string {
	var names []string
	for name, info := range tc.components {
		if info.Tier == TierServer {
			names = append(names, name)
		}
	}
	return names
}

// RuntimeComponentNames returns all runtime component names.
func (tc *TierClassifier) RuntimeComponentNames() []string {
	var names []string
	for name, info := range tc.components {
		if info.Tier == TierRuntime {
			names = append(names, name)
		}
	}
	return names
}
