package darwin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// Scheme is the custom URL scheme served by the app handler.
	Scheme = "krate"
	// Host is the fixed host of the app origin.
	Host = "app"
	// OriginURL is the app origin.
	OriginURL = Scheme + "://" + Host
)

var errBadOrigin = errors.New("darwin: request is not for " + OriginURL)

// navAllowed is the navigation lock policy. Main-frame (or new-window)
// navigations may only target the app origin; subframes may additionally load
// inert about/blob/data documents.
func navAllowed(scheme, host string, mainFrame bool) bool {
	scheme = strings.ToLower(scheme)
	if scheme == Scheme {
		return strings.EqualFold(host, Host)
	}
	if mainFrame {
		return false
	}
	switch scheme {
	case "about", "blob", "data":
		return true
	}
	return false
}

// newHTTPRequest builds the server-side request for a scheme task from plain
// values. The result looks like what net/http would hand a handler: URL holds
// only path and query, Host is the app host and Body is never nil.
func newHTTPRequest(ctx context.Context, method, rawURL string, fields map[string]string, body []byte) (*http.Request, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(u.Scheme, Scheme) || !strings.EqualFold(u.Host, Host) {
		return nil, errBadOrigin
	}
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, OriginURL+u.RequestURI(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.URL = &url.URL{Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
	req.RequestURI = u.RequestURI()
	req.Host = Host
	req.RemoteAddr = "krate-webview"
	req.Header = requestHeader(fields)
	if len(body) == 0 {
		req.Body = http.NoBody
		req.ContentLength = 0
	} else {
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}
	return req, nil
}
