package typecheck

import "testing"

func TestRendererDecoratorDiagnosticsDropped(t *testing.T) {
	src := "@renderer\nexport function Canvas() { return null; }\nconst x: string = 1;\n"
	out := "a.tsx(1,2): error TS1206: Decorators are not valid here.\n" +
		"a.tsx(1,2): error TS2304: Cannot find name 'renderer'.\n" +
		"a.tsx(3,7): error TS2322: Type 'number' is not assignable to type 'string'.\n"
	kept, remaining, dropped := Filter(out, func(string) (string, error) { return src, nil })
	if remaining != 1 || dropped != 2 {
		t.Fatalf("remaining=%d dropped=%d, kept:\n%s", remaining, dropped, kept)
	}
}
