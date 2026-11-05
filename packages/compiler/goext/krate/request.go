package krate

import (
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Request is the incoming request context. A runtime Go component
// (`*.runtime.go`) may declare a second parameter of this type to read the
// method, URL, route params, query, and headers:
//
//	func Show(p ShowProps, r kr.Request) kr.HTML {
//	    return kr.HTML("<h1>" + kr.Escape(r.Param("id")) + "</h1>")
//	}
//
// It is empty for server-tier (build-time) components.
//
// Read the request through the accessors (Header, Cookie, QueryValue, Param):
// they record what the render looked at, and Krate caches the render of a
// component that declares `revalidate` per those values only, so a visitor's
// cookie or a random query string cannot split or share an entry by accident.
// A component that reads the Method, URL, Params, Query or Headers fields
// directly, or hands the Request to another function, is cached per whole
// request instead.
type Request struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url,omitempty"`
	Params  map[string]string `json:"params,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	reads *Reads
}

// Vary lists what one render read from the request, like the Vary header of
// an HTTP response. Names are lower-case for headers and as sent otherwise.
type Vary struct {
	// Full is set when the component reads the request in a way that cannot be
	// recorded (its fields, or the Request passed on): the render varies on
	// the whole request.
	Full    bool     `json:"full,omitempty"`
	Headers []string `json:"headers,omitempty"`
	Cookies []string `json:"cookies,omitempty"`
	Query   []string `json:"query,omitempty"`
	Params  []string `json:"params,omitempty"`
}

// Reads is the recorder of one render. The generated component registry
// attaches it with Request.Track and reads it back with Vary.
type Reads struct {
	mu   sync.Mutex
	full bool
	seen map[string]bool // kind byte + ":" + name
}

// Track attaches a recorder to the request and returns it. Copies of the
// request (it is passed by value) share it.
func (r *Request) Track() *Reads {
	rd := &Reads{}
	r.reads = rd
	return rd
}

// MarkFull records that the render varies on the whole request.
func (rd *Reads) MarkFull() {
	rd.mu.Lock()
	rd.full = true
	rd.mu.Unlock()
}

// Vary returns what has been read so far.
func (rd *Reads) Vary() Vary {
	rd.mu.Lock()
	defer rd.mu.Unlock()
	v := Vary{Full: rd.full}
	for k := range rd.seen {
		name := k[2:]
		switch k[0] {
		case 'h':
			v.Headers = append(v.Headers, name)
		case 'c':
			v.Cookies = append(v.Cookies, name)
		case 'q':
			v.Query = append(v.Query, name)
		case 'p':
			v.Params = append(v.Params, name)
		}
	}
	sort.Strings(v.Headers)
	sort.Strings(v.Cookies)
	sort.Strings(v.Query)
	sort.Strings(v.Params)
	return v
}

func (rd *Reads) note(kind byte, name string) {
	if rd == nil {
		return
	}
	rd.mu.Lock()
	if rd.seen == nil {
		rd.seen = map[string]bool{}
	}
	rd.seen[string(kind)+":"+name] = true
	rd.mu.Unlock()
}

// Param returns a route parameter (convenience for r.Params[name]).
func (r Request) Param(name string) string {
	r.reads.note('p', name)
	return r.Params[name]
}

// QueryValue returns a query-string value (convenience for r.Query[name]).
func (r Request) QueryValue(name string) string {
	r.reads.note('q', name)
	return r.Query[name]
}

// Header returns a request header, matched case-insensitively. "" when absent.
func (r Request) Header(name string) string {
	r.reads.note('h', strings.ToLower(name))
	return r.header(name)
}

func (r Request) header(name string) string {
	for k, v := range r.Headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// Cookie returns a cookie value by name. "" when absent.
func (r Request) Cookie(name string) string {
	r.reads.note('c', name)
	raw := r.header("Cookie")
	if raw == "" {
		return ""
	}
	probe := http.Request{Header: http.Header{"Cookie": []string{raw}}}
	c, err := probe.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
