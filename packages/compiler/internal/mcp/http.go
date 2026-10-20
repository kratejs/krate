package mcp

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type mcpHTTPServer struct {
	svc      *Service
	token    string
	mu       sync.Mutex
	sessions map[string]*mcpHTTPSession
}

type sseEvent struct {
	id   int
	data []byte
}

type mcpHTTPSession struct {
	id      string
	srv     *Server
	in      *io.PipeWriter
	mu      sync.Mutex
	buf     []byte
	pending map[string]chan []byte
	subs    map[int]chan sseEvent
	nextSub int
	nextID  int
	history []sseEvent
}

func newMCPSession(svc *Service) *mcpHTTPSession {
	pr, pw := io.Pipe()
	s := &mcpHTTPSession{
		id:      newSessionID(),
		in:      pw,
		pending: map[string]chan []byte{},
		subs:    map[int]chan sseEvent{},
	}
	s.srv = NewServer(s, "krate", "http")
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
// sessions, SSE notifications, and request-scoped responses.
func (s *Service) HTTPHandler(token string) http.Handler {
	h := &mcpHTTPServer{svc: s, token: token, sessions: map[string]*mcpHTTPSession{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", h.handle)
	return mux
}

func (h *mcpHTTPServer) handle(w http.ResponseWriter, r *http.Request) {
	if !mcpAuthorized(r, h.token) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.handleSSE(w, r)
	case http.MethodPost:
		h.handlePost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *mcpHTTPServer) session(id string) *mcpHTTPSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	if id != "" {
		if s, ok := h.sessions[id]; ok {
			return s
		}
	}
	s := newMCPSession(h.svc)
	if id == "" {
		id = s.id
	}
	s.id = id
	h.sessions[id] = s
	return s
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
	sess := h.session(strings.TrimSpace(r.Header.Get("Mcp-Session-Id")))
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
	sess := h.session(strings.TrimSpace(r.Header.Get("Mcp-Session-Id")))
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
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.HTTPHandler(token),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}
