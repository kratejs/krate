package krate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// RevalidateResult is the Krate host's answer to a revalidation: what was
// invalidated and what was skipped (static pages, unknown routes) with why.
type RevalidateResult struct {
	Revalidated []struct {
		Target string   `json:"target"`
		Kind   string   `json:"kind"`
		Routes []string `json:"routes,omitempty"`
	} `json:"revalidated"`
	Skipped []struct {
		Target string `json:"target"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

// RevalidatePath invalidates the cached render of path on the Krate server,
// like revalidatePath from @krate/runtime/cache. typ is "page" (default) or
// "layout" (every page under path). It returns once every cache has been
// invalidated, so the response a Go API route sends after it is
// read-your-writes. Skipped targets are logged to stderr.
func RevalidatePath(path string, typ ...string) error {
	t := "page"
	if len(typ) > 0 && typ[0] != "" {
		t = typ[0]
	}
	if t != "page" && t != "layout" {
		return fmt.Errorf("krate.RevalidatePath: type must be \"page\" or \"layout\", got %q", t)
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("krate.RevalidatePath: path must start with \"/\", got %q", path)
	}
	_, err := Revalidate([]string{path + "\x00" + t}, nil)
	return err
}

// RevalidateTag invalidates every cached page render, region and data entry
// that read tag, like revalidateTag from @krate/runtime/cache.
func RevalidateTag(tag string) error {
	if tag == "" {
		return errors.New("krate.RevalidateTag: tag must not be empty")
	}
	_, err := Revalidate(nil, []string{tag})
	return err
}

// Revalidate sends paths (each "path" or "path\x00type") and tags to the
// Krate host and returns its structured result. It fails when the process
// does not run under `krate serve` / `krate dev` (KRATE_HOST_URL unset).
func Revalidate(paths, tags []string) (RevalidateResult, error) {
	var out RevalidateResult
	host := os.Getenv("KRATE_HOST_URL")
	secret := os.Getenv("KRATE_INTERNAL_SECRET")
	if host == "" || secret == "" {
		return out, errors.New("krate: revalidation needs the Krate server (KRATE_HOST_URL is not set); call it from a Go API route served by krate serve/dev")
	}
	type pathReq struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	body := struct {
		Paths []pathReq `json:"paths"`
		Tags  []string  `json:"tags"`
	}{Paths: []pathReq{}, Tags: append([]string{}, tags...)}
	for _, p := range paths {
		path, typ, _ := strings.Cut(p, "\x00")
		if typ == "" {
			typ = "page"
		}
		body.Paths = append(body.Paths, pathReq{Path: path, Type: typ})
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(host, "/")+"/__krate/internal/revalidate", bytes.NewReader(data))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Krate-Internal", secret)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return out, fmt.Errorf("krate: revalidation failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("krate: revalidation failed: host answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("krate: revalidation reply: %w", err)
	}
	for _, s := range out.Skipped {
		fmt.Fprintf(os.Stderr, "[krate] revalidation skipped for %s: %s\n", s.Target, s.Reason)
	}
	return out, nil
}
