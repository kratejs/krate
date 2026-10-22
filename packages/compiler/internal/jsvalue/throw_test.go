package jsvalue

import "testing"

func TestInterpThrowAndCatch(t *testing.T) {
	caught := []struct{ src, want string }{
		{`(() => { try { throw new Error("boom"); } catch (e) { return e.message + "!"; } })()`, `'boom!'`},
		{`(() => { try { throw new TypeError("bad"); } catch (e) { return e.name; } })()`, `'TypeError'`},
		{`(() => { try { throw "plain"; } catch ({ message }) { return message; } })()`, `'plain'`},
		{`(() => { try { return 1; } finally { } })()`, `1`},
		{`new RangeError("r").message`, `'r'`},
	}
	for _, tc := range caught {
		got := evalSrc(t, tc.src)
		if src, ok := JSSource(got); !ok || src != tc.want {
			t.Errorf("%s = %#v, want %s", tc.src, got, tc.want)
		}
	}

	v := evalSrc(t, `(() => { if (count === 0) { throw new Error("zero " + name); } return 1; })()`)
	thrown := v.Thrown()
	if thrown == nil || thrown.Message != "zero bob" || thrown.Name != "Error" || !v.IsUnsupported() {
		t.Fatalf("thrown = %#v (%#v)", thrown, v)
	}
	if thrown.Line == 0 {
		t.Error("throw position missing")
	}
	// A throw inside a catch propagates.
	if v := evalSrc(t, `(() => { try { throw new Error("a"); } catch (e) { throw new Error("b"); } })()`); v.Thrown() == nil || v.Thrown().Message != "b" {
		t.Errorf("rethrow = %#v", v)
	}
}
