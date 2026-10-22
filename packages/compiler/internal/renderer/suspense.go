package renderer

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// Suspense boundaries and lazy hydration chunks.
//
// A <Suspense> boundary is delimited by <!--suspense:ID--> markers. When its
// content can suspend on the client (a component in it reads use(promise) or
// a resource), the boundary is emitted as a conditional slot so the runtime
// can show either state:
//
//	<!--suspense:ID--><!--k:sID-->fallback<!--/k:sID--><!--/suspense:ID-->
//	<template data-kc="sID" data-kb="c">content</template>   (hoisted)
//
// The content cannot be resolved at build time, so the fallback is active and
// the content waits in the template; hydration binds the content's slots in
// the template and the runtime swaps it in once nothing inside is pending.
// Boundaries whose content never suspends bake the content only, as before.

// boundarySlotID is the conditional slot id of a suspending boundary (the
// runtime's boundarySlotId).
func boundarySlotID(streamID string) string {
	return "s" + streamID
}

func (e *Emitter) emitSuspenseSlot(s *irtree.SuspenseSlot) SlotOutput {
	id := escape.Comment(s.StreamID)
	open, close := `<!--suspense:`+id+`-->`, `<!--/suspense:`+id+`-->`

	if s.Mode != irtree.SuspenseModeStatic {
		// Region and empty boundaries bake the fallback; a region render
		// replaces it at request time.
		var out SlotOutput
		for _, child := range s.Fallback {
			mergeSlotOutput(&out, e.emitSlotNode(child))
		}
		out.HTML = open + out.HTML + close
		return out
	}

	var content SlotOutput
	for _, child := range s.Resolved {
		mergeSlotOutput(&content, e.emitSlotNode(child))
	}
	if !claimBoundary(content.Signatures, s.StreamID) {
		content.HTML = open + content.HTML + close
		return content
	}

	var fallback SlotOutput
	for _, child := range s.Fallback {
		mergeSlotOutput(&fallback, e.emitSlotNode(child))
	}
	sid := boundarySlotID(s.StreamID)
	// The effect scheduler treats the boundary as a conditional: content
	// effects wait until it is revealed.
	tagCondBranch(content.Signatures, irtree.SlotID(sid), "c", false)
	tagCondBranch(fallback.Signatures, irtree.SlotID(sid), "a", true)
	contentHTML, fallbackHTML := content.HTML, fallback.HTML
	content.HTML, fallback.HTML = "", ""
	var out SlotOutput
	mergeSlotOutput(&out, content)
	mergeSlotOutput(&out, fallback)
	out.HTML = open + slotMarker(sid) + fallbackHTML + slotEndMarker(sid) + close
	out.Templates = `<template data-kc="` + escape.HTMLAttr(sid) + `" data-kb="c">` + contentHTML + `</template>` + out.Templates
	return out
}

// claimBoundary assigns the boundary to every signature in its content that
// no inner boundary claimed (the innermost boundary wins) and reports whether
// any of them can suspend.
func claimBoundary(sigs []irtree.ComponentSignature, streamID string) bool {
	suspends := false
	for i := range sigs {
		if sigs[i].Boundary != "" || sigs[i].Tier != irtree.TierClient {
			continue
		}
		sigs[i].Boundary = streamID
		if sigSuspends(&sigs[i]) {
			suspends = true
		}
	}
	return suspends
}

// sigSuspends reports whether a component's hydration code reads use() values
// or resources, so it must run inside its boundary.
func sigSuspends(sig *irtree.ComponentSignature) bool {
	// A lazy instance waits for its chunk or loader before it is shown.
	return sig.Lazy || sigCodeSuspends(sig)
}

// sigCodeSuspends reports whether a component's hydration code itself can
// suspend (use() values, resources, lazy loaders): it runs in __krateBoundary.
func sigCodeSuspends(sig *irtree.ComponentSignature) bool {
	for _, group := range [][]string{sig.PreSignalVars, sig.ExtraVars} {
		for _, js := range group {
			if strings.Contains(js, "__krateUse(") || strings.Contains(js, "createResource(") || strings.Contains(js, "__krateLazyPayload(") {
				return true
			}
		}
	}
	return false
}

// LazyChunk is the hydration code of one lazy() component on a page. The
// page script loads it with __krateLazy(Token, ...); the build writes JS to a
// file and replaces Token with its URL.
type LazyChunk struct {
	Name  string
	Token string
	JS    string
}

// LazyChunkToken is the URL placeholder of a lazy chunk in page hydration JS.
func LazyChunkToken(name string) string {
	return "__KRATE_LAZY_CHUNK_" + name + "__"
}

// sigSpan locates one component's hydration IIFE in the page script.
type sigSpan struct {
	sig        *irtree.ComponentSignature
	start, end int
}

// finishSuspenseHydration rewrites the per-component IIFEs of a page script:
// suspending components run through __krateBoundary, and lazy components
// (with their descendants) move to LazyChunks, leaving a __krateLazy call.
func finishSuspenseHydration(result *EmitResult, js string, spans []sigSpan, listFns string) string {
	result.LazyChunks = nil
	needed := false
	for _, sp := range spans {
		if sp.sig.Lazy || sigSuspends(sp.sig) {
			needed = true
			break
		}
	}
	if !needed {
		return js
	}

	type lazyRoot struct{ id, name string }
	var roots []lazyRoot
	chunkBody := map[string]*strings.Builder{}
	var order []string

	var out strings.Builder
	prev := 0
	for _, sp := range spans {
		out.WriteString(js[prev:sp.start])
		prev = sp.end
		text := js[sp.start:sp.end]
		if sigCodeSuspends(sp.sig) {
			text = wrapBoundaryRun(text, sp.sig.Boundary)
		}
		id := string(sp.sig.ComponentID)
		owner := ""
		for _, r := range roots {
			if strings.HasPrefix(id, r.id+".") {
				owner = r.name
				break
			}
		}
		isRoot := owner == "" && sp.sig.Lazy && sp.sig.LazyName != ""
		if isRoot {
			owner = sp.sig.LazyName
			roots = append(roots, lazyRoot{id: id, name: owner})
		}
		if owner == "" {
			out.WriteString(text)
			continue
		}
		cb := chunkBody[owner]
		if cb == nil {
			cb = &strings.Builder{}
			chunkBody[owner] = cb
			order = append(order, owner)
		}
		cb.WriteString(text)
		if isRoot {
			out.WriteString("__krateLazy(" + strconv.Quote(LazyChunkToken(owner)) + "," + boundaryArg(sp.sig.Boundary) + ");\n")
		}
	}
	out.WriteString(js[prev:])

	for _, name := range order {
		result.LazyChunks = append(result.LazyChunks, LazyChunk{
			Name:  name,
			Token: LazyChunkToken(name),
			JS:    "__krateLazyRun(function(){\n" + listFns + chunkBody[name].String() + "});\n",
		})
	}
	return out.String()
}

func boundaryArg(streamID string) string {
	if streamID == "" {
		return "null"
	}
	return strconv.Quote(streamID)
}

// wrapBoundaryRun turns `// component X\n(function(){\n...})();\n` into a
// __krateBoundary run of the same body.
func wrapBoundaryRun(text, streamID string) string {
	const open, close = "(function(){\n", "})();\n"
	i := strings.Index(text, open)
	if i < 0 || !strings.HasSuffix(text, close) {
		return text
	}
	body := text[i+len(open) : len(text)-len(close)]
	return text[:i] + "__krateBoundary(" + boundaryArg(streamID) + ",function(){\n" + body + "});\n"
}
