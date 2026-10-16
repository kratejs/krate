package krate

// Request is the incoming request context. A runtime Go component
// (`*.runtime.go`) may declare a second parameter of this type to read the
// method, URL, route params, query, and headers:

//	func Show(p ShowProps, r kr.Request) kr.HTML {
//	    return kr.HTML("<h1>" + kr.Escape(r.Params["id"]) + "</h1>")
//	}
//
// It is empty for server-tier (build-time) components.
type Request struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url,omitempty"`
	Params  map[string]string `json:"params,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Param returns a route parameter (convenience for r.Params[name]).
func (r Request) Param(name string) string { return r.Params[name] }

// QueryValue returns a query-string value (convenience for r.Query[name]).
func (r Request) QueryValue(name string) string { return r.Query[name] }
