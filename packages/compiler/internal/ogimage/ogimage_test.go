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

// Length stop positions (px, a unitless 0) and hard stops, as in a 16px brand
// stripe: linear-gradient(to right, #25757e 0, #3c7b9a 16px, #070b0c 16px).
// They used to be rejected, so the og route answered 500.
func TestGradientLengthAndHardStops(t *testing.T) {
	spec := &Spec{Width: 200, Height: 40, Tree: el("div", map[string]any{
		"width": "100%", "height": "100%",
		"backgroundImage": "linear-gradient(to right, #25757e 0, #3c7b9a 16px, #070b0c 16px)",
	})}
	data, err := Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, data)
	if c := at(img, 0, 20); !near(c, color.NRGBA{0x25, 0x75, 0x7e, 255}, 8) {
		t.Errorf("x=0: %v, want the first stop", c)
	}
	if c := at(img, 14, 20); !near(c, color.NRGBA{0x3c, 0x7b, 0x9a, 255}, 12) {
		t.Errorf("x=14: %v, want near the second stop", c)
	}
	for _, x := range []int{17, 100, 199} {
		if c := at(img, x, 20); !near(c, color.NRGBA{0x07, 0x0b, 0x0c, 255}, 2) {
			t.Errorf("x=%d: %v, want the flat background after the hard stop", x, c)
		}
	}
	// Two positions on one stop, and a later stop placed before an earlier one.
	for _, g := range []string{"linear-gradient(red 0 50%, blue 50% 100%)", "linear-gradient(90deg, red 40px, blue 10px)", "linear-gradient(red 1em, blue 2rem)"} {
		spec.Tree.Style["backgroundImage"] = g
		if _, err := Render(spec); err != nil {
			t.Errorf("%s: %v", g, err)
		}
	}
	if c := at(decode(t, mustRender(t, &Spec{Width: 100, Height: 10, Tree: el("div", map[string]any{"width": "100%", "height": "100%", "backgroundImage": "linear-gradient(90deg, red 40px, blue 10px)"})})), 60, 5); !near(c, color.NRGBA{0, 0, 255, 255}, 2) {
		t.Errorf("a stop before the previous one must clamp to it (hard edge at 40px): %v", c)
	}
}

func mustRender(t *testing.T, s *Spec) []byte {
	t.Helper()
	data, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
