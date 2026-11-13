package windows

import (
	"encoding/json"
	"errors"
)

// A page cannot see the path of a file the user drops on it, but WebView2 can
// hand it to the host: chrome.webview.postMessageWithAdditionalObjects sends
// File objects along with a message, and the host reads ICoreWebView2File.Path
// from the message's additional objects. dropShim, injected into every
// document of the app origin, posts the files of each drop in the capture
// phase, so the page still gets its own drop event first-hand and nothing is
// cancelled. The message is JSON text on the channel the streamed responses
// use:
//
//	{"k":"drop","x":120,"y":80}   plus the File objects as additional objects

const maxDropMsg = 1 << 10

// dropMsg is the message the shim posts.
type dropMsg struct {
	K string `json:"k"`
	X int    `json:"x"`
	Y int    `json:"y"`
}

// parseDropMsg reports whether s is a drop message, and its position.
func parseDropMsg(s string) (m dropMsg, isDrop bool, err error) {
	if len(s) > maxDropMsg {
		return m, false, nil
	}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return dropMsg{}, false, err
	}
	if m.K != "drop" {
		return dropMsg{}, false, nil
	}
	if m.X < 0 || m.Y < 0 || m.X > 1<<20 || m.Y > 1<<20 {
		return dropMsg{}, false, errors.New("windows: drop position out of range")
	}
	return m, true, nil
}

// dropShim posts the files of a drop to the host. It does nothing where the
// WebView2 runtime lacks postMessageWithAdditionalObjects, in other origins
// and in frames.
const dropShim = `(function(){
if(location.origin!==` + `"` + Origin + `"` + `)return;
var wv=window.chrome&&window.chrome.webview;
if(!wv||typeof wv.postMessageWithAdditionalObjects!=="function"||window.__krateDrop)return;
if(window.top!==window)return;
window.__krateDrop=1;
addEventListener("drop",function(e){
try{
var dt=e.dataTransfer;
if(!dt||!dt.files||!dt.files.length)return;
wv.postMessageWithAdditionalObjects(JSON.stringify({k:"drop",x:Math.max(0,Math.round(e.clientX)||0),y:Math.max(0,Math.round(e.clientY)||0)}),Array.prototype.slice.call(dt.files));
}catch(_){}
},true);
})();`
