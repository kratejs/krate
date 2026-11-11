package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
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
			select {
			case <-p.started:
			default:
				close(p.started)
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
func queryScript(id int, expr string) string {
	u := fmt.Sprintf("%s?id=%d", queryPath, id)
	return fmt.Sprintf(`(async()=>{try{await fetch(%q,{method:"POST",body:"1"})}catch(_){return}const gone=()=>{navigator.sendBeacon(%q,"1")};addEventListener("pagehide",gone);let b;try{const v=await (%s);b=JSON.stringify({ok:true,v:v===undefined?null:v})}catch(e){b=JSON.stringify({ok:false,error:String(e&&e.stack||e)})}removeEventListener("pagehide",gone);try{await fetch(%q,{method:"POST",body:b})}catch(_){}})()`,
		u+"&phase=start", u+"&phase=gone", expr, u)
}

// Query evaluates a JavaScript expression in every open window and returns its
// JSON-encoded value, awaiting a returned promise. It needs Dev. The first
// window to answer wins.
func (c *Control) Query(expr string) (string, error) {
	if !c.a.opts.Dev {
		return "", errors.New("query needs --dev")
	}
	id, p := c.a.results.open()
	defer c.a.results.close(id)
	deadline := time.After(queryTimeout)
	for started := false; !started; {
		if err := c.Eval(queryScript(id, expr)); err != nil {
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
	select {
	case body := <-p.result:
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
	case <-p.gone:
		return "", errors.New("query: the page unloaded before it answered")
	case <-deadline:
		return "", fmt.Errorf("query: no answer from the page after %s", queryTimeout)
	}
}
