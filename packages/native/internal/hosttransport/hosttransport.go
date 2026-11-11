// Package hosttransport is the transport between the shell and a host child
// process, and between a child and the shell's SDK server.
//
// Unix sockets are the default. Node, Bun and Deno cannot connect to or listen
// on an AF_UNIX socket on Windows (a path given to net.connect there means a
// named pipe), so a JavaScript host child on Windows uses loopback TCP with a
// random token instead. The token is passed in the environment and every HTTP
// request must carry it, so another user's process that finds the port cannot
// call the child or the SDK.
//
// Addresses are strings:
//
//	unix:<path>
//	tcp:<host>:<port>?token=<hex>
//
// A bare path is a unix socket path (the form older shells used). The host of
// a tcp address must be a loopback IP literal.
package hosttransport

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Network names.
const (
	Unix = "unix"
	TCP  = "tcp"
)

// TokenHeader carries the token on every request of a tcp transport.
const TokenHeader = "X-Krate-Token"

// TokenEnv is the environment variable that carries the token to a child that
// listens on tcp.
const TokenEnv = "KRATE_GOHOST_TOKEN"

// ReadyLine is the line a child writes to stdout once it listens. A child on a
// tcp transport appends its address: "ready tcp:127.0.0.1:41234".
const ReadyLine = "ready"

// For picks the transport for a child on goos. jsChild is true for a Node,
// Bun or Deno child: those cannot use AF_UNIX on Windows. Go children keep
// unix sockets everywhere (AF_UNIX works for Go on Windows 10 and later).
func For(goos string, jsChild bool) string {
	if goos == "windows" && jsChild {
		return TCP
	}
	return Unix
}

// NewToken returns 128 random bits as hex.
func NewToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("hosttransport: no random token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Endpoint is a parsed address.
type Endpoint struct {
	Network string // Unix or TCP
	// Addr is the socket path (Unix) or host:port (TCP).
	Addr  string
	Token string // TCP only
}

// String formats the endpoint as an address.
func (e Endpoint) String() string {
	if e.Network == TCP {
		s := "tcp:" + e.Addr
		if e.Token != "" {
			s += "?token=" + url.QueryEscape(e.Token)
		}
		return s
	}
	return "unix:" + e.Addr
}

// Parse reads an address. A string with neither prefix is a unix path.
func Parse(s string) (Endpoint, error) {
	switch {
	case strings.HasPrefix(s, "tcp:"):
		rest := strings.TrimPrefix(s, "tcp:")
		var tok string
		if i := strings.IndexByte(rest, '?'); i >= 0 {
			q, err := url.ParseQuery(rest[i+1:])
			if err != nil {
				return Endpoint{}, fmt.Errorf("hosttransport: bad address %q: %w", s, err)
			}
			tok = q.Get("token")
			rest = rest[:i]
		}
		host, port, err := net.SplitHostPort(rest)
		if err != nil {
			return Endpoint{}, fmt.Errorf("hosttransport: bad tcp address %q: %w", s, err)
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return Endpoint{}, fmt.Errorf("hosttransport: tcp address %q must use a loopback IP address", s)
		}
		if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
			return Endpoint{}, fmt.Errorf("hosttransport: bad port in %q", s)
		}
		return Endpoint{Network: TCP, Addr: rest, Token: tok}, nil
	case strings.HasPrefix(s, "unix:"):
		p := strings.TrimPrefix(s, "unix:")
		if p == "" {
			return Endpoint{}, errors.New("hosttransport: empty unix socket path")
		}
		return Endpoint{Network: Unix, Addr: p}, nil
	case s == "":
		return Endpoint{}, errors.New("hosttransport: empty address")
	}
	return Endpoint{Network: Unix, Addr: s}, nil
}

// ListenTCP starts a loopback listener on an ephemeral port and returns it
// with the endpoint (host:port filled in) and a fresh token.
func ListenTCP() (net.Listener, Endpoint, error) {
	tok, err := NewToken()
	if err != nil {
		return nil, Endpoint{}, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, Endpoint{}, err
	}
	return ln, Endpoint{Network: TCP, Addr: ln.Addr().String(), Token: tok}, nil
}

// Listen opens the listener ep describes. For a tcp endpoint with port 0 the
// returned endpoint carries the port that was chosen.
func Listen(ep Endpoint) (net.Listener, Endpoint, error) {
	ln, err := net.Listen(ep.Network, ep.Addr)
	if err != nil {
		return nil, ep, err
	}
	if ep.Network == TCP {
		ep.Addr = ln.Addr().String()
	}
	return ln, ep, nil
}

// Guard rejects, with 401, requests that do not carry the endpoint's token.
// An endpoint without a token (unix) passes everything through.
func Guard(ep Endpoint, h http.Handler) http.Handler {
	if ep.Network != TCP {
		return h
	}
	want := []byte(ep.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get(TokenHeader))
		if len(want) == 0 || subtle.ConstantTimeCompare(got, want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// ReadyAddr reports whether line is a ready line and returns the address a
// tcp child announced on it ("" for a plain "ready").
func ReadyAddr(line string) (addr string, ok bool) {
	if line == ReadyLine {
		return "", true
	}
	if rest, found := strings.CutPrefix(line, ReadyLine+" tcp:"); found {
		if _, err := Parse("tcp:" + rest); err == nil {
			return rest, true
		}
	}
	return "", false
}

// ReadyLineFor formats the ready line for a listener of ep.
func ReadyLineFor(ep Endpoint) string {
	if ep.Network == TCP {
		return ReadyLine + " tcp:" + ep.Addr
	}
	return ReadyLine
}

type tokenTripper struct {
	rt    http.RoundTripper
	token string
}

func (t tokenTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set(TokenHeader, t.token)
	return t.rt.RoundTrip(r2)
}

// NewTransport returns an HTTP transport that dials ep whatever the URL host
// says, adding the token header for tcp. The returned *http.Transport is for
// CloseIdleConnections.
func NewTransport(ep Endpoint) (http.RoundTripper, *http.Transport) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, ep.Network, ep.Addr)
		},
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	}
	if ep.Network == TCP && ep.Token != "" {
		return tokenTripper{rt: tr, token: ep.Token}, tr
	}
	return tr, tr
}
