// Package mcp implements `krate mcp`, an MCP (Model Context Protocol) server
// that exposes the Krate compiler to agents over JSON-RPC 2.0.
// Transport is stdio by default (one JSON object per line over stdin/stdout),
// which is what Claude Desktop/Code, Cursor, VS Code, and opencode expect. The
// dispatcher is transport-agnostic so an HTTP mode can be layered on later.
// There is no MCP SDK dependency: the protocol surface Krate needs
// (initialize, tools/list, tools/call, resources/list, resources/read,
// resources/templates/list, prompts/list, prompts/get, completions/complete,
// notifications) is implemented directly with encoding/json.
// Protocol details:
//   - Initialize negotiates the client's protocol version against the set of
//
// versions this server supports (2024-11-05 ... 2025-11-25) and advertises
//
//	  capabilities (tools, resources, prompts, completions) plus instructions.
//	- Tool handlers run in goroutines but execute under a single mutex, so the
//	  read loop stays live to observe notifications (e.g. cancelled) while a
//	  long operation is running, without letting build/check output capture
//	  overlap.
//	- notifications/cancelled marks the referenced request; clients receive a
//	  -32800 (RequestCancelled) error rather than a result.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// JSON-RPC 2.0 error codes.
const (
	codeParseError       = -32700
	codeInvalidRequest   = -32600
	codeMethodNotFound   = -32601
	codeInvalidParams    = -32602
	codeInternalError    = -32603
	codeRequestCancelled = -32800
)

// mcpVersions lists the protocol revisions this server supports, oldest first.
// The latest supported version is advertised when the client asks for a newer
// one or sends none.
var mcpVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}

// request is an incoming JSON-RPC message. Notifications omit ID.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r request) isNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// response is an outgoing JSON-RPC message.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

// Server speaks MCP over a pair of streams.
type Server struct {
	out                   *bufio.Writer
	suppressNotifications bool
	outMu                 sync.Mutex
	pmu                   sync.Mutex // guards inflight
	server                *serverInfo

	tools         map[string]Tool
	toolOrder     []string
	resources     map[string]Resource
	resOrder      []string
	templates     map[string]*ResourceTemplate
	tmplOrder     []string
	prompts       map[string]*Prompt
	promptOrd     []string
	completes     map[string]completionFn
	subscriptions map[string]bool

	// reqCh is the fifo queue consumed by the worker goroutine. Requests are
	// processed strictly in order, so responses are emitted in request order.
	reqCh chan dispatchItem
	done  chan struct{}

	// inflight tracks pending requests by canonical ID for cancellation.
	inflight map[string]*pendingOp

	// initialized tracks the handshake so tools/list before initialize can be
	// tolerated (some clients do this); it is informational only.
	initialized bool

	redactMu sync.RWMutex
	redactor func(string) string
	auditor  func(auditRecord)
	clientID string
	remote   bool
}

// dispatchItem is an enqueued request together with its cancellation state.
type dispatchItem struct {
	req request
	op  *pendingOp
}

// serverInfo identifies Krate to the client.
type serverInfo struct {
	Name    string
	Version string
}

// pendingOp is the cancel state for a single in-flight request.
type pendingOp struct {
	ctx           context.Context
	cancel        context.CancelFunc
	cancelled     *boolFlag
	progressToken any
}

type boolFlag struct {
	mu    sync.Mutex
	value bool
}

func (b *boolFlag) set() {
	b.mu.Lock()
	b.value = true
	b.mu.Unlock()
}

func (b *boolFlag) get() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.value
}

// NewServer creates a server writing JSON-RPC responses to out.
func NewServer(out io.Writer, name, version string) *Server {
	bw := bufio.NewWriter(out)
	return &Server{
		out:           bw,
		server:        &serverInfo{Name: name, Version: version},
		tools:         make(map[string]Tool),
		resources:     make(map[string]Resource),
		templates:     make(map[string]*ResourceTemplate),
		prompts:       make(map[string]*Prompt),
		completes:     make(map[string]completionFn),
		subscriptions: make(map[string]bool),
		inflight:      make(map[string]*pendingOp),
	}
}

// SetRedactor installs a text redactor applied to every outgoing JSON frame.
func (s *Server) SetRedactor(fn func(string) string) {
	s.redactMu.Lock()
	s.redactor = fn
	s.redactMu.Unlock()
}

// SetAuditHook installs a tool-call audit sink.
func (s *Server) SetAuditHook(fn func(auditRecord)) {
	s.redactMu.Lock()
	s.auditor = fn
	s.redactMu.Unlock()
}

// SetClient identifies the transport client for audit records.
func (s *Server) SetClient(id string, remote bool) {
	s.redactMu.Lock()
	s.clientID = id
	s.remote = remote
	s.redactMu.Unlock()
}

func (s *Server) applyRedaction(text string) string {
	s.redactMu.RLock()
	fn := s.redactor
	s.redactMu.RUnlock()
	if fn != nil {
		return fn(text)
	}
	return text
}

func (s *Server) audit(rec auditRecord) {
	s.redactMu.RLock()
	fn := s.auditor
	rec.Client = s.clientID
	rec.Remote = s.remote
	s.redactMu.RUnlock()
	if fn != nil {
		fn(rec)
	}
}

// RegisterTool adds a tool. Registration order is preserved in tools/list.
func (s *Server) RegisterTool(t Tool) {
	if _, exists := s.tools[t.Name]; !exists {
		s.toolOrder = append(s.toolOrder, t.Name)
	}
	s.tools[t.Name] = t
}

// RegisterResource adds a static resource.
func (s *Server) RegisterResource(r Resource) {
	if _, exists := s.resources[r.URI]; !exists {
		s.resOrder = append(s.resOrder, r.URI)
	}
	s.resources[r.URI] = r
}

// RegisterResourceTemplate adds a parameterized resource exposed via
// resources/templates/list and URI-template matching on resources/read.
func (s *Server) RegisterResourceTemplate(t *ResourceTemplate) {
	if _, exists := s.templates[t.URITemplate]; !exists {
		s.tmplOrder = append(s.tmplOrder, t.URITemplate)
	}
	s.templates[t.URITemplate] = t
}

// RegisterPrompt adds a curated prompt surfaced via prompts/list and prompts/get.
func (s *Server) RegisterPrompt(p *Prompt) {
	if _, exists := s.prompts[p.Name]; !exists {
		s.promptOrd = append(s.promptOrd, p.Name)
	}
	s.prompts[p.Name] = p
}

// RegisterCompletion adds an argument-completion provider for a resource
// template URI ("resource") or a prompt name ("prompt").
func (s *Server) RegisterCompletion(refType, refName, argName string, fn completionFn) {
	if fn == nil {
		return
	}
	s.completes[refKey(refType, refName, argName)] = fn
}

// Serve runs the read/dispatch loop until EOF. It returns when the input
// stream ends (after in-flight handlers drain); individual request errors are
// reported to the client and do not terminate the loop. Requests are processed
// in strict arrival order by a single worker, so responses are emitted in
// request order. Cancellation and exit notifications are handled inline in the
// read loop, so they are observed even while a long handler is running.
func (s *Server) Serve(in io.Reader) error {
	s.reqCh = make(chan dispatchItem, 256)
	s.done = make(chan struct{})
	go s.worker()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
loop:
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(trimSpace(line)) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, codeParseError, "parse error: "+err.Error())
			continue
		}
		switch req.Method {
		case "notifications/cancelled", "cancelled":
			s.cancelRequest(req.Params)
			continue
		case "exit":
			break loop
		case "shutdown":
			// Let the worker emit the shutdown result, then stop reading.
			s.dispatch(req)
			break loop
		}
		s.dispatch(req)
	}
	close(s.reqCh)
	<-s.done
	return scanner.Err()
}

// worker drains the request queue in order, writing one response at a time.
func (s *Server) worker() {
	defer close(s.done)
	for item := range s.reqCh {
		op := item.op
		req := item.req
		if !req.isNotification() && op.cancelled.get() {
			s.writeError(req.ID, codeRequestCancelled, "request cancelled")
			s.finishPending(req, op)
			continue
		}
		s.handle(req, op)
		s.finishPending(req, op)
	}
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}

// dispatch registers the request as pending and enqueues it. The single worker
// executes handlers in order, so output capture (os.Stdout swapping) and
// response ordering are naturally serialized.
func (s *Server) dispatch(req request) {
	op := s.registerPending(req)
	s.reqCh <- dispatchItem{req: req, op: op}
}

// registerPending records a request so a later notifications/cancelled can
// reference it. Notifications return a no-op op.
func (s *Server) registerPending(req request) *pendingOp {
	ctx, cancel := context.WithCancel(context.Background())
	op := &pendingOp{ctx: ctx, cancel: cancel, cancelled: &boolFlag{}}
	if req.isNotification() {
		return op
	}
	s.pmu.Lock()
	s.inflight[string(req.ID)] = op
	s.pmu.Unlock()
	return op
}

func (s *Server) finishPending(req request, op *pendingOp) {
	if !req.isNotification() {
		s.pmu.Lock()
		if cur, ok := s.inflight[string(req.ID)]; ok && cur == op {
			delete(s.inflight, string(req.ID))
		}
		s.pmu.Unlock()
	}
	op.cancel()
}

// cancelRequest marks the referenced request as cancelled.
func (s *Server) cancelRequest(raw json.RawMessage) {
	var params struct {
		RequestID json.RawMessage `json:"requestId"`
		Reason    string          `json:"reason,omitempty"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	_ = params.Reason
	s.pmu.Lock()
	op, ok := s.inflight[string(params.RequestID)]
	s.pmu.Unlock()
	if !ok || op == nil {
		return
	}
	op.cancelled.set()
	op.cancel()
}

// handle dispatches one request. Notifications produce no response.
func (s *Server) handle(req request, op *pendingOp) {
	notification := req.isNotification()

	var result any
	var rerr *rpcError

	switch req.Method {
	case "initialize":
		result = s.initializeResult(req.Params)
	case "notifications/initialized", "initialized":
		s.initialized = true
		return
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = s.listTools()
	case "tools/call":
		result, rerr = s.callTool(req.Params, op)
	case "resources/list":
		result = s.listResources()
	case "resources/read":
		result, rerr = s.readResource(req.Params)
	case "resources/templates/list":
		result = s.listResourceTemplates()
	case "resources/subscribe":
		result, rerr = s.setSubscription(req.Params, true)
	case "resources/unsubscribe":
		result, rerr = s.setSubscription(req.Params, false)
	case "prompts/list":
		result = s.listPrompts()
	case "prompts/get":
		result, rerr = s.getPrompt(req.Params)
	case "completions/complete":
		result, rerr = s.complete(req.Params)
	case "shutdown":
		result = map[string]any{}
	default:
		if notification {
			return
		}
		rerr = &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}

	if notification {
		return
	}
	if op.cancelled.get() {
		s.writeError(req.ID, codeRequestCancelled, "request cancelled")
		return
	}
	if rerr != nil {
		s.writeError(req.ID, rerr.Code, rerr.Message)
		return
	}
	s.writeResult(req.ID, result)
}

// negotiateVersion picks the highest supported protocol version that is at or
// below the client's requested version. Unknown or blank requests get the
// highest supported version.
func negotiateVersion(requested string) string {
	if requested == "" {
		return mcpVersions[len(mcpVersions)-1]
	}
	chosen := mcpVersions[0]
	for _, v := range mcpVersions {
		if v <= requested {
			chosen = v
		}
	}
	return chosen
}

func (s *Server) initializeResult(raw json.RawMessage) map[string]any {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	version := negotiateVersion(params.ProtocolVersion)
	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools":       map[string]any{},
			"resources":   map[string]any{"subscribe": true},
			"prompts":     map[string]any{},
			"completions": map[string]any{},
			"logging":     map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    s.server.Name,
			"version": s.server.Version,
		},
		"instructions": "Krate compiles pages from " + "src/pages (default) into static+SSR output. " +
			"Read routes with list_routes, a page with read_page (source/ast/html; format defaults to the raw source), " +
			"read content entries with read_content, and validate with build/check. " +
			"search_docs searches Krate's own framework documentation (embedded in the compiler, not the current project) and returns slugs; read a full page with the krate://docs/{slug} resource, or pass full:true to include full text. " +
			"Write tools (create_page, edit_file, apply_patch, batch_edit, create_content, edit_content) default to a dry-run unified diff; pass apply:true to write. " +
			"create_content validates frontmatter against a collection's schema and refuses existing entries; edit_content re-parses the result and refuses schema violations. " +
			"Krate conventions: pages under <pagesDir>, root layout in _layout.tsx, directed content via getCollection from 'krate/content'. " +
			"Content collections come from content: in krate.config.ts plus plugin-contributed ones (e.g. the docs collection). " +
			"Debug the running app with dev_server_status, get_errors (build, browser and server errors from the live dev server, with KR codes), get_logs and dev_server (start/stop, needs allowTasks dev); explain a KR code with explain_diagnostic. " +
			"Understand a page with explain_page (mode, tiers, islands, exact bytes), why_hydrated, why_in_bundle, list_islands, get_page_metadata, get_rendered_html and render_preview. " +
			"Understand the project with get_project_metadata, get_config (with sources), list_api_routes, list_middleware, list_actions, get_server_action_by_id, list_collections, env_check (names only, never values), bundle_report and compat_check. " +
			"Check quality with audit_page, typecheck and, with Playwright installed, page_check and browser_eval. Move versions with upgrade and migrate_nextjs. Tools that run project code (dev_server, typecheck, page_check, browser_eval) need an allowTasks entry in .krate/mcp.json.",
	}
}

func (s *Server) listTools() map[string]any {
	tools := make([]map[string]any, 0, len(s.toolOrder))
	for _, name := range s.toolOrder {
		t := s.tools[name]
		tm := map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		}
		if len(t.Annotations) > 0 {
			tm["annotations"] = t.Annotations
		}
		if len(t.OutputSchema) > 0 {
			tm["outputSchema"] = t.OutputSchema
		}
		tools = append(tools, tm)
	}
	return map[string]any{"tools": tools}
}

func (s *Server) listResources() map[string]any {
	res := make([]map[string]any, 0, len(s.resOrder))
	for _, uri := range s.resOrder {
		r := s.resources[uri]
		res = append(res, map[string]any{
			"uri":         r.URI,
			"name":        r.Name,
			"description": r.Description,
			"mimeType":    r.MIMEType,
		})
	}
	return map[string]any{"resources": res}
}

func (s *Server) listResourceTemplates() map[string]any {
	tmpls := make([]map[string]any, 0, len(s.tmplOrder))
	for _, uri := range s.tmplOrder {
		t := s.templates[uri]
		m := map[string]any{
			"uriTemplate": t.URITemplate,
			"name":        t.Name,
			"description": t.Description,
			"mimeType":    t.MIMEType,
		}
		if t.Title != "" {
			m["title"] = t.Title
		}
		tmpls = append(tmpls, m)
	}
	if tmpls == nil {
		tmpls = []map[string]any{}
	}
	return map[string]any{"resourceTemplates": tmpls}
}

func (s *Server) listPrompts() map[string]any {
	prompts := make([]map[string]any, 0, len(s.promptOrd))
	for _, name := range s.promptOrd {
		p := s.prompts[name]
		pm := map[string]any{
			"name":        p.Name,
			"description": p.Description,
		}
		if len(p.Arguments) > 0 {
			args := make([]map[string]any, 0, len(p.Arguments))
			for _, a := range p.Arguments {
				am := map[string]any{"name": a.Name}
				if a.Description != "" {
					am["description"] = a.Description
				}
				if a.Required {
					am["required"] = true
				}
				args = append(args, am)
			}
			pm["arguments"] = args
		}
		prompts = append(prompts, pm)
	}
	return map[string]any{"prompts": prompts}
}

func (s *Server) getPrompt(raw json.RawMessage) (any, *rpcError) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	p, ok := s.prompts[params.Name]
	if !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown prompt: " + params.Name}
	}
	return p.render(params.Arguments)
}

// writeResult sends a successful response. Callers must ensure ID is valid.
func (s *Server) writeResult(id json.RawMessage, result any) {
	s.writeJSON(response{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) writeError(id json.RawMessage, code int, msg string) {
	s.writeJSON(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

// writeJSON encodes one frame, applies redaction to the serialized text, and
// writes it. Redacting the wire form covers tool content, resources, errors,
// and structured payloads with one choke point.
func (s *Server) writeJSON(value any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return
	}
	out := s.applyRedaction(buf.String())
	s.outMu.Lock()
	defer s.outMu.Unlock()
	_, _ = s.out.WriteString(out)
	_ = s.out.Flush()
}

// writeNotification sends a JSON-RPC notification. Notifications are disabled
// for stateless HTTP request/response calls, where progress must travel over a
// persistent SSE stream instead.
func (s *Server) writeNotification(method string, params any) {
	if s.suppressNotifications {
		return
	}
	s.writeJSON(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// NotifyLog sends a structured log notification.
func (s *Server) NotifyLog(level, message string) {
	s.writeNotification("notifications/message", map[string]any{
		"level": level, "logger": "krate", "data": map[string]any{"message": message},
	})
}

// NotifyResourceUpdated sends a resources/updated notification to subscribed
// clients. It is a no-op when the client did not subscribe.
func (s *Server) NotifyResourceUpdated(uri string) {
	if s.suppressNotifications {
		return
	}
	s.pmu.Lock()
	subscribed := s.subscriptions[uri]
	s.pmu.Unlock()
	if !subscribed {
		return
	}
	s.writeNotification("notifications/resources/updated", map[string]any{"uri": uri})
}

func (s *Server) setSubscription(raw json.RawMessage, on bool) (any, *rpcError) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.URI == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "resources/subscribe requires uri"}
	}
	s.pmu.Lock()
	if on {
		s.subscriptions[params.URI] = true
	} else {
		delete(s.subscriptions, params.URI)
	}
	s.pmu.Unlock()
	return map[string]any{}, nil
}

func (s *Server) writeProgress(token any, progress, total float64, message string) {
	params := map[string]any{"progressToken": token, "progress": progress, "total": total}
	if message != "" {
		params["message"] = message
	}
	s.writeNotification("notifications/progress", params)
}

// Errorf builds an internal RPC error.
func Errorf(format string, args ...any) *rpcError {
	return &rpcError{Code: codeInternalError, Message: fmt.Sprintf(format, args...)}
}

// ErrorfCode builds an RPC error with an explicit JSON-RPC code.
func ErrorfCode(code int, format string, args ...any) *rpcError {
	return &rpcError{Code: code, Message: fmt.Sprintf(format, args...)}
}
