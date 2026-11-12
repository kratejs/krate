package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/sdk"
)

const (
	queryPath    = "/__krate/dev/result"
	queryTimeout = 45 * time.Second
	// startWait is how long Query waits for the page to say it is running
	// the script before it sends the script again (the window can still be
	// loading when the first attempt is made).
	defaultStartWait = 2 * time.Second
)

// startWait is a variable so tests can shorten it.
var startWait = defaultStartWait

// pingEvery is how often a single-view query checks that its page is still
// the one showing.
var pingEvery = 500 * time.Millisecond

// queryResults matches the pages' answers to the queries waiting for them.
// Only dev builds route requests here.
type queryResults struct {
	mu      sync.Mutex
	next    int
	waiting map[int]*pending
}

type pending struct {
	started chan struct{} // closed when the page runs the script
	gone    chan struct{} // closed when the page unloads before answering
	result  chan []byte
	doc     string // the document token the script started in
}

func (q *queryResults) open() (int, *pending) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.waiting == nil {
		q.waiting = map[int]*pending{}
	}
	q.next++
	p := &pending{started: make(chan struct{}), gone: make(chan struct{}), result: make(chan []byte, 1)}
	q.waiting[q.next] = p
	return q.next, p
}

func (q *queryResults) close(id int) {
	q.mu.Lock()
	delete(q.waiting, id)
	q.mu.Unlock()
}

// serve handles POST /__krate/dev/result?id=N and reports whether the request
// was one.
func (q *queryResults) serve(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != queryPath {
		return false
	}
	var id int
	if _, err := fmt.Sscanf(r.URL.Query().Get("id"), "%d", &id); err != nil || r.Method != http.MethodPost {
		http.Error(w, "bad query result", http.StatusBadRequest)
		return true
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return true
	}
	q.mu.Lock()
	p := q.waiting[id]
	q.mu.Unlock()
	if p != nil {
		switch r.URL.Query().Get("phase") {
		case "start":
			q.mu.Lock()
			if p.doc == "" {
				p.doc = string(body)
			}
			q.mu.Unlock()
			select {
			case <-p.started:
			default:
				close(p.started)
			}
		case "ping":
			// A ping answered by another document means the page that ran
			// the query has gone (sendBeacon cannot reach krate://app, so
			// this is how WebKit notices a navigation).
			q.mu.Lock()
			moved := p.doc != "" && string(body) != p.doc
			q.mu.Unlock()
			if moved {
				select {
				case <-p.gone:
				default:
					close(p.gone)
				}
			}
		case "gone":
			select {
			case <-p.gone:
			default:
				close(p.gone)
			}
		default:
			select {
			case p.result <- body:
			default:
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}

// queryScript wraps an expression so the page first says it is running (a
// page that has not loaded yet cannot reach the endpoint and does nothing),
// then posts the value (a promise is awaited) back as JSON. A page that
// unloads before answering reports that instead, so a query racing a
// navigation fails at once.
// docToken names the current document: set once per page load, so a new
// page after a navigation has a different one.
const docToken = `(globalThis.__krateDoc??=Math.random().toString(36).slice(2)+Date.now())`

// pingScript reports the current document's token for a pending query.
func pingScript(id int) string {
	return fmt.Sprintf(`fetch(%q,{method:"POST",body:`+docToken+`}).catch(()=>{})`, fmt.Sprintf("%s?id=%d&phase=ping", queryPath, id))
}

func queryScript(id int, expr string) string {
	u := fmt.Sprintf("%s?id=%d", queryPath, id)
	return fmt.Sprintf(`(async()=>{try{await fetch(%q,{method:"POST",body:`+docToken+`})}catch(_){return}const gone=()=>{navigator.sendBeacon(%q,"1")};addEventListener("pagehide",gone);let b;try{const v=await (%s);b=JSON.stringify({ok:true,v:v===undefined?null:v})}catch(e){b=JSON.stringify({ok:false,error:e&&e.message!==undefined?String(e.name||"Error")+": "+e.message+(e.stack?"\n"+e.stack:""):String(e)})}try{await fetch(%q,{method:"POST",body:b})}catch(_){}removeEventListener("pagehide",gone)})()`,
		u+"&phase=start", u+"&phase=gone", expr, u)
}

// Query evaluates a JavaScript expression in every open window and returns its
// JSON-encoded value, awaiting a returned promise. It needs Dev. The first
// window to answer wins.
func (c *Control) Query(expr string) (string, error) {
	return c.query(c.Eval, expr, false)
}

// QueryView is Query for one view, named by its SDK id (a window's primary
// view has the window's id, "main" for the first).
func (c *Control) QueryView(view, expr string) (string, error) {
	if c.a.sdk == nil {
		return "", errors.New("query-in needs native windows")
	}
	known := c.a.sdk.Views()
	found := false
	ids := make([]string, 0, len(known))
	for _, v := range known {
		ids = append(ids, v.ID)
		found = found || v.ID == view
	}
	if !found {
		return "", fmt.Errorf("no view %q (open views: %s)", view, strings.Join(ids, ", "))
	}
	return c.query(func(js string) error { return c.a.sdk.EvalView(view, js) }, expr, true)
}

// Views lists the open views.
func (c *Control) Views() []sdk.ViewInfo {
	if c.a.sdk == nil {
		return nil
	}
	return c.a.sdk.Views()
}

// Emit delivers a native event (tray, menu or shortcut) to the app as if the
// platform had produced it; payload is a JSON object.
func (c *Control) Emit(area, payload string) error {
	if c.a.sdk == nil || !c.a.opts.Dev {
		return errors.New("emit needs --dev")
	}
	return c.a.sdk.Inject(area, []byte(payload))
}

// query runs expr through eval. With one view (single), it also pings the
// view while waiting, so a navigation away from the page ends the query at
// once instead of at the timeout.
func (c *Control) query(eval func(js string) error, expr string, single bool) (string, error) {
	if !c.a.opts.Dev {
		return "", errors.New("query needs --dev")
	}
	id, p := c.a.results.open()
	defer c.a.results.close(id)
	deadline := time.After(queryTimeout)
	for started := false; !started; {
		if err := eval(queryScript(id, expr)); err != nil {
			return "", err
		}
		select {
		case <-p.started:
			started = true
		case <-time.After(startWait):
		case <-deadline:
			return "", fmt.Errorf("query: the page did not run the script within %s", queryTimeout)
		}
	}
	var ping <-chan time.Time
	if single {
		t := time.NewTicker(pingEvery)
		defer t.Stop()
		ping = t.C
	}
	for {
		select {
		case body := <-p.result:
			return decodeAnswer(body)
		case <-ping:
			_ = eval(pingScript(id))
			continue
		case <-p.gone:
			// The answer may have arrived just before the page left.
			select {
			case body := <-p.result:
				return decodeAnswer(body)
			default:
			}
			return "", errors.New("query: the page unloaded before it answered")
		case <-deadline:
			return "", fmt.Errorf("query: no answer from the page after %s", queryTimeout)
		}
	}
}

// decodeAnswer reads the answer the query script posted.
func decodeAnswer(body []byte) (string, error) {
	var res struct {
		OK    bool            `json:"ok"`
		V     json.RawMessage `json:"v"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("query: unreadable answer: %v", err)
	}
	if !res.OK {
		return "", errors.New(res.Error)
	}
	return string(res.V), nil
}
