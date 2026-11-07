package mcp

import (
	"bytes"
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
)

// htmlInfo is what the page tools extract from a built document.
type htmlInfo struct {
	Lang        string            `json:"lang,omitempty"`
	Title       string            `json:"title,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
	Canonical   string            `json:"canonical,omitempty"`
	Links       []linkInfo        `json:"links,omitempty"`
	Scripts     []scriptInfo      `json:"scripts,omitempty"`
	Stylesheets []string          `json:"stylesheets,omitempty"`
	JSONLD      []string          `json:"jsonLdTypes,omitempty"`
	Headings    []headingInfo     `json:"headings,omitempty"`
	Landmarks   map[string]int    `json:"landmarks,omitempty"`
	Anchors     []anchorInfo      `json:"-"`
	Forms       []formInfo        `json:"forms,omitempty"`
	Images      []imageInfo       `json:"-"`
	Buttons     int               `json:"buttons,omitempty"`
	TextWords   int               `json:"textWords"`
	Text        string            `json:"-"`
	InlineJS    int               `json:"inlineScripts,omitempty"`
}

type linkInfo struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
	As   string `json:"as,omitempty"`
}

type scriptInfo struct {
	Src    string `json:"src,omitempty"`
	Type   string `json:"type,omitempty"`
	Inline bool   `json:"inline,omitempty"`
	Bytes  int    `json:"inlineBytes,omitempty"`
}

type headingInfo struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type anchorInfo struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

type formInfo struct {
	Action string   `json:"action,omitempty"`
	Method string   `json:"method,omitempty"`
	Fields []string `json:"fields,omitempty"`
}

type imageInfo struct {
	Src    string `json:"src"`
	Alt    string `json:"alt"`
	HasAlt bool   `json:"hasAlt"`
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		if c.Type == html.ElementNode && (c.Data == "script" || c.Data == "style") {
			return
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

var landmarkTags = map[string]bool{"header": true, "nav": true, "main": true, "aside": true, "footer": true, "section": true, "article": true}

// parseHTMLInfo extracts metadata and structure from a built page.
func parseHTMLInfo(doc []byte) *htmlInfo {
	root, err := html.Parse(bytes.NewReader(doc))
	info := &htmlInfo{Meta: map[string]string{}, Landmarks: map[string]int{}}
	if err != nil {
		return info
	}
	var text strings.Builder
	var walk func(n *html.Node, inBody bool)
	walk = func(n *html.Node, inBody bool) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "html":
				info.Lang = attrVal(n, "lang")
			case "title":
				if info.Title == "" {
					info.Title = nodeText(n)
				}
			case "meta":
				key := attrVal(n, "name")
				if key == "" {
					key = attrVal(n, "property")
				}
				if key == "" && hasAttr(n, "charset") {
					key = "charset"
					info.Meta[key] = attrVal(n, "charset")
				} else if key != "" {
					info.Meta[key] = attrVal(n, "content")
				}
				if h := attrVal(n, "http-equiv"); h != "" {
					info.Meta["http-equiv:"+h] = attrVal(n, "content")
				}
			case "link":
				rel, href := attrVal(n, "rel"), attrVal(n, "href")
				switch rel {
				case "canonical":
					info.Canonical = href
				case "stylesheet":
					info.Stylesheets = append(info.Stylesheets, href)
				default:
					info.Links = append(info.Links, linkInfo{Rel: rel, Href: href, As: attrVal(n, "as")})
				}
			case "script":
				typ := attrVal(n, "type")
				if typ == "application/ld+json" {
					var v any
					body := nodeRaw(n)
					if json.Unmarshal([]byte(body), &v) == nil {
						info.JSONLD = append(info.JSONLD, ldTypes(v)...)
					}
					return
				}
				if src := attrVal(n, "src"); src != "" {
					info.Scripts = append(info.Scripts, scriptInfo{Src: src, Type: typ})
				} else {
					info.InlineJS++
					info.Scripts = append(info.Scripts, scriptInfo{Type: typ, Inline: true, Bytes: len(nodeRaw(n))})
				}
				return
			case "style":
				return
			case "body":
				inBody = true
			case "h1", "h2", "h3", "h4", "h5", "h6":
				info.Headings = append(info.Headings, headingInfo{Level: int(n.Data[1] - '0'), Text: nodeText(n)})
			case "a":
				if h := attrVal(n, "href"); h != "" {
					info.Anchors = append(info.Anchors, anchorInfo{Href: h, Text: nodeText(n)})
				}
			case "img":
				info.Images = append(info.Images, imageInfo{Src: attrVal(n, "src"), Alt: attrVal(n, "alt"), HasAlt: hasAttr(n, "alt")})
			case "button":
				info.Buttons++
			case "form":
				f := formInfo{Action: attrVal(n, "action"), Method: strings.ToUpper(attrVal(n, "method"))}
				var fields func(*html.Node)
				fields = func(c *html.Node) {
					if c.Type == html.ElementNode && (c.Data == "input" || c.Data == "select" || c.Data == "textarea") {
						if t := attrVal(c, "type"); t != "hidden" {
							name := attrVal(c, "name")
							if name == "" {
								name = attrVal(c, "id")
							}
							f.Fields = append(f.Fields, c.Data+":"+name)
						}
					}
					for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
						fields(ch)
					}
				}
				fields(n)
				info.Forms = append(info.Forms, f)
			}
			if inBody && landmarkTags[n.Data] {
				info.Landmarks[n.Data]++
			}
			if role := attrVal(n, "role"); inBody && role != "" {
				switch role {
				case "banner", "navigation", "main", "complementary", "contentinfo", "search":
					info.Landmarks["role="+role]++
				}
			}
		}
		if n.Type == html.TextNode && inBody {
			text.WriteString(n.Data)
			text.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inBody)
		}
	}
	walk(root, false)
	info.Text = strings.Join(strings.Fields(text.String()), " ")
	info.TextWords = len(strings.Fields(info.Text))
	if len(info.Landmarks) == 0 {
		info.Landmarks = nil
	}
	return info
}

func nodeRaw(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

func ldTypes(v any) []string {
	var out []string
	switch t := v.(type) {
	case map[string]any:
		switch ty := t["@type"].(type) {
		case string:
			out = append(out, ty)
		case []any:
			for _, x := range ty {
				if s, ok := x.(string); ok {
					out = append(out, s)
				}
			}
		}
		if g, ok := t["@graph"].([]any); ok {
			for _, x := range g {
				out = append(out, ldTypes(x)...)
			}
		}
	case []any:
		for _, x := range t {
			out = append(out, ldTypes(x)...)
		}
	}
	return out
}

// splitHTML separates head and body source for the get_rendered_html tool.
func splitHTML(doc string) (head, body string) {
	low := strings.ToLower(doc)
	hs := strings.Index(low, "<head")
	he := strings.Index(low, "</head>")
	if hs >= 0 && he > hs {
		head = doc[hs : he+len("</head>")]
	}
	bs := strings.Index(low, "<body")
	if bs >= 0 {
		body = doc[bs:]
	}
	return head, body
}
