package ogimage

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func el(tag string, style map[string]any, children ...*Node) *Node {
	return &Node{Type: "element", Tag: tag, Style: style, Children: children}
}

func txt(s string) *Node { return &Node{Type: "text", Text: s} }

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	return img
}

func at(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func near(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}

func card() *Spec {
	return &Spec{Width: 1200, Height: 630, Tree: el("div", map[string]any{
		"width": "100%", "height": "100%", "flexDirection": "column", "justifyContent": "space-between", "padding": 64,
		"backgroundImage": "linear-gradient(135deg, #0f172a 0%, #1e3a8a 60%, #7c3aed 100%)", "color": "white", "fontFamily": "sans-serif",
	},
		el("div", map[string]any{"display": "flex", "alignItems": "center", "gap": 16},
			el("div", map[string]any{"width": 56, "height": 56, "borderRadius": 28, "backgroundColor": "#f59e0b"}),
			el("div", map[string]any{"fontSize": 28, "fontWeight": 700}, txt("Krate")),
		),
		el("div", map[string]any{"display": "flex", "flexDirection": "column", "gap": 12},
			el("div", map[string]any{"fontSize": 72, "fontWeight": 700, "lineHeight": 1.1}, txt("Metadata routes without a headless browser")),
			el("div", map[string]any{"fontSize": 30, "color": "rgba(255,255,255,0.75)"}, txt("A real PNG, drawn in Go from a JSX subset.")),
		),
		el("div", map[string]any{"display": "flex", "justifyContent": "space-between", "alignItems": "center", "fontSize": 24},
			el("div", map[string]any{"padding": "10 20", "borderRadius": 999, "border": "2px solid rgba(255,255,255,0.6)"}, txt("krate.js.org")),
			el("div", map[string]any{"width": 160, "height": 12, "borderRadius": 6, "backgroundImage": "linear-gradient(to right, #34d399, #3b82f6)", "boxShadow": "0 6px 18px rgba(0,0,0,0.5)"}),
		),
	)}
}

func TestRenderCard(t *testing.T) {
	data, err := Render(card())
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, data)
	if b := img.Bounds(); b.Dx() != 1200 || b.Dy() != 630 {
		t.Fatalf("size = %v", b)
	}
	// The gradient runs from the dark top-left to the violet bottom-right.
	if c := at(img, 4, 4); !near(c, color.NRGBA{0x0f, 0x17, 0x2b, 255}, 28) {
		t.Errorf("top-left = %v, want the start of the gradient", c)
	}
	if c := at(img, 1195, 625); !near(c, color.NRGBA{0x7c, 0x3a, 0xed, 255}, 28) {
		t.Errorf("bottom-right = %v, want the end of the gradient", c)
	}
	// The amber dot sits inside the padding at the top left.
	if c := at(img, 64+28, 64+28); !near(c, color.NRGBA{0xf5, 0x9e, 0x0b, 255}, 6) {
		t.Errorf("logo dot = %v", c)
	}
	// The headline paints light text: some pixel in its band is near white.
	light := 0
	for y := 250; y < 420; y++ {
		for x := 64; x < 1100; x++ {
			if c := at(img, x, y); c.R > 230 && c.G > 230 && c.B > 230 {
				light++
			}
		}
	}
	if light < 2000 {
		t.Errorf("the headline drew only %d light pixels", light)
	}
	if dir := os.Getenv("KRATE_OG_OUT"); dir != "" {
		if err := os.WriteFile(dir+"/card.png", data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
