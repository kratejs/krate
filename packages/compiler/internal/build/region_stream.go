package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type regionFrameEvent struct {
	frame regionFrame
	err   error
}

// canStreamOutOfOrder only enables completion-order streaming for sibling
// regions. A coarse page region needs its headers and status before the body,
// and nested boundaries can be replaced by their parent, so both stay on the
// ordered assembly path.
func canStreamOutOfOrder(all []*regionBoundary) bool {
	if len(all) == 0 {
		return false
	}
	for _, b := range all {
		if b.id == "page" || len(b.children) > 0 {
			return false
		}
	}
	return true
}

// splitBodyScripts keeps the document markup in the streamed prefix and defers
// the page/runtime scripts until every region chunk has been inserted. That way
// hydration sees the completed server markup even when regions finish out of
// document order.
func splitBodyScripts(shell string) (string, string) {
	lower := strings.ToLower(shell)
	body := strings.Index(lower, "<body")
	if body >= 0 {
		if rel := strings.Index(lower[body:], "<script"); rel >= 0 {
			at := body + rel
			return shell[:at], shell[at:]
		}
	}
	if at := strings.LastIndex(lower, "</body>"); at >= 0 {
		return shell[:at], shell[at:]
	}
	return shell, ""
}

func streamSiblingRegions(w http.ResponseWriter, flusher http.Flusher, shell string, regionList []map[string]string, goReq []goRenderRegion, renderGo func([]goRenderRegion) (map[string]string, error), reqCtx string, ssrPort int, route string, params, query, headers map[string]string, internalSecret string) regionPageResult {
	res := regionPageResult{served: true}
	frames := make(chan regionFrameEvent, len(regionList)+len(goReq)+1)
	var wg sync.WaitGroup
	if len(regionList) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			streamNodeRegionEvents(ssrPort, route, params, query, headers, regionList, internalSecret, frames)
		}()
	}
	if len(goReq) > 0 && renderGo != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			html, err := renderGo(goReq)
			if err != nil {
				frames <- regionFrameEvent{err: err}
				return
			}
			for id, body := range html {
				frames <- regionFrameEvent{frame: regionFrame{Type: "region", ID: id, HTML: body}}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(frames)
	}()

	prefix, tail := splitBodyScripts(shell)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = w.Write([]byte(prefix))
	flush()

	for ev := range frames {
		if ev.err != nil {
			res.renderErr = ev.err.Error()
			continue
		}
		f := ev.frame
		if f.Type != "region" || f.ID == "" {
			continue
		}
		if f.NoStore {
			res.noStore = true
		}
		if f.NotFound || f.Redirect != "" || f.Fatal || f.Status >= 500 || f.Error != "" {
			// The shell and earlier chunks are already on the wire, so keep the
			// baked fallback for this region instead of committing a second
			// status. The caller records the failure for logs/metrics.
			if f.Error != "" {
				res.renderErr = f.Error
			}
			continue
		}
		html := f.HTML
		if html == "" {
			continue
		}
		if renderGo != nil && strings.Contains(html, "krate-go:") {
			resolved, err := resolveGoMarkersE(html, reqCtx, renderGo)
			if err != nil {
				res.renderErr = err.Error()
				continue
			}
			html = resolved
		}
		_, _ = w.Write([]byte(regionReplaceScript(f.ID, html)))
		flush()
	}
	_, _ = w.Write([]byte(tail))
	flush()
	return res
}

// streamNodeRegionEvents reads the sidecar's NDJSON stream and forwards each
// frame as soon as it arrives (completion order, not document order).
func streamNodeRegionEvents(ssrPort int, route string, params, query, headers map[string]string, regionList []map[string]string, internalSecret string, out chan<- regionFrameEvent) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"route":   route,
		"url":     route,
		"method":  "GET",
		"headers": headers,
		"params":  params,
		"query":   query,
		"regions": regionList,
	})
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/__krate/regions", ssrPort), bytes.NewReader(reqBody))
	if err != nil {
		out <- regionFrameEvent{err: err}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if internalSecret != "" {
		req.Header.Set(internalSecretHeader, internalSecret)
	}
	resp, err := regionClient.Do(req)
	if err != nil {
		out <- regionFrameEvent{err: err}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out <- regionFrameEvent{err: fmt.Errorf("ssr regions: unexpected status %d", resp.StatusCode)}
		return
	}
	reader := bufio.NewReaderSize(resp.Body, 256)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var frame regionFrame
			if json.Unmarshal(line, &frame) == nil {
				out <- regionFrameEvent{frame: frame}
			}
		}
		if err != nil {
			return
		}
	}
}

// regionReplaceScript inserts one region chunk at its marker pair. It is an
// inline script because the surrounding shell has already been streamed.
func regionReplaceScript(id, html string) string {
	payload, _ := json.Marshal(html)
	var b strings.Builder
	b.WriteString(`<script data-krate-stream>`)
	b.WriteString(`(function(){var id=`)
	b.WriteString(strconv.Quote(id))
	b.WriteString(`;var html=`)
	b.WriteString(string(payload))
	b.WriteString(`;var kinds=["suspense:","region:"];var open=null,kind="";var w=document.createTreeWalker(document.documentElement,NodeFilter.SHOW_COMMENT,null,false);while(w.nextNode()){var v=w.currentNode.nodeValue||"";for(var i=0;i<kinds.length;i++){if(v===kinds[i]+id){open=w.currentNode;kind=kinds[i];break;}}if(open)break;}if(!open)return;var close=null,n=open.nextSibling;while(n){if(n.nodeType===8&&n.nodeValue==="/"+kind+id){close=n;break;}n=n.nextSibling;}if(!close)return;var r=document.createRange();r.setStartAfter(open);r.setEndBefore(close);r.deleteContents();var t=document.createElement("template");t.innerHTML=html;r.insertNode(t.content);})();`)
	b.WriteString(`</script>`)
	return b.String()
}
