package ogimage

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
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

func TestLayoutFlexSharesTheMainAxis(t *testing.T) {
	spec := &Spec{Width: 300, Height: 100, Tree: el("div", map[string]any{"width": "100%", "height": "100%"},
		el("div", map[string]any{"width": 50, "backgroundColor": "#ff0000"}),
		el("div", map[string]any{"flexGrow": 1, "backgroundColor": "#00ff00"}),
		el("div", map[string]any{"width": 50, "backgroundColor": "#0000ff"}),
	)}
	img := decode(t, mustRender(t, spec))
	for _, c := range []struct {
		x    int
		want color.NRGBA
	}{{25, color.NRGBA{255, 0, 0, 255}}, {150, color.NRGBA{0, 255, 0, 255}}, {275, color.NRGBA{0, 0, 255, 255}}} {
		if got := at(img, c.x, 50); !near(got, c.want, 2) {
			t.Errorf("x=%d: %v, want %v", c.x, got, c.want)
		}
	}
	// stretch: the items are as tall as the container.
	if got := at(img, 25, 95); !near(got, color.NRGBA{255, 0, 0, 255}, 2) {
		t.Errorf("align-items: stretch should fill the height: %v", got)
	}
}

func TestLayoutCenteringAndAbsolute(t *testing.T) {
	spec := &Spec{Width: 200, Height: 200, Tree: el("div", map[string]any{"width": "100%", "height": "100%", "justifyContent": "center", "alignItems": "center", "backgroundColor": "#ffffff"},
		el("div", map[string]any{"width": 40, "height": 40, "backgroundColor": "#112233"}),
		el("div", map[string]any{"position": "absolute", "right": 10, "bottom": 10, "width": 20, "height": 20, "backgroundColor": "#aa0000"}),
	)}
	img := decode(t, mustRender(t, spec))
	if got := at(img, 100, 100); !near(got, color.NRGBA{0x11, 0x22, 0x33, 255}, 2) {
		t.Errorf("centre = %v", got)
	}
	if got := at(img, 100, 70); !near(got, color.NRGBA{255, 255, 255, 255}, 2) {
		t.Errorf("above the box should stay white: %v", got)
	}
	if got := at(img, 180, 180); !near(got, color.NRGBA{0xaa, 0, 0, 255}, 2) {
		t.Errorf("absolute child at the bottom right = %v", got)
	}
}

func TestBordersRadiusAndOpacity(t *testing.T) {
	spec := &Spec{Width: 100, Height: 100, Tree: el("div", map[string]any{"width": "100%", "height": "100%", "backgroundColor": "#ffffff", "padding": 10},
		el("div", map[string]any{"width": 80, "height": 80, "border": "4px solid #ff0000", "borderRadius": 40, "opacity": 0.5}),
	)}
	img := decode(t, mustRender(t, spec))
	// The corner of the box is outside the circle: still white.
	if got := at(img, 12, 12); !near(got, color.NRGBA{255, 255, 255, 255}, 2) {
		t.Errorf("outside the rounded corner = %v", got)
	}
	// On the ring, half opacity red over white.
	if got := at(img, 50, 12); !near(got, color.NRGBA{255, 128, 128, 255}, 10) {
		t.Errorf("border at half opacity = %v", got)
	}
}

func TestImagesAreDrawnAndClipped(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			src.Set(x, y, color.NRGBA{0, 0, 255, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, src)
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	spec := &Spec{Width: 100, Height: 100, Tree: el("div", map[string]any{"width": "100%", "height": "100%", "backgroundColor": "#ffffff", "padding": 10},
		&Node{Type: "element", Tag: "img", Src: uri, Style: map[string]any{"width": 80, "height": 80, "borderRadius": 40}},
	)}
	img := decode(t, mustRender(t, spec))
	if got := at(img, 50, 50); !near(got, color.NRGBA{0, 0, 255, 255}, 6) {
		t.Errorf("image centre = %v", got)
	}
	if got := at(img, 12, 12); !near(got, color.NRGBA{255, 255, 255, 255}, 6) {
		t.Errorf("rounded corners clip the image: %v", got)
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

// Anything outside the subset is an error naming it, never a silently wrong image.
func TestUnsupportedFeaturesAreReported(t *testing.T) {
	spec := &Spec{Width: 100, Height: 100, Tree: el("div", map[string]any{"display": "grid", "transform": "rotate(5deg)", "position": "fixed", "width": "calc(100% - 4px)", "boxShadow": "inset 0 0 4px #000", "color": "notacolor", "filter": "blur(2px)"},
		el("canvas", nil),
		el("svg", nil),
		el("span", map[string]any{"fontSize": "2vw"}, txt("hello")),
	)}
	_, err := Render(spec)
	var e *Error
	if err == nil || !asError(err, &e) {
		t.Fatalf("want an *Error, got %v", err)
	}
	all := strings.Join(e.Issues, "\n")
	for _, want := range []string{`"transform"`, `"filter"`, "display: grid", "position: fixed", "calc(", "inset", "notacolor", "<canvas>", "<svg>", "2vw"} {
		if !strings.Contains(all, want) {
			t.Errorf("issues do not mention %q:\n%s", want, all)
		}
	}
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func TestMissingGlyphsAreReported(t *testing.T) {
	spec := &Spec{Width: 200, Height: 100, Tree: el("div", map[string]any{"fontSize": 24}, txt("Hello \u4f60\u597d \U0001F600"))}
	_, err := Render(spec)
	if err == nil || !strings.Contains(err.Error(), "no glyph") {
		t.Fatalf("want a missing-glyph error, got %v", err)
	}
	if !strings.Contains(err.Error(), "\u4f60") || !strings.Contains(err.Error(), "\U0001F600") {
		t.Errorf("the error names the characters: %v", err)
	}
}

func TestTextWrapsAndEllipsizes(t *testing.T) {
	long := strings.Repeat("word ", 30)
	spec := &Spec{Width: 300, Height: 200, Tree: el("div", map[string]any{"width": "100%", "height": "100%", "backgroundColor": "white"},
		el("div", map[string]any{"width": 300, "fontSize": 20, "color": "black"}, txt(long))),
	}
	img := decode(t, mustRender(t, spec))
	dark := 0
	for y := 70; y < 120; y++ {
		for x := 0; x < 300; x++ {
			if c := at(img, x, y); c.R < 100 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Error("long text must wrap onto further lines")
	}
	one := &Spec{Width: 200, Height: 40, Tree: el("div", map[string]any{"width": 200, "fontSize": 20, "whiteSpace": "nowrap", "textOverflow": "ellipsis", "overflow": "hidden", "backgroundColor": "white", "color": "black"}, txt(long))}
	mustRender(t, one)
}

func TestCustomFonts(t *testing.T) {
	// A font that is not valid TrueType is reported, not ignored.
	spec := &Spec{Width: 100, Height: 50, Fonts: []FontSpec{{Name: "Broken", Data: base64.StdEncoding.EncodeToString([]byte("nope"))}}, Tree: el("div", nil, txt("x"))}
	if _, err := Render(spec); err == nil || !strings.Contains(err.Error(), "Broken") {
		t.Fatalf("want a font error naming Broken, got %v", err)
	}
}

func TestSizeIsBounded(t *testing.T) {
	if _, err := Render(&Spec{Width: 100000, Height: 10, Tree: el("div", nil)}); err == nil {
		t.Error("an absurd size must be refused")
	}
}
