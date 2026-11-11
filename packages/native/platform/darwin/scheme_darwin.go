//go:build darwin

package darwin

import (
	"context"
	"errors"
	"net/http"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// taskState tracks one WKURLSchemeTask. Every field except writer is touched
// only on the main thread. done becomes true when the task finished, failed or
// was stopped; after that no message may be sent to the task.
type taskState struct {
	task   objc.ID
	done   bool
	writer *taskWriter
}

// startTask handles webView:startURLSchemeTask: on the main thread. It copies
// everything it needs out of the NSURLRequest, then serves the request on a
// goroutine. The task is retained until it completes or is stopped.
func (a *App) startTask(webView, task objc.ID) {
	retain(task)
	st := &taskState{task: task}
	a.tasks[task] = st

	req := task.Send(sel("request"))
	rawURL := goString(req.Send(sel("URL")).Send(sel("absoluteString")))
	method := goString(req.Send(sel("HTTPMethod")))
	fields := goStringMap(req.Send(sel("allHTTPHeaderFields")))
	body := requestBody(req)

	// The webview that made the request, bound natively: page script cannot
	// change it. A web view the app does not know tags the request with view 0.
	var view caps.ViewRef
	if v := a.byWeb[webView]; v != nil {
		view = v.ref
	}
	ctx, cancel := context.WithCancel(caps.WithView(context.Background(), view))
	w := newTaskWriter(&nsTaskSink{app: a, st: st}, cancel)
	st.writer = w
	handler := a.opts.Handler

	go func() {
		r, err := newHTTPRequest(ctx, method, rawURL, fields, body)
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("bad request\n"))
			w.finish()
			w.stop()
			return
		}
		serve(handler, r, w)
	}()
}

// stopTask handles webView:stopURLSchemeTask:. After this returns the task
// must never be messaged again, so queued sink calls check st.done.
func (a *App) stopTask(task objc.ID) {
	st := a.tasks[task]
	if st == nil {
		return
	}
	st.done = true
	delete(a.tasks, task)
	st.writer.stop()
	release(task)
}

// requestBody returns the request body from HTTPBody or, when WebKit supplies
// a stream (common for fetch() with a body), by draining HTTPBodyStream. The
// stream is read right here on the main thread; its data is already local.
func requestBody(req objc.ID) []byte {
	if d := req.Send(sel("HTTPBody")); d != 0 {
		return dataBytes(d)
	}
	stream := req.Send(sel("HTTPBodyStream"))
	if stream == 0 {
		return nil
	}
	stream.Send(sel("open"))
	defer stream.Send(sel("close"))
	var out []byte
	buf := make([]byte, 64<<10)
	read := sel("read:maxLength:")
	for {
		n := objc.Send[int](stream, read, unsafe.Pointer(&buf[0]), uint(len(buf)))
		if n <= 0 {
			break
		}
		out = append(out, buf[:n]...)
	}
	return out
}

// nsTaskSink forwards writer events to the task on the main thread.
type nsTaskSink struct {
	app *App
	st  *taskState
}

var errTaskFailed = errors.New("krate: response aborted")

func (s *nsTaskSink) receiveResponse(status int, h http.Header) {
	fields := responseFields(h)
	dispatch(func() {
		if s.st.done {
			return
		}
		task := s.st.task
		url := task.Send(sel("request")).Send(sel("URL"))
		resp := cls("NSHTTPURLResponse").Send(sel("alloc")).Send(
			sel("initWithURL:statusCode:HTTPVersion:headerFields:"),
			url, status, nsString("HTTP/1.1"), nsDictionary(fields))
		if resp == 0 {
			return
		}
		task.Send(sel("didReceiveResponse:"), resp)
		release(resp)
	})
}

func (s *nsTaskSink) receiveData(p []byte) {
	if len(p) == 0 {
		return
	}
	dispatch(func() {
		if s.st.done {
			return
		}
		data := cls("NSData").Send(sel("dataWithBytes:length:"), unsafe.Pointer(&p[0]), uint(len(p)))
		s.st.task.Send(sel("didReceiveData:"), data)
	})
}

func (s *nsTaskSink) finish() {
	dispatch(func() {
		if s.st.done {
			return
		}
		s.complete()
		s.st.task.Send(sel("didFinish"))
		s.release()
	})
}

func (s *nsTaskSink) fail(err error) {
	dispatch(func() {
		if s.st.done {
			return
		}
		if err == nil {
			err = errTaskFailed
		}
		// NSURLErrorDomain, NSURLErrorNetworkConnectionLost.
		e := cls("NSError").Send(sel("errorWithDomain:code:userInfo:"),
			nsString("NSURLErrorDomain"), -1005, objc.ID(0))
		s.complete()
		s.st.task.Send(sel("didFailWithError:"), e)
		s.release()
	})
}

// complete marks the task done and drops it from the registry. The task is
// released by release, after the final message has been sent.
func (s *nsTaskSink) complete() {
	s.st.done = true
	delete(s.app.tasks, s.st.task)
}

func (s *nsTaskSink) release() { release(s.st.task) }
