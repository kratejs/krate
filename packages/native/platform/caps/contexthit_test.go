package caps

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseContextHit(t *testing.T) {
	info := ContextMenuInfo{Kinds: []string{"page"}}
	ParseContextHit(`{"link":"https://e.x/a","image":"data:image/gif;base64,AA","editable":true,"sel":"hi"}`, &info)
	if !reflect.DeepEqual(info.Kinds, []string{"page", "link", "image", "editable", "selection"}) ||
		info.LinkURL != "https://e.x/a" || info.ImageURL != "data:image/gif;base64,AA" || info.SelectionText != "hi" {
		t.Fatalf("%+v", info)
	}
	for _, bad := range []string{"", "not json", "[]", `{"link":3}`} {
		in := ContextMenuInfo{Kinds: []string{"page"}}
		ParseContextHit(bad, &in)
		if len(in.Kinds) != 1 || in.LinkURL != "" {
			t.Fatalf("%q changed the info: %+v", bad, in)
		}
	}
}

func TestContextHitScriptEmbedsThePoint(t *testing.T) {
	s := ContextHitScript(12, 34)
	if !strings.HasSuffix(s, "})(12,34)") || !strings.Contains(s, "elementFromPoint") {
		t.Fatal(s)
	}
}
