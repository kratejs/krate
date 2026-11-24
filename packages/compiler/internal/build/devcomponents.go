package build

import (
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// The component inspector's table (dev builds only). The renderer marks
// component roots with encoded data-kd tokens and leaves <!--kd:...-->
// comments for instances without an element (see renderer/devmarks.go).
// finishDevComponents turns those into small numbers in document order and
// writes the table the overlay reads:
//
//	<script type="application/json" id="__krate_dev_components">[...]</script>

// devComponentsID is the id of the table's script element.
const devComponentsID = "__krate_dev_components"

// DevComponentEntry is one row of the inspector table.
type DevComponentEntry struct {
	N    int    `json:"n"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Tier string `json:"tier"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// Parent is the n of the enclosing instance, -1 at the top.
	Parent int    `json:"parent"`
	Island string `json:"island,omitempty"`
	Region string `json:"region,omitempty"`
	// Boundary is the position of the instance's <!--krate-island:...-->
	// comment among those of the document ("use client" islands).
	Boundary *int `json:"boundary,omitempty"`

	declParent string
	domParent  int
}

var devCommentRe = regexp.MustCompile(`<!--` + regexp.QuoteMeta(renderer.DevCommentPrefix) + `([^>]*?)-->`)

// devTable collects the instances of one document.
type devTable struct {
	root    string
	entries []*DevComponentEntry
	byKey   map[string]*DevComponentEntry
}

func (t *devTable) add(c renderer.DevComponent, domParent int) *DevComponentEntry {
	if e, ok := t.byKey[c.Key]; ok {
		return e
	}
	file := c.File
	if file != "" {
		file = displayRel(t.root, file)
	}
	e := &DevComponentEntry{
		N:          len(t.entries),
		Key:        c.Key,
		Name:       c.Name,
		Tier:       c.Tier,
		File:       file,
		Line:       c.Line,
		Island:     c.Island,
		Region:     c.Region,
		declParent: c.Parent,
		domParent:  domParent,
	}
	t.entries = append(t.entries, e)
	t.byKey[c.Key] = e
	return e
}

// finishDevComponents numbers the marked component instances of a dev
// document, drops the markers' encoded form and appends the table. root
// makes the files relative.
func finishDevComponents(html, root string) string {
	t := &devTable{root: root, byKey: map[string]*DevComponentEntry{}}

	// External islands describe themselves in comments the page emits after
	// its markup; their elements are found by id below.
	islands := map[string]renderer.DevComponent{}
	var islandOrder []string
	html = devCommentRe.ReplaceAllStringFunc(html, func(m string) string {
		c, ok := renderer.DecodeDevComponent(devCommentRe.FindStringSubmatch(m)[1])
		if !ok || c.Island == "" {
			return m
		}
		if _, seen := islands[c.Island]; !seen {
			islandOrder = append(islandOrder, c.Island)
		}
		islands[c.Island] = c
		return ""
	})

	type open struct {
		name  string
		inner int // n of the innermost instance at or above this element, -1 for none
	}
	var stack []open
	innermost := func() int {
		if len(stack) == 0 {
			return -1
		}
		return stack[len(stack)-1].inner
	}
	boundaries := 0

	var out strings.Builder
	out.Grow(len(html) + 512)
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
				i = len(html)
				continue
			}
			comment := rest[:4+end+3]
			body := rest[4 : 4+end]
			i += len(comment)
			if strings.HasPrefix(body, renderer.DevCommentPrefix) {
				if c, ok := renderer.DecodeDevComponent(body[len(renderer.DevCommentPrefix):]); ok {
					t.add(c, innermost())
				}
				continue
			}
			if strings.HasPrefix(body, "krate-island:") {
				t.addBoundary(body[len("krate-island:"):], boundaries, innermost())
				boundaries++
			}
			out.WriteString(comment)
			continue
		case strings.HasPrefix(rest, "</"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				out.WriteString(rest)
				i = len(html)
				continue
			}
			name := strings.ToLower(strings.TrimSpace(rest[2:end]))
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j].name == name {
					stack = stack[:j]
					break
				}
			}
			out.WriteString(rest[:end+1])
			i += end + 1
			continue
		case strings.HasPrefix(rest, "<!"), strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				out.WriteString(rest)
				i = len(html)
				continue
			}
			out.WriteString(rest[:end+1])
			i += end + 1
			continue
		}
		name, ok := devTagName(rest)
		if !ok {
			out.WriteByte('<')
			i++
			continue
		}
		end := devTagEnd(rest)
		if end < 0 {
			out.WriteString(rest)
			i = len(html)
			continue
		}
		tag := rest[:end+1]
		i += end + 1
		lower := strings.ToLower(name)
		inner := innermost()
		var ns []int
		tokens, stripped := devTagTokens(tag)
		for _, tok := range tokens {
			c, ok := renderer.DecodeDevComponent(tok)
			if !ok {
				continue
			}
			e := t.add(c, inner)
			ns = append(ns, e.N)
			inner = e.N
		}
		if id := devAttrValue(stripped, "data-krate-island"); id != "" {
			if c, ok := islands[id]; ok {
				e := t.add(c, inner)
				ns = append(ns, e.N)
				inner = e.N
			}
		}
		if len(ns) > 0 {
			parts := make([]string, len(ns))
			for k, n := range ns {
				parts[k] = strconv.Itoa(n)
			}
			tag = devSetAttr(stripped, renderer.DevAttr, strings.Join(parts, " "))
		} else if len(tokens) > 0 {
			tag = stripped
		}
		out.WriteString(tag)
		if strings.HasSuffix(tag, "/>") || htmlVoidElements[lower] {
			continue
		}
		if htmlRawTextElements[lower] {
			closeAt := devIndexFold(html[i:], "</"+lower)
			if closeAt < 0 {
				out.WriteString(html[i:])
				i = len(html)
				continue
			}
			out.WriteString(html[i : i+closeAt])
			i += closeAt
		}
		stack = append(stack, open{name: lower, inner: inner})
	}
	// Islands whose element the document does not hold (a hidden branch).
	for _, id := range islandOrder {
		t.add(islands[id], -1)
	}
	for _, e := range t.entries {
		e.Parent = e.domParent
		if p, ok := t.byKey[e.declParent]; ok && e.declParent != "" && p.N != e.N {
			e.Parent = p.N
		}
	}

	rows := make([]*DevComponentEntry, len(t.entries))
	copy(rows, t.entries)
	data, err := json.Marshal(rows)
	if err != nil {
		data = []byte("[]")
	}
	script := `<script type="application/json" id="` + devComponentsID + `">` + escapeJSONForScript(string(data)) + `</script>`
	doc := out.String()
	if at := strings.LastIndex(doc, "</body>"); at >= 0 {
		return doc[:at] + script + doc[at:]
	}
	return doc + script
}

// addBoundary records the "use client" island of a <!--krate-island:{...}-->
// comment: its module and export.
func (t *devTable) addBoundary(payload string, ordinal, domParent int) {
	var rec struct {
		M string `json:"m"`
		E string `json:"e"`
	}
	_ = json.Unmarshal([]byte(payload), &rec)
	name := rec.E
	if name == "" || name == "default" {
		name = strings.TrimSuffix(path.Base(rec.M), path.Ext(rec.M))
	}
	if name == "" || name == "." {
		name = "island"
	}
	e := t.add(renderer.DevComponent{Key: "boundary:" + strconv.Itoa(ordinal), Name: name, Tier: renderer.DevTierIsland, File: rec.M}, domParent)
	b := ordinal
	e.Boundary = &b
}

var htmlVoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

var htmlRawTextElements = map[string]bool{
	"script": true, "style": true, "textarea": true, "title": true, "xmp": true,
	"iframe": true, "noembed": true, "noframes": true, "plaintext": true,
}

func devTagName(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	c := s[1]
	if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return "", false
	}
	j := 2
	for j < len(s) && !strings.ContainsRune(" \t\n\r\f/>", rune(s[j])) {
		j++
	}
	return s[1:j], true
}

func devTagEnd(s string) int {
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

// devTagTokens returns the encoded tokens of a start tag's data-kd attribute
// (as the renderer writes it: quoted, after a space) and the tag without it.
func devTagTokens(tag string) ([]string, string) {
	const attr = " " + renderer.DevAttr + `="`
	at := strings.Index(tag, attr)
	if at < 0 {
		return nil, tag
	}
	end := strings.IndexByte(tag[at+len(attr):], '"')
	if end < 0 {
		return nil, tag
	}
	value := tag[at+len(attr) : at+len(attr)+end]
	return strings.Fields(value), tag[:at] + tag[at+len(attr)+end+1:]
}

// devAttrValue returns the value of a start tag's attribute, "" when absent.
func devAttrValue(tag, name string) string {
	at := strings.Index(tag, " "+name+"=")
	if at < 0 {
		return ""
	}
	v := tag[at+len(name)+2:]
	if v == "" {
		return ""
	}
	if q := v[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(v[1:], q); end >= 0 {
			return v[1 : end+1]
		}
		return ""
	}
	end := strings.IndexAny(v, " \t\n\r\f/>")
	if end < 0 {
		return v
	}
	return v[:end]
}

// devSetAttr adds name="value" at the end of a start tag.
func devSetAttr(tag, name, value string) string {
	end := len(tag) - 1
	if strings.HasSuffix(tag, "/>") {
		end = len(tag) - 2
	}
	return tag[:end] + " " + name + `="` + escape.HTMLAttr(value) + `"` + tag[end:]
}

func devIndexFold(s, sub string) int {
	n := len(sub)
	for i := 0; i+n <= len(s); i++ {
		if strings.EqualFold(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}
