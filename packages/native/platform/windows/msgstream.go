package windows

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
)

// WebView2 reads the IStream of a WebResourceRequested response to its end
// before the page sees any of it (the stream "must have all the content data"
// when the deferral completes), so a response that never ends, like
// /__krate/signals or /__krate/hmr, would never arrive and its reader would
// stall WebView2. Streaming GETs therefore bypass WebResourceRequested: a
// script injected into every app page routes them through web messages. The
// page posts an open message, the handler runs here, and every chunk goes
// back as a message that the script turns into a ReadableStream body.
//
// Page to host (JSON text):
//
//	{"k":"open","id":7,"p":"/__krate/signals?names=a","h":[["x-krate-window","w1"]]}
//	{"k":"cancel","id":7}
//
// Host to page (JSON text):
//
//	{"k":"head","id":7,"s":200,"h":[["content-type","..."]]}
//	{"k":"data","id":7,"d":"<base64>"}
//	{"k":"end","id":7}
//	{"k":"err","id":7,"e":"message"}

const (
	maxStreamsPerView = 64
	maxStreamMsg      = 64 << 10
	dataChunk         = 32 << 10
)

// streamMsg is a message from the page.
type streamMsg struct {
	K  string      `json:"k"`
	ID int         `json:"id"`
	P  string      `json:"p"`
	H  [][2]string `json:"h"`
}

func parseStreamMsg(s string) (streamMsg, error) {
	var m streamMsg
	if len(s) > maxStreamMsg {
		return m, errors.New("windows: stream message too large")
	}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return m, err
	}
	if m.ID <= 0 {
		return m, errors.New("windows: stream message without an id")
	}
	switch m.K {
	case "open":
		if !validStreamPath(m.P) {
			return m, errors.New("windows: bad stream path")
		}
	case "cancel":
	default:
		return m, errors.New("windows: unknown stream message")
	}
	return m, nil
}

// validStreamPath accepts an absolute path on the app origin and nothing that
// could name another host.
func validStreamPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.ContainsAny(p, "\\\r\n") && len(p) < 8192
}

// streamTable tracks the open streams of one view.
type streamTable struct {
	mu     sync.Mutex
	m      map[int]context.CancelFunc
	closed bool
}

// add registers cancel under id. It reports false when the table is closed,
// full or already has id.
func (t *streamTable) add(id int, cancel context.CancelFunc) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || len(t.m) >= maxStreamsPerView {
		return false
	}
	if _, dup := t.m[id]; dup {
		return false
	}
	if t.m == nil {
		t.m = map[int]context.CancelFunc{}
	}
	t.m[id] = cancel
	return true
}

func (t *streamTable) remove(id int) {
	t.mu.Lock()
	delete(t.m, id)
	t.mu.Unlock()
}

// cancel stops the stream id if it is open.
func (t *streamTable) cancel(id int) {
	t.mu.Lock()
	c := t.m[id]
	t.mu.Unlock()
	if c != nil {
		c()
	}
}

// reopen lets a view that was closed (a released window coming back) take
// streams again.
func (t *streamTable) reopen() {
	t.mu.Lock()
	t.closed = false
	t.mu.Unlock()
}

// closeAll cancels every stream and refuses new ones.
func (t *streamTable) closeAll() {
	t.mu.Lock()
	t.closed = true
	cs := make([]context.CancelFunc, 0, len(t.m))
	for _, c := range t.m {
		cs = append(cs, c)
	}
	t.mu.Unlock()
	for _, c := range cs {
		c()
	}
}

// msgWriter is the http.ResponseWriter of a streamed response: it turns the
// head and every write into messages handed to send, in order.
type msgWriter struct {
	id     int
	send   func(string)
	mu     sync.Mutex
	header http.Header
	status int
	head   bool
	done   bool
}

func newMsgWriter(id int, send func(string)) *msgWriter {
	return &msgWriter{id: id, send: send, header: http.Header{}}
}

func (w *msgWriter) Header() http.Header { return w.header }

func (w *msgWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.headLocked(code)
}

func (w *msgWriter) headLocked(code int) {
	if w.head || w.done {
		return
	}
	if code >= 100 && code < 200 && code != 101 {
		return
	}
	w.head = true
	w.status = code
	pairs := make([][2]string, 0, len(w.header))
	for k, vs := range w.header {
		if hopByHop[http.CanonicalHeaderKey(k)] || k == "" {
			continue
		}
		for _, v := range vs {
			pairs = append(pairs, [2]string{cleanHeaderToken(k), cleanHeaderValue(v)})
		}
	}
	w.send(encodeMsg(map[string]any{"k": "head", "id": w.id, "s": code, "h": pairs}))
}

func (w *msgWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return 0, http.ErrHandlerTimeout
	}
	w.headLocked(http.StatusOK)
	for off := 0; off < len(p); off += dataChunk {
		end := min(off+dataChunk, len(p))
		w.send(encodeMsg(map[string]any{"k": "data", "id": w.id, "d": base64.StdEncoding.EncodeToString(p[off:end])}))
	}
	return len(p), nil
}

// Flush implements http.Flusher: the head goes out at once, data already does.
func (w *msgWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.headLocked(http.StatusOK)
}

// finish ends the stream; a handler that wrote nothing gets a 200 head first.
func (w *msgWriter) finish(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return
	}
	if err != nil && !w.head {
		w.done = true
		w.send(encodeMsg(map[string]any{"k": "err", "id": w.id, "e": err.Error()}))
		return
	}
	w.headLocked(http.StatusOK)
	w.done = true
	if err != nil {
		w.send(encodeMsg(map[string]any{"k": "err", "id": w.id, "e": err.Error()}))
		return
	}
	w.send(encodeMsg(map[string]any{"k": "end", "id": w.id}))
}

func encodeMsg(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// runStream serves one streamed GET with h, sending its messages through send.
// It returns when the handler does. A cancelled ctx ends the handler's reads;
// nothing is sent after that, the page already went away.
func runStream(ctx context.Context, h http.Handler, id int, path string, headers [][2]string, send func(string)) {
	req, err := buildRequest(ctx, http.MethodGet, Origin+path, headers, nil)
	w := newMsgWriter(id, send)
	if err != nil {
		w.finish(err)
		return
	}
	var herr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				herr = errors.New("internal error")
			}
		}()
		h.ServeHTTP(w, req)
	}()
	if ctx.Err() != nil {
		return
	}
	w.finish(herr)
}

// streamShim runs in every document of the app origin. It wraps fetch for the
// streaming GET endpoints and serves them over web messages (see msgstream.go).
// Other requests, other origins and documents without the WebView2 message
// channel use the original fetch.
const streamShim = `(function(){
if(location.origin!==` + `"` + Origin + `"` + `)return;
var wv=window.chrome&&window.chrome.webview;
if(!wv||window.__krateMsgStreams)return;
window.__krateMsgStreams=1;
var of=window.fetch.bind(window),open=Object.create(null),seq=0;
var ROUTE=/^\/__krate\/(stream\/|signals(\?|$)|hmr(\?|$))/;
function bytes(s){var b=atob(s),u=new Uint8Array(b.length);for(var i=0;i<b.length;i++)u[i]=b.charCodeAt(i);return u}
wv.addEventListener("message",function(e){
var m=e.data;if(typeof m!=="string")return;
try{m=JSON.parse(m)}catch(_){return}
var s=m&&open[m.id];if(!s)return;
if(m.k==="head")s.head(m);else if(m.k==="data")s.data(bytes(m.d));else if(m.k==="end")s.end();else if(m.k==="err")s.err(m.e||"stream failed");
});
addEventListener("pagehide",function(){for(var k in open){try{wv.postMessage(JSON.stringify({k:"cancel",id:Number(k)}))}catch(_){}}});
function abortError(){try{return new DOMException("The operation was aborted.","AbortError")}catch(_){var e=new Error("aborted");e.name="AbortError";return e}}
window.fetch=function(input,init){
var req=typeof Request!=="undefined"&&input instanceof Request?input:null,url;
try{url=new URL(req?req.url:String(input),location.href)}catch(_){return of(input,init)}
var method=String((init&&init.method)||(req&&req.method)||"GET").toUpperCase();
if(method!=="GET"||url.origin!==location.origin||!ROUTE.test(url.pathname+url.search))return of(input,init);
var hs=[];
try{new Headers((init&&init.headers)||(req&&req.headers)||{}).forEach(function(v,k){hs.push([k,v])})}catch(_){}
var signal=(init&&init.signal)||(req&&req.signal)||null;
var id=++seq,ctl=null,headSeen=false,fin=false;
return new Promise(function(resolve,reject){
function cleanup(){fin=true;delete open[id];if(signal)signal.removeEventListener("abort",onAbort)}
function onAbort(){
if(fin)return;
try{wv.postMessage(JSON.stringify({k:"cancel",id:id}))}catch(_){}
var err=abortError();cleanup();
if(headSeen&&ctl){try{ctl.error(err)}catch(_){}}else reject(err);
}
if(signal){if(signal.aborted){reject(abortError());return}signal.addEventListener("abort",onAbort)}
var body=new ReadableStream({
start:function(c){ctl=c},
cancel:function(){if(fin)return;try{wv.postMessage(JSON.stringify({k:"cancel",id:id}))}catch(_){}cleanup()}
});
open[id]={
head:function(m){
headSeen=true;
var nb=m.s===101||m.s===204||m.s===205||m.s===304;
try{resolve(new Response(nb?null:body,{status:m.s,headers:m.h}))}catch(e){cleanup();reject(e)}
if(nb)cleanup();
},
data:function(u){try{ctl.enqueue(u)}catch(_){}},
end:function(){if(fin)return;cleanup();try{ctl.close()}catch(_){}},
err:function(msg){if(fin)return;cleanup();var e=new TypeError(msg);if(headSeen){try{ctl.error(e)}catch(_){}}else reject(e)}
};
try{wv.postMessage(JSON.stringify({k:"open",id:id,p:url.pathname+url.search,h:hs}))}
catch(e){cleanup();reject(new TypeError(String(e)))}
});
};
})();`
