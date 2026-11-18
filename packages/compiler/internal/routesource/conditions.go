package routesource

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Condition is one has or missing item of a rule, with Next.js semantics.
//
// Type is "query", "header", "cookie" or "host". Key names the query
// parameter, header or cookie (not used for host). Without Value the item
// matches when the field is present and non-empty, and the field's value is
// captured under the key. With Value, the field must match ^(?:Value)$, and
// the named groups of Value are captured.
type Condition struct {
	Type  string `json:"type"`
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

type cond struct {
	Condition
	re *regexp.Regexp
}

// Conditions is a compiled has/missing pair.
type Conditions struct {
	has, missing []cond
}

// CompileConditions validates and compiles has and missing. It returns nil
// when both are empty.
func CompileConditions(has, missing []Condition) (*Conditions, error) {
	if len(has) == 0 && len(missing) == 0 {
		return nil, nil
	}
	c := &Conditions{}
	for _, list := range []struct {
		name  string
		items []Condition
		dst   *[]cond
	}{{"has", has, &c.has}, {"missing", missing, &c.missing}} {
		for i, it := range list.items {
			cc, err := compileCondition(it)
			if err != nil {
				if IsUnsupported(err) {
					return nil, unsupported("%s[%d]: %v", list.name, i, err)
				}
				return nil, fmt.Errorf("%s[%d]: %w", list.name, i, err)
			}
			*list.dst = append(*list.dst, cc)
		}
	}
	return c, nil
}

func compileCondition(it Condition) (cond, error) {
	c := cond{Condition: it}
	switch it.Type {
	case "query", "header", "cookie":
		if it.Key == "" {
			return c, fmt.Errorf("type %q needs a key", it.Type)
		}
	case "host":
	default:
		return c, fmt.Errorf("type must be \"query\", \"header\", \"cookie\" or \"host\", got %q", it.Type)
	}
	if it.Value != "" {
		re, err := regexp.Compile("^(?:" + it.Value + ")$")
		if err != nil {
			return c, unsupportedRegex(it.Value, err)
		}
		c.re = re
	}
	return c, nil
}

// safeParamName keeps only ASCII letters, as Next does for the capture a
// presence-only condition makes from its key.
func safeParamName(key string) string {
	var sb strings.Builder
	for i := 0; i < len(key); i++ {
		if c := key[i]; c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// value returns the request field a condition reads, and whether it is set.
func (c cond) value(r *http.Request, q url.Values) (string, bool) {
	var v string
	switch c.Type {
	case "query":
		vals := q[c.Key]
		if len(vals) > 0 {
			v = vals[len(vals)-1]
		}
	case "header":
		if strings.EqualFold(c.Key, "host") {
			v = r.Host
		} else if vals := r.Header.Values(c.Key); len(vals) > 0 {
			sep := ", "
			if strings.EqualFold(c.Key, "cookie") {
				sep = "; "
			}
			v = strings.Join(vals, sep)
		}
	case "cookie":
		if ck, err := r.Cookie(c.Key); err == nil {
			v = ck.Value
			if dec, err := url.PathUnescape(v); err == nil {
				v = dec
			}
		}
	case "host":
		v = strings.ToLower(strings.SplitN(r.Host, ":", 2)[0])
	}
	return v, v != ""
}

func (c cond) match(r *http.Request, q url.Values, params map[string]string) bool {
	v, ok := c.value(r, q)
	if !ok {
		return false
	}
	if c.re == nil {
		key := c.Key
		if c.Type == "header" {
			key = strings.ToLower(key)
		}
		params[safeParamName(key)] = v
		return true
	}
	m := c.re.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	named := false
	for i, n := range c.re.SubexpNames() {
		if n != "" {
			named = true
			params[n] = m[i]
		}
	}
	if !named && c.Type == "host" && m[0] != "" {
		params["host"] = m[0]
	}
	return true
}

// Match evaluates the conditions against a request. Every has item must
// match and no missing item may. It returns the captures of the has items.
func (c *Conditions) Match(r *http.Request) (map[string]string, bool) {
	if c == nil {
		return nil, true
	}
	q := r.URL.Query()
	params := map[string]string{}
	for _, h := range c.has {
		if !h.match(r, q, params) {
			return nil, false
		}
	}
	for _, m := range c.missing {
		if m.match(r, q, map[string]string{}) {
			return nil, false
		}
	}
	return params, true
}
