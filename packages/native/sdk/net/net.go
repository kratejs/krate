// Package sdknet is the network area of the Krate Native SDK for Go host code.
//
// The webview has its own fetch, so there is no UI id for requests. What Go
// host code needs is an http.Client with sane timeouts and a way to attach a
// secret from the OS credential store to a request without the secret ever
// passing through the UI.
package sdknet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"golang.org/x/net/http/httpguts"
)

func init() {
	krate.RegisterCapability("net", func() bool { return true })
}

// DefaultTimeout bounds one whole request, including reading the body.
const DefaultTimeout = 60 * time.Second

var transport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
}

type secretHeadersKey struct{}

// Client returns an http.Client with DefaultTimeout, a shared connection pool
// and bounded dial, TLS and response-header waits. Redirects are limited to
// 10 and drop headers added with WithSecret when they leave the original host.
func Client() *http.Client { return ClientWithTimeout(DefaultTimeout) }

// ClientWithTimeout is Client with another overall timeout; 0 means none (use
// a context deadline for long downloads).
func ClientWithTimeout(d time.Duration) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   d,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("sdknet: stopped after 10 redirects")
			}
			if hs, _ := via[0].Context().Value(secretHeadersKey{}).([]string); len(hs) > 0 && req.URL.Host != via[0].URL.Host {
				for _, h := range hs {
					req.Header.Del(h)
				}
			}
			return nil
		},
	}
}

// getSecret is replaced in tests.
var getSecret = sdkos.SecretGet

// WithSecret sets header on req to the secret stored under secretKey. Include
// any scheme in the stored value ("Bearer abc"). It returns
// sdkos.ErrSecretNotFound when there is no such secret, and it replaces req's
// context, so use the request afterwards, not a copy made earlier. A client
// from Client does not forward the header to another host on redirect.
func WithSecret(ctx context.Context, req *http.Request, secretKey, header string) error {
	if !httpguts.ValidHeaderFieldName(header) {
		return fmt.Errorf("sdknet: %q is not a valid header name", header)
	}
	v, err := getSecret(ctx, secretKey)
	if err != nil {
		return err
	}
	if !httpguts.ValidHeaderFieldValue(v) {
		return fmt.Errorf("sdknet: secret %q is not a valid header value", secretKey)
	}
	prev, _ := req.Context().Value(secretHeadersKey{}).([]string)
	hs := append(append([]string(nil), prev...), http.CanonicalHeaderKey(header))
	*req = *req.WithContext(context.WithValue(req.Context(), secretHeadersKey{}, hs))
	req.Header.Set(header, v)
	return nil
}

// Register is part of every area's contract; the net area has no UI ids (the
// webview has fetch), so it registers nothing. The "net" permission is
// reserved for a future network policy.
func Register(r *scheme.Registry, set permission.Set) {}
