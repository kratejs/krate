// Package ogimage renders the JSX subset of ImageResponse (Open Graph and
// Twitter card images) to a PNG, in Go, with no dependency beyond
// golang.org/x/image: a flexbox layout, text set with embedded or supplied
// TrueType fonts, colours, gradients, borders, shadows and images.
//
// Anything outside the subset is reported as an issue instead of being drawn
// wrong: Render returns an *Error listing every unsupported property, value,
// element or character.
package ogimage

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Spec is what ImageResponse hands over: the canvas, fonts and the element tree.
type Spec struct {
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Fonts  []FontSpec `json:"fonts,omitempty"`
	Tree   *Node      `json:"tree"`
	// Debug draws nothing extra; reserved.
}

// FontSpec is a TrueType or OpenType font (base64 data) for a family.
type FontSpec struct {
	Name   string `json:"name"`
	Data   string `json:"data"`
	Weight int    `json:"weight,omitempty"`
	Style  string `json:"style,omitempty"`
}

// Node is an element or a text run of the tree.
type Node struct {
	Type     string         `json:"type"` // "element" or "text"
	Tag      string         `json:"tag,omitempty"`
	Style    map[string]any `json:"style,omitempty"`
	Src      string         `json:"src,omitempty"`
	Text     string         `json:"text,omitempty"`
	Children []*Node        `json:"children,omitempty"`
}

// ParseSpec decodes the JSON ImageResponse produces.
func ParseSpec(data []byte) (*Spec, error) {
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unreadable image spec: %w", err)
	}
	return &s, nil
}

// Error lists everything in the tree the renderer cannot draw.
type Error struct {
	Issues []string
}

func (e *Error) Error() string {
	return "ImageResponse uses features the image renderer does not support:\n  - " + strings.Join(e.Issues, "\n  - ")
}

type issues struct {
	seen map[string]bool
	list []string
}

func (i *issues) add(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if i.seen == nil {
		i.seen = map[string]bool{}
	}
	if !i.seen[msg] {
		i.seen[msg] = true
		i.list = append(i.list, msg)
	}
}

func (i *issues) err() error {
	if len(i.list) == 0 {
		return nil
	}
	sort.Strings(i.list)
	return &Error{Issues: i.list}
}
