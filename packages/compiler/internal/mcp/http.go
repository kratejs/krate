package mcp

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type mcpHTTPServer struct {
	svc      *Service
	token    string
	mu       sync.Mutex
	sessions map[string]*mcpHTTPSession
	rate     *rateLimiter
	started  time.Time
	metrics  mcpHTTPMetrics
}

type mcpHTTPMetrics struct {
	requests       atomic.Int64
	unauthorized   atomic.Int64
	rateLimited    atomic.Int64
	originRejected atomic.Int64
}

type sseEvent struct {
	id   int
	data []byte
}

type mcpHTTPSession struct {
	id       string
	lastSeen time.Time
	srv      *Server
	in       *io.PipeWriter
	mu       sync.Mutex
	buf      []byte
	pending  map[string]chan []byte
	subs     map[int]chan sseEvent
	nextSub  int
	nextID   int
	history  []sseEvent
}

func newMCPSession(svc *Service) *mcpHTTPSession {
	pr, pw := io.Pipe()
	s := &mcpHTTPSession{
		id:       newSessionID(),
		lastSeen: time.Now(),
		in:       pw,
		pending:  map[string]chan []byte{},
		subs:     map[int]chan sseEvent{},
	}
	s.srv = NewServer(s, "krate", "http")
	s.srv.SetClient(s.id, true)
	svc.Register(s.srv)
	go func() { _ = s.srv.Serve(pr) }()
	return s
}

// Write routes each JSON-RPC frame to either an SSE subscriber (notifications)
// or the waiting HTTP request (responses). The Server writes one JSON object
// per call after flushing its encoder.
func (s *mcpHTTPSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			break
		}
		line := append([]byte(nil), s.buf[:i]...)
		s.buf = s.buf[i+1:]
		s.routeLocked(line)
	}
	s.mu.Unlock()
	return len(p), nil
}

func (s *mcpHTTPSession) routeLocked(line []byte) {
	var msg map[string]any
	if json.Unmarshal(line, &msg) != nil {
		return
	}
	if method, ok := msg["method"].(string); ok && strings.HasPrefix(method, "notifications/") {
		s.nextID++
		ev := sseEvent{id: s.nextID, data: append([]byte(nil), line...)}
		s.history = append(s.history, ev)
		if len(s.history) > 256 {
			s.history = s.history[len(s.history)-256:]
		}
		for _, ch := range s.subs {
			select {
			case ch <- ev:
			default:
			}
		}
		return
	}
	if id, ok := msg["id"]; ok {
		key := canonicalID(id)
		if ch, exists := s.pending[key]; exists {
			delete(s.pending, key)
			select {
			case ch <- append([]byte(nil), line...):
			default:
			}
		}
	}
}

func canonicalID(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *mcpHTTPSession) addPending(key string) chan []byte {
	ch := make(chan []byte, 1)
	s.mu.Lock()
	s.pending[key] = ch
	s.mu.Unlock()
	return ch
}

func (s *mcpHTTPSession) removePending(key string) {
	s.mu.Lock()
	delete(s.pending, key)
	s.mu.Unlock()
}

func (s *mcpHTTPSession) feed(line []byte) error {
	_, err := s.in.Write(append(line, '\n'))
	return err
}

func (s *mcpHTTPSession) subscribe(lastID int) ([]sseEvent, chan sseEvent, int) {
	ch := make(chan sseEvent, 32)
	s.mu.Lock()
	s.nextSub++
	id := s.nextSub
	s.subs[id] = ch
	var replay []sseEvent
	for _, ev := range s.history {
		if ev.id > lastID {
			replay = append(replay, ev)
		}
	}
	s.mu.Unlock()
	return replay, ch, id
}

func (s *mcpHTTPSession) unsubscribe(id int) {
	s.mu.Lock()
	delete(s.subs, id)
	s.mu.Unlock()
}

// HTTPHandler exposes the MCP service over Streamable HTTP with persistent
// sessions, SSE notifications, and request-scoped responses. It also exposes
// /healthz (unauthenticated liveness) and /metrics (bearer-protected
// Prometheus text) for hosted deployments.
func (s *Service) HTTPHandler(token string) http.Handler {
	limit, window := s.policy.rateLimit()
	h := &mcpHTTPServer{
		svc:      s,
		token:    token,
		sessions: map[string]*mcpHTTPSession{},
		rate:     newRateLimiter(limit, window),
		started:  time.Now(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", h.handle)
	mux.HandleFunc("/healthz", h.health)
	mux.HandleFunc("/metrics", h.metricsHandler)
	return mux
}

const (
	maxMCPSessions = 128
	mcpSessionTTL  = 30 * time.Minute
)

func (s *mcpHTTPSession) touch() {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

func (s *mcpHTTPSession) close() {
	_ = s.in.Close()
}

func (h *mcpHTTPServer) handle(w http.ResponseWriter, r *http.Request) {
	h.metrics.requests.Add(1)
	if !h.svc.policy.allowsHost(r.Host) || !originAllowed(r, h.svc.policy) {
		h.metrics.originRejected.Add(1)
		h.svc.recordHTTPReject(r, "host_or_origin")
		http.Error(w, "host or origin is not allowed", http.StatusForbidden)
		return
	}
	if !mcpAuthorized(r, h.token) {
		h.metrics.unauthorized.Add(1)
		h.svc.recordHTTPReject(r, "unauthorized")
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if ok, retry := h.rate.allow(rateLimitKey(r.RemoteAddr)); !ok {
		h.metrics.rateLimited.Add(1)
		h.svc.recordHTTPReject(r, "rate_limited")
		seconds := int(retry.Seconds() + 0.999)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.handleSSE(w, r)
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodDelete:
		id := strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))
		h.mu.Lock()
		sess := h.sessions[id]
		delete(h.sessions, id)
		h.mu.Unlock()
		if sess != nil {
			sess.close()
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *mcpHTTPServer) session(id string) (*mcpHTTPSession, bool) {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for sid, s := range h.sessions {
		if now.Sub(s.lastSeen) > mcpSessionTTL {
			delete(h.sessions, sid)
			s.close()
		}
	}
	if id != "" {
		if s, ok := h.sessions[id]; ok {
			s.touch()
			return s, true
		}
		return nil, false
	}
	if len(h.sessions) >= maxMCPSessions {
		return nil, false
	}
	s := newMCPSession(h.svc)
	h.sessions[s.id] = s
	return s, true
}

func (h *mcpHTTPServer) handlePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		http.Error(w, "empty request", http.StatusBadRequest)
		return
	}
	sess, ok := h.session(strings.TrimSpace(r.Header.Get("Mcp-Session-Id")))
	if !ok {
		http.Error(w, "unknown or exhausted MCP session", http.StatusNotFound)
		return
	}
	w.Header().Set("Mcp-Session-Id", sess.id)

	var msg struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
		return
	}
	if msg.Method == "" {
		http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
		return
	}
	if msg.ID == nil {
		if err := sess.feed(body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	key := canonicalID(msg.ID)
	ch := sess.addPending(key)
	defer sess.removePending(key)
	if err := sess.feed(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	select {
	case resp := <-ch:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	case <-r.Context().Done():
		http.Error(w, "request cancelled", http.StatusRequestTimeout)
	case <-time.After(5 * time.Minute):
		http.Error(w, "request timed out", http.StatusGatewayTimeout)
	}
}

func (h *mcpHTTPServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(strings.TrimSpace(r.Header.Get("Mcp-Session-Id")))
	if !ok {
		http.Error(w, "unknown or exhausted MCP session", http.StatusNotFound)
		return
	}
	w.Header().Set("Mcp-Session-Id", sess.id)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	lastID := 0
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		lastID, _ = strconv.Atoi(v)
	}
	replay, ch, sub := sess.subscribe(lastID)
	defer sess.unsubscribe(sub)
	writeEvent := func(ev sseEvent) bool {
		if _, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", ev.id, ev.data); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	for _, ev := range replay {
		if !writeEvent(ev) {
			return
		}
	}
	_, _ = io.WriteString(w, ": connected\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if !writeEvent(ev) {
				return
			}
		case <-ticker.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func mcpAuthorized(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	want := "Bearer " + token
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// originAllowed rejects browser cross-origin requests. Non-browser clients do
// not send Origin, so they are unaffected.
func originAllowed(r *http.Request, policy *Policy) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return false
	}
	return policy.allowsHost(u.Host)
}

func (h *mcpHTTPServer) health(w http.ResponseWriter, r *http.Request) {
	if !h.svc.policy.allowsHost(r.Host) {
		h.svc.recordHTTPReject(r, "host")
		http.Error(w, "host is not allowed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.mu.Lock()
	active := len(h.sessions)
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":         "ok",
		"uptimeSeconds":  int64(time.Since(h.started).Seconds()),
		"activeSessions": active,
	})
}

func (h *mcpHTTPServer) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.svc.policy.allowsHost(r.Host) {
		h.svc.recordHTTPReject(r, "host")
		http.Error(w, "host is not allowed", http.StatusForbidden)
		return
	}
	if !mcpAuthorized(r, h.token) {
		h.svc.recordHTTPReject(r, "unauthorized")
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.mu.Lock()
	active := len(h.sessions)
	h.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, "# HELP krate_mcp_http_requests_total HTTP requests to /mcp.\n")
	fmt.Fprintf(w, "# TYPE krate_mcp_http_requests_total counter\n")
	fmt.Fprintf(w, "krate_mcp_http_requests_total %d\n", h.metrics.requests.Load())
	fmt.Fprintf(w, "# HELP krate_mcp_http_unauthorized_total Requests rejected for missing or invalid credentials.\n")
	fmt.Fprintf(w, "# TYPE krate_mcp_http_unauthorized_total counter\n")
	fmt.Fprintf(w, "krate_mcp_http_unauthorized_total %d\n", h.metrics.unauthorized.Load())
	fmt.Fprintf(w, "# HELP krate_mcp_http_rate_limited_total Requests rejected by the rate limiter.\n")
	fmt.Fprintf(w, "# TYPE krate_mcp_http_rate_limited_total counter\n")
	fmt.Fprintf(w, "krate_mcp_http_rate_limited_total %d\n", h.metrics.rateLimited.Load())
	fmt.Fprintf(w, "# HELP krate_mcp_http_origin_rejected_total Requests rejected by host or origin checks.\n")
	fmt.Fprintf(w, "# TYPE krate_mcp_http_origin_rejected_total counter\n")
	fmt.Fprintf(w, "krate_mcp_http_origin_rejected_total %d\n", h.metrics.originRejected.Load())
	fmt.Fprintf(w, "# HELP krate_mcp_sessions_active Current persistent MCP sessions.\n")
	fmt.Fprintf(w, "# TYPE krate_mcp_sessions_active gauge\n")
	fmt.Fprintf(w, "krate_mcp_sessions_active %d\n", active)
}

func loopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("krate-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// ServeHTTP starts a localhost MCP HTTP server. Callers that need remote access
// should wrap this with TLS/reverse-proxy authentication.
func ServeHTTP(s *Service, addr, token string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid MCP address %q: %w", addr, err)
	}
	if token == "" && !loopbackHost(host) {
		return fmt.Errorf("refusing to expose unauthenticated MCP on %s; set a bearer token or bind loopback", addr)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.HTTPHandler(token),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}
