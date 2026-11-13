package caps

import (
	"encoding/json"
	"fmt"
)

// ContextHitScript returns JavaScript that describes the element at the
// view coordinates x, y as a JSON string, for backends whose engine does not
// report what was right-clicked (WKWebView). ParseContextHit reads the answer.
func ContextHitScript(x, y int) string {
	return fmt.Sprintf(`(function(x,y){var o={};try{var e=document.elementFromPoint(x,y);if(e&&e.closest){`+
		`var a=e.closest('a[href]');if(a)o.link=a.href;`+
		`var i=e.closest('img');if(i)o.image=i.currentSrc||i.src;`+
		`var m=e.closest('video,audio');if(m)o.media=m.currentSrc||m.src;`+
		`var d=e.closest('input,textarea,[contenteditable]');`+
		`if(d){var t=(d.type||'').toLowerCase();var text=d.tagName!=='INPUT'||/^(|text|search|url|tel|email|password|number)$/.test(t);`+
		`o.editable=!!(text&&!d.readOnly&&!d.disabled&&(d.tagName!=='DIV'&&d.tagName!=='SPAN'||d.isContentEditable));}}`+
		`o.sel=String(getSelection());}catch(_){}return JSON.stringify(o)})(%d,%d)`, x, y)
}

// ParseContextHit adds the answer of ContextHitScript to info. An empty or
// invalid answer leaves info as it is.
func ParseContextHit(raw string, info *ContextMenuInfo) {
	var h struct {
		Link     string `json:"link"`
		Image    string `json:"image"`
		Media    string `json:"media"`
		Editable bool   `json:"editable"`
		Sel      string `json:"sel"`
	}
	if raw == "" || json.Unmarshal([]byte(raw), &h) != nil {
		return
	}
	add := func(kind string) {
		if !info.Has(kind) {
			info.Kinds = append(info.Kinds, kind)
		}
	}
	if h.Link != "" {
		add("link")
		info.LinkURL = h.Link
	}
	if h.Image != "" {
		add("image")
		info.ImageURL = h.Image
	}
	if h.Media != "" {
		add("media")
		info.MediaURL = h.Media
	}
	if h.Editable {
		add("editable")
	}
	if h.Sel != "" {
		add("selection")
		info.SelectionText = h.Sel
	}
}
