package css

import (
	"reflect"
	"testing"
)

func TestSplitPartsKeepsSourceOrder(t *testing.T) {
	text := ".lead{}\n" + PartMarker("/a.css") + ".a{}\n" + PartMarker("/b.css") + "  \n" + PartMarker("/c.css") + ".c{}"
	got := SplitParts(text)
	want := []Part{{"", ".lead{}\n"}, {"/a.css", ".a{}\n"}, {"/c.css", ".c{}"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitParts = %#v, want %#v", got, want)
	}
	if s := StripPartMarkers(text); s != ".lead{}\n.a{}\n  \n.c{}" && s != ".lead{}\n.a{}\n.c{}" {
		t.Fatalf("StripPartMarkers = %q", s)
	}
}

func TestPartMarkerCannotCloseEarly(t *testing.T) {
	got := SplitParts(PartMarker("/odd*/name.css") + ".x{}")
	if len(got) != 1 || got[0].CSS != ".x{}" {
		t.Fatalf("SplitParts = %#v", got)
	}
}

func TestMapPartsSeesEachSourceAlone(t *testing.T) {
	text := PartMarker("/a.css") + ".a{}" + PartMarker("/b.css") + ".b{}"
	var seen []string
	out := MapParts(text, func(s string) string { seen = append(seen, s); return s + "/**/" })
	if !reflect.DeepEqual(seen, []string{".a{}", ".b{}"}) {
		t.Fatalf("fn saw %q", seen)
	}
	if got := SplitParts(out); len(got) != 2 || got[1].Key != "/b.css" || got[1].CSS != ".b{}/**/" {
		t.Fatalf("MapParts output splits to %#v", got)
	}
	if !StartsWithPart("\n"+text) || StartsWithPart(".a{}") {
		t.Fatal("StartsWithPart")
	}
}
