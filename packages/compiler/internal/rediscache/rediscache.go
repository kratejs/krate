// Package rediscache is a small Redis client for the cluster invalidation bus:
// PUBLISH and SUBSCRIBE over RESP2, standard library only. The Node side has the
// same protocol in packages/runtime/src/resp.ts; both agree on the channel name
// (<prefix>c:invalidate).
package rediscache

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Options describe a Redis server.
type Options struct {
	Addr     string
	Username string
	Password string
	DB       int
	TLS      bool
	// Timeout bounds dialing and each command (default 500ms).
	Timeout time.Duration
}

// ParseURL reads redis://[user:password@]host[:port][/db] and rediss:// URLs.
func ParseURL(raw string) (Options, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Options{}, fmt.Errorf("invalid cache URL: %w", err)
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return Options{}, fmt.Errorf("unsupported cache URL scheme %q (want redis:// or rediss://)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	o := Options{Addr: net.JoinHostPort(host, port), TLS: u.Scheme == "rediss"}
	if u.User != nil {
		o.Username = u.User.Username()
		o.Password, _ = u.User.Password()
	}
	if p := strings.TrimPrefix(u.Path, "/"); p != "" {
		if o.DB, err = strconv.Atoi(p); err != nil {
			return Options{}, fmt.Errorf("invalid cache URL database %q", p)
		}
	}
	return o, nil
}

func (o Options) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return 500 * time.Millisecond
}

type conn struct {
	net.Conn
	r *bufio.Reader
	o Options
}

func dial(o Options) (*conn, error) {
	d := net.Dialer{Timeout: o.timeout()}
	var nc net.Conn
	var err error
	if o.TLS {
		host, _, _ := net.SplitHostPort(o.Addr)
		nc, err = tls.DialWithDialer(&d, "tcp", o.Addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		nc, err = d.Dial("tcp", o.Addr)
	}
	if err != nil {
		return nil, err
	}
	c := &conn{Conn: nc, r: bufio.NewReader(nc), o: o}
	if o.Password != "" {
		args := []string{"AUTH", o.Password}
		if o.Username != "" {
			args = []string{"AUTH", o.Username, o.Password}
		}
		if _, err := c.do(args...); err != nil {
			_ = nc.Close()
			return nil, err
		}
	}
	if o.DB != 0 {
		if _, err := c.do("SELECT", strconv.Itoa(o.DB)); err != nil {
			_ = nc.Close()
			return nil, err
		}
	}
	return c, nil
}

func (c *conn) write(args ...string) error {
	_ = c.SetWriteDeadline(time.Now().Add(c.o.timeout()))
	var sb strings.Builder
	fmt.Fprintf(&sb, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&sb, "$%d\r\n%s\r\n", len(a), a)
	}
	_, err := io.WriteString(c.Conn, sb.String())
	return err
}

func (c *conn) do(args ...string) (any, error) {
	if err := c.write(args...); err != nil {
		return nil, err
	}
	_ = c.SetReadDeadline(time.Now().Add(c.o.timeout()))
	return readReply(c.r)
}

// readReply parses one RESP2 reply: string, int64, nil, or []any.
func readReply(r *bufio.Reader) (any, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, errors.New("empty reply")
	}
	body := line[1:]
	switch line[0] {
	case '+':
		return body, nil
	case '-':
		return nil, errors.New(body)
	case ':':
		return strconv.ParseInt(body, 10, 64)
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		out := make([]any, n)
		for i := range out {
			if out[i], err = readReply(r); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected reply %q", line)
}

// Bus publishes and receives cluster invalidation messages on one channel.
type Bus struct {
	opts    Options
	channel string
}

// NewBus returns a bus on <prefix>c:invalidate.
func NewBus(o Options, prefix string) *Bus {
	if prefix == "" {
		prefix = "krate:"
	}
	return &Bus{opts: o, channel: prefix + "c:invalidate"}
}

// Publish sends msg to every subscriber. It dials per call: revalidations are
// rare, and a held connection would need its own failure handling.
func (b *Bus) Publish(msg string) error {
	c, err := dial(b.opts)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.do("PUBLISH", b.channel, msg)
	return err
}

// Subscribe calls handler for each message until stop is called. After a lost
// connection it reconnects with backoff and calls handler("*"): messages
// published meanwhile are gone, so everything may be stale.
func (b *Bus) Subscribe(handler func(msg string)) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var cur net.Conn
	go func() {
		delay := 200 * time.Millisecond
		first := true
		for {
			select {
			case <-done:
				return
			default:
			}
			c, err := dial(b.opts)
			if err == nil {
				err = c.write("SUBSCRIBE", b.channel)
			}
			if err != nil {
				if c != nil {
					_ = c.Close()
				}
				if !sleep(done, delay) {
					return
				}
				if delay < 5*time.Second {
					delay *= 2
				}
				continue
			}
			mu.Lock()
			cur = c.Conn
			mu.Unlock()
			delay = 200 * time.Millisecond
			if !first {
				handler("*")
			}
			first = false
			for {
				_ = c.SetReadDeadline(time.Time{})
				reply, err := readReply(c.r)
				if err != nil {
					break
				}
				if parts, ok := reply.([]any); ok && len(parts) == 3 && parts[0] == "message" {
					if s, ok := parts[2].(string); ok {
						handler(s)
					}
				}
			}
			_ = c.Close()
			if !sleep(done, delay) {
				return
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(done)
			mu.Lock()
			if cur != nil {
				_ = cur.Close()
			}
			mu.Unlock()
		})
	}
}

func sleep(done <-chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-done:
		return false
	case <-t.C:
		return true
	}
}
