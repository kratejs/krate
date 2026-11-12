package caps

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// FindTracker follows the search of one view for engines that report how many
// matches there are but not which one is highlighted (WebKitGTK, WKWebView,
// a window.find fallback). It keeps the index of the highlighted match the way
// stepping through them moves it. Not safe for concurrent use.
type FindTracker struct {
	text       string
	matchCase  bool
	total      int
	active     int
	backward   bool
	continuing bool
}

// Text is the text of the running search, "" when there is none.
func (f *FindTracker) Text() string { return f.text }

// Total is the number of matches the last result reported.
func (f *FindTracker) Total() int { return f.total }

// Reset forgets the search.
func (f *FindTracker) Reset() { *f = FindTracker{} }

// Start registers a call of Find and reports whether it continues the search
// that is already running (same text and case rule, matches found, and
// o.Continue set) rather than starting a new one.
func (f *FindTracker) Start(text string, o FindOptions) (continuing bool) {
	if o.Continue && f.text == text && f.matchCase == o.MatchCase && f.total > 0 {
		f.continuing, f.backward = true, o.Backward
		return true
	}
	*f = FindTracker{text: text, matchCase: o.MatchCase, backward: o.Backward}
	return false
}

// Continuing reports whether the current call continues a search; Report then
// keeps the total of the search instead of the number the engine reports.
func (f *FindTracker) Continuing() bool { return f.continuing }

// Report records the number of matches an engine found and returns the 1-based
// index of the highlighted match (0 without matches).
func (f *FindTracker) Report(total int) int {
	if f.continuing && f.total > 0 {
		total = f.total // a continued search may report only the stepped match
	}
	f.total = total
	switch {
	case total == 0:
		f.active = 0
	case f.active == 0:
		f.active = 1
		if f.backward {
			f.active = total
		}
	case f.continuing && f.backward:
		f.active = (f.active+total-2)%total + 1
	case f.continuing:
		f.active = f.active%total + 1
	}
	if f.active > total {
		f.active = total
	}
	f.continuing = false
	return f.active
}

// FindCountScript is JavaScript that evaluates to the number of times text
// occurs in the visible text of the page. WKWebView and the window.find
// fallback of WebView2 have no match count of their own.
func FindCountScript(text string, matchCase bool) string {
	q, _ := json.Marshal(text)
	return fmt.Sprintf(`(function(){var q=%s,t=document.body?document.body.innerText:"";if(!q)return 0;`+
		`if(!%t){q=q.toLowerCase();t=t.toLowerCase()}var n=0,i=0;while((i=t.indexOf(q,i))>=0){n++;i+=q.length}return n})()`, q, matchCase)
}

// FindScript is JavaScript that runs window.find for a fallback engine and
// evaluates to the number of matches. A new search first clears the selection
// so that the first match is highlighted.
func FindScript(text string, o FindOptions, fresh bool) string {
	q, _ := json.Marshal(text)
	clear := ""
	if fresh {
		clear = "try{getSelection().removeAllRanges()}catch(e){}"
	}
	return fmt.Sprintf(`(function(){%s window.find(%s,%t,%t,%t,false,false,false);return %s})()`,
		clear, q, o.MatchCase, o.Backward, !o.NoWrap, FindCountScript(text, o.MatchCase))
}

// StopFindScript clears the highlight of a fallback search.
const StopFindScript = `(function(){try{getSelection().removeAllRanges()}catch(e){}return 0})()`

// CookieOrigin validates the origin or URL of a cookie call and returns the
// URL to query (scheme, host and port with a trailing slash) and the host.
// Only http and https can hold cookies in the web engines, so any other scheme
// is an error that wraps the one the caller gives for it.
func CookieOrigin(raw string, unsupported error) (uri, host string, err error) {
	u, perr := url.Parse(raw)
	if perr != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return "", "", fmt.Errorf("krate: %q is not a URL with a scheme and host", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("%w: web engines keep no cookies for the %s: scheme (the app origin on this platform); pass an http(s) URL", unsupported, u.Scheme)
	}
	return u.Scheme + "://" + u.Host + "/", u.Hostname(), nil
}
