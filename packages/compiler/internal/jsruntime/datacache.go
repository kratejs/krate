package jsruntime

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// DataCache is the Go-side data cache behind unstable_cache and Next-style
// fetch caching in the embedded QuickJS runtime. It is shared by every VM
// worker (so a cached value survives worker reuse and isolation mode) and is
// invalidated by tag from the Krate host's revalidation endpoint.
type DataCache struct {
	mu    sync.Mutex
	max   int
	m     map[string]dataEntry
	byTag map[string]map[string]struct{}
}

type dataEntry struct {
	value string
	exp   time.Time // zero = never expires
	tags  []string
}

// NewDataCache returns a data cache bounded to max entries (<= 0: 1000).
func NewDataCache(max int) *DataCache {
	if max <= 0 {
		max = 1000
	}
	return &DataCache{max: max, m: map[string]dataEntry{}, byTag: map[string]map[string]struct{}{}}
}

// Get returns a fresh cached value.
func (c *DataCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return "", false
	}
	if !e.exp.IsZero() && !time.Now().Before(e.exp) {
		c.deleteLocked(key)
		return "", false
	}
	return e.value, true
}

// Set stores value for ttl (ttl < 0: until invalidated; 0: not stored).
func (c *DataCache) Set(key, value string, ttl time.Duration, tags []string) {
	if ttl == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteLocked(key)
	if len(c.m) >= c.max {
		now := time.Now()
		for k, e := range c.m {
			if !e.exp.IsZero() && !now.Before(e.exp) {
				c.deleteLocked(k)
			}
		}
		for k := range c.m {
			if len(c.m) < c.max {
				break
			}
			c.deleteLocked(k)
		}
	}
	e := dataEntry{value: value, tags: append([]string(nil), tags...)}
	if ttl > 0 {
		e.exp = time.Now().Add(ttl)
	}
	c.m[key] = e
	for _, t := range e.tags {
		set := c.byTag[t]
		if set == nil {
			set = map[string]struct{}{}
			c.byTag[t] = set
		}
		set[key] = struct{}{}
	}
}

// InvalidateTags removes every entry carrying one of tags and returns how many.
func (c *DataCache) InvalidateTags(tags []string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range tags {
		for k := range c.byTag[t] {
			if _, ok := c.m[k]; ok {
				n++
			}
			c.deleteLocked(k)
		}
	}
	return n
}

// Len returns the number of stored entries.
func (c *DataCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

func (c *DataCache) deleteLocked(key string) {
	e, ok := c.m[key]
	if !ok {
		return
	}
	delete(c.m, key)
	for _, t := range e.tags {
		if set := c.byTag[t]; set != nil {
			delete(set, key)
			if len(set) == 0 {
				delete(c.byTag, t)
			}
		}
	}
}

// CacheHooks connects embedded API routes to the host's on-demand
// revalidation and data cache (see @krate/runtime/cache).
type CacheHooks struct {
	// Revalidate applies a {paths, tags} revalidation synchronously and
	// returns the host's JSON result (or {"error": "..."}).
	Revalidate func(payloadJSON string) string
	// Data backs unstable_cache and fetch caching; nil disables both.
	Data *DataCache
}

// registerCacheHooks installs _go_revalidate, _go_cache_get and _go_cache_set.
func (r *Runtime) registerCacheHooks(h *CacheHooks) error {
	if h == nil {
		return nil
	}
	r.dataCache = h.Data
	if h.Revalidate != nil {
		if err := r.RegisterFunc("_go_revalidate", func(args []any) (any, error) {
			payload, _ := argString(args, 0)
			return h.Revalidate(payload), nil
		}); err != nil {
			return err
		}
	}
	if h.Data == nil {
		return nil
	}
	if err := r.RegisterFunc("_go_cache_get", func(args []any) (any, error) {
		key, _ := argString(args, 0)
		v, ok := h.Data.Get(key)
		if !ok {
			return "", nil
		}
		return v, nil
	}); err != nil {
		return err
	}
	return r.RegisterFunc("_go_cache_set", func(args []any) (any, error) {
		key, _ := argString(args, 0)
		value, _ := argString(args, 1)
		ttl := time.Duration(0)
		if len(args) > 2 {
			if secs, ok := toFloat(args[2]); ok {
				if secs < 0 {
					ttl = -1
				} else {
					ttl = time.Duration(secs * float64(time.Second))
				}
			}
		}
		var tags []string
		if s, ok := argString(args, 3); ok && s != "" {
			_ = json.Unmarshal([]byte(s), &tags)
		}
		h.Data.Set(key, value, ttl, tags)
		return nil, nil
	})
}

func argString(args []any, i int) (string, bool) {
	if i >= len(args) {
		return "", false
	}
	s, ok := args[i].(string)
	return s, ok
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint32:
		return float64(n), true
	}
	return 0, false
}

// fetchCachePolicy reads Next-style fetch options: it returns the cache TTL
// (< 0 forever, 0 bypass) and tags. Only GET/HEAD requests are cached.
func fetchCachePolicy(method string, opts map[string]any) (time.Duration, []string) {
	if opts == nil {
		return 0, nil
	}
	var tags []string
	ttl := time.Duration(0)
	mode, _ := opts["cache"].(string)
	if mode == "force-cache" {
		ttl = -1
	}
	if next, ok := opts["next"].(map[string]any); ok {
		if raw, ok := next["tags"].([]any); ok {
			for _, t := range raw {
				if s, ok := t.(string); ok && s != "" {
					tags = append(tags, s)
				}
			}
		}
		switch v := next["revalidate"].(type) {
		case bool:
			if !v {
				ttl = -1
			}
		default:
			if secs, ok := toFloat(v); ok {
				if secs <= 0 {
					return 0, tags
				}
				ttl = time.Duration(secs * float64(time.Second))
			}
		}
	}
	if mode == "no-store" {
		return 0, tags
	}
	m := strings.ToUpper(method)
	if m != "GET" && m != "HEAD" {
		return 0, tags
	}
	return ttl, tags
}

// fetchCacheKey identifies a cached fetch by method, URL and sorted headers.
func fetchCacheKey(method, url string, headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("fetch:")
	b.WriteString(strings.ToUpper(method))
	b.WriteString(" ")
	b.WriteString(url)
	lower := make(map[string]string, len(headers))
	for k, v := range headers {
		lower[strings.ToLower(k)] = v
	}
	for _, k := range keys {
		b.WriteString("\x00")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(lower[k])
	}
	return b.String()
}

// fetchResult is a fetch response as handed to JS (and stored in the data
// cache).
type fetchResult struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	StatusText string            `json:"statusText"`
}
