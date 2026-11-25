package renderer

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// Component inspector markers (dev builds only).
//
// With Emitter.DevInspect set, every component instance marks the top-level
// elements of its output with a data-kd attribute holding an encoded
// DevComponent (outer components first when several share a root element).
// Instances without an element of their own (runtime regions, external
// islands) leave a <!--kd:...--> comment instead. The build numbers the
// instances of the final document, rewrites data-kd to those numbers, drops
// the comments and writes the table the overlay reads (see
// build/devcomponents.go). Attributes do not change the DOM structure the
// hydration code walks, so the markers never change what a page does.

// DevAttr is the attribute that marks a component's root elements.
const DevAttr = "data-kd"

// DevCommentPrefix opens the comment of an instance without an element.
const DevCommentPrefix = "kd:"

// Inspector tiers beyond the IR's component tiers.
const (
	DevTierIsland = "island"
)

// DevComponent is one component instance in the inspector table.
type DevComponent struct {
	// Key identifies the instance within the document. For client
	// components it is the hydration scope id, which the page script uses
	// when it registers signals and hydration times.
	Key    string
	Name   string
	Tier   string
	File   string
	Line   int
	Parent string // Key of the enclosing instance, "" when unknown
	Island string // data-krate-island id of an external island
	Region string // region id of a runtime component
}

// devEncodeField writes every byte outside a small safe set as *XX (hex), so
// an encoded value is valid inside a quoted attribute and inside a comment,
// and escape.Comment and escape.HTMLAttr leave it unchanged.
func devEncodeField(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '/', c == ':', c == '@', c == '$', c == '~', c == '+':
			b.WriteByte(c)
		default:
			b.WriteByte('*')
			b.WriteByte("0123456789ABCDEF"[c>>4])
			b.WriteByte("0123456789ABCDEF"[c&15])
		}
	}
	return b.String()
}

func devDecodeField(s string) (string, bool) {
	if !strings.Contains(s, "*") {
		return s, true
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '*' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", false
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			return "", false
		}
		b.WriteByte(byte(v))
		i += 2
	}
	return b.String(), true
}

// EncodeDevComponent packs c into a single token without spaces.
func EncodeDevComponent(c DevComponent) string {
	fields := []string{c.Key, c.Name, c.Tier, c.File, strconv.Itoa(c.Line), c.Parent, c.Island, c.Region}
	for i, f := range fields {
		fields[i] = devEncodeField(f)
	}
	return strings.Join(fields, ",")
}

// DecodeDevComponent unpacks a token of EncodeDevComponent.
func DecodeDevComponent(s string) (DevComponent, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 8 {
		return DevComponent{}, false
	}
	for i, p := range parts {
		v, ok := devDecodeField(p)
		if !ok {
			return DevComponent{}, false
		}
		parts[i] = v
	}
	line, _ := strconv.Atoi(parts[4])
	c := DevComponent{Key: parts[0], Name: parts[1], Tier: parts[2], File: parts[3], Line: line, Parent: parts[5], Island: parts[6], Region: parts[7]}
	return c, c.Key != ""
}

// devComment is the marker of an instance that has no element.
func devComment(c DevComponent) string {
	return "<!--" + DevCommentPrefix + escape.Comment(EncodeDevComponent(c)) + "-->"
}

// devUnmarked are elements a component root never marks: document and head
// elements, raw text elements, templates, and the island placeholders the
// build later finds by their exact markup.
var devUnmarked = map[string]bool{
	"html": true, "head": true, "body": true, "meta": true, "link": true, "title": true,
	"script": true, "style": true, "base": true, "noscript": true, "template": true,
	"slot": true, "krate-head": true,
}

// htmlVoid are the void elements: they have no end tag.
var htmlVoid = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

// htmlRawText are elements whose content is not markup.
var htmlRawText = map[string]bool{
	"script": true, "style": true, "textarea": true, "title": true, "xmp": true,
	"iframe": true, "noembed": true, "noframes": true, "plaintext": true,
}

// MarkDevRoots adds the token enc to the data-kd attribute of every
// top-level element of the fragment html, before the tokens it already holds
// (enclosing components come first). Comments, text and nested elements are
// left alone.
func MarkDevRoots(html, enc string) string {
	if enc == "" || !strings.Contains(html, "<") {
		return html
	}
	var out strings.Builder
	out.Grow(len(html) + 64)
	depth := 0
	i := 0
	for i < len(html) {
		lt := strings.IndexByte(html[i:], '<')
		if lt < 0 {
			out.WriteString(html[i:])
			break
		}
		out.WriteString(html[i : i+lt])
		i += lt
		rest := html[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				out.WriteString(rest)
				return out.String()
			}
			out.WriteString(rest[:4+end+3])
			i += 4 + end + 3
			continue
		case strings.HasPrefix(rest, "</"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				out.WriteString(rest)
				return out.String()
			}
			out.WriteString(rest[:end+1])
			i += end + 1
			if depth > 0 {
				depth--
			}
			continue
		case strings.HasPrefix(rest, "<!"), strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				out.WriteString(rest)
				return out.String()
			}
			out.WriteString(rest[:end+1])
			i += end + 1
			continue
		}
		name, ok := startTagName(rest)
		if !ok {
			out.WriteByte('<')
			i++
			continue
		}
		end := startTagEnd(rest)
		if end < 0 {
			out.WriteString(rest)
			return out.String()
		}
		tag := rest[:end+1]
		lower := strings.ToLower(name)
		selfClosing := strings.HasSuffix(tag, "/>")
		if depth == 0 && !devUnmarked[lower] && !strings.Contains(tag, "data-krate-island=") {
			tag = addDevToken(tag, enc)
		}
		out.WriteString(tag)
		i += end + 1
		if selfClosing || htmlVoid[lower] {
			continue
		}
		if htmlRawText[lower] {
			// Copy the content up to the end tag as is.
			closeAt := indexFold(html[i:], "</"+lower)
			if closeAt < 0 {
				out.WriteString(html[i:])
				return out.String()
			}
			out.WriteString(html[i : i+closeAt])
			i += closeAt
			depth++
			continue
		}
		depth++
	}
	return out.String()
}

// addDevToken puts enc first in the start tag's data-kd attribute, adding the
// attribute at the end of the tag when it has none.
func addDevToken(tag, enc string) string {
	const attr = " " + DevAttr + `="`
	if i := strings.Index(tag, attr); i >= 0 {
		at := i + len(attr)
		return tag[:at] + escape.HTMLAttr(enc) + " " + tag[at:]
	}
	end := len(tag) - 1
	if strings.HasSuffix(tag, "/>") {
		end = len(tag) - 2
	}
	return tag[:end] + attr + escape.HTMLAttr(enc) + `"` + tag[end:]
}

// startTagName returns the tag name of the start tag s begins with.
func startTagName(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	c := s[1]
	if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return "", false
	}
	j := 2
	for j < len(s) {
		c := s[j]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '/' || c == '>' {
			break
		}
		j++
	}
	return s[1:j], true
}

// startTagEnd returns the index of the '>' closing the start tag s begins
// with, skipping quoted attribute values.
func startTagEnd(s string) int {
	var quote byte
	for j := 1; j < len(s); j++ {
		c := s[j]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return j
		}
	}
	return -1
}

// indexFold is strings.Index ignoring ASCII case in s.
func indexFold(s, sub string) int {
	n := len(sub)
	for i := 0; i+n <= len(s); i++ {
		if strings.EqualFold(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}

// devTierName is the inspector tier of an IR component.
func devTierName(t irtree.ComponentTier) string {
	switch t {
	case irtree.TierStatic, irtree.TierServer, irtree.TierRuntime, irtree.TierClient:
		return t.String()
	}
	return irtree.TierClient.String()
}

// devNode describes an IR component instance for the inspector.
func (e *Emitter) devNode(node *irtree.ComponentNode) DevComponent {
	file := node.Module
	if file == "" {
		file = e.componentSources[node.Name]
	}
	if file == "" {
		file = node.SourceFile
	}
	line := node.Line
	if line == 0 && node.Fn != nil {
		line = node.Fn.Position.Line
	}
	tier := devTierName(node.Tier)
	if node.IsSSREval {
		// A component evaluated at build time ships no code; a *.server file
		// keeps its tier name.
		tier = devBuildTimeTier(file)
	}
	return DevComponent{
		Key:    string(node.ID),
		Name:   node.Name,
		Tier:   tier,
		File:   file,
		Line:   line,
		Parent: e.devParent(),
	}
}

// devBuildTimeTier names the tier of a component rendered at build time:
// "server" for a *.server.* file, else "static".
func devBuildTimeTier(file string) string {
	base := file
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	if strings.Contains(base, ".server.") {
		return irtree.TierServer.String()
	}
	return irtree.TierStatic.String()
}

// devParent is the key of the instance being emitted, "" at the top.
func (e *Emitter) devParent() string {
	if len(e.devStack) == 0 {
		return ""
	}
	return e.devStack[len(e.devStack)-1]
}

// devMark marks the roots of out with the instance c.
func (e *Emitter) devMark(out SlotOutput, c DevComponent) SlotOutput {
	out.HTML = MarkDevRoots(out.HTML, EncodeDevComponent(c))
	return out
}

// devIslandComments describes the page's external islands: the build finds
// each one's element by its data-krate-island id.
func devIslandComments(tree *irtree.ComponentTree) string {
	var b strings.Builder
	for _, x := range tree.ExternalComponents {
		b.WriteString(devComment(DevComponent{
			Key:    "island:" + x.ID,
			Name:   x.Local,
			Tier:   DevTierIsland,
			File:   x.File,
			Line:   x.Line,
			Island: x.ID,
		}))
	}
	return b.String()
}
