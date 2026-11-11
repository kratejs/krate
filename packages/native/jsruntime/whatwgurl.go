package jsruntime

import (
	"encoding/json"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// urlRecord is the component view of a parsed URL handed to the JS URL class.
type urlRecord struct {
	Href     string `json:"href"`
	Origin   string `json:"origin"`
	Protocol string `json:"protocol"`
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Hostname string `json:"hostname"`
	Port     string `json:"port"`
	Pathname string `json:"pathname"`
	Search   string `json:"search"`
	Hash     string `json:"hash"`
}

var specialDefaultPorts = map[string]string{
	"http": "80", "https": "443", "ws": "80", "wss": "443", "ftp": "21", "file": "",
}

func isSpecialScheme(s string) bool {
	_, ok := specialDefaultPorts[s]
	return ok
}

// parseWHATWGURL parses input against an optional base following the WHATWG
// URL rules closely enough for server code: it normalizes case, default
// ports, dot segments and percent-encoding, and rejects what a browser
// rejects (no scheme without a base, bad ports, empty hosts for special
// schemes). It returns ok=false for an invalid URL.
func parseWHATWGURL(input, base string) (urlRecord, bool) {
	var baseRec *urlRecord
	if base != "" {
		b, ok := parseWHATWGURL(base, "")
		if !ok {
			return urlRecord{}, false
		}
		baseRec = &b
	}
	input = strings.TrimFunc(input, func(r rune) bool { return r <= 0x20 })
	input = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(input)

	scheme, rest, hasScheme := splitScheme(input)
	if hasScheme {
		scheme = strings.ToLower(scheme)
		if isSpecialScheme(scheme) {
			if baseRec != nil && baseRec.Protocol == scheme+":" && !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "\\") {
				return resolveRelative(rest, *baseRec)
			}
			return parseAuthorityURL(scheme, rest)
		}
		if strings.HasPrefix(rest, "//") {
			return parseAuthorityURL(scheme, rest)
		}
		return parseOpaqueURL(scheme, rest)
	}
	if baseRec == nil {
		return urlRecord{}, false
	}
	if baseRec.Pathname != "" && !strings.HasPrefix(baseRec.Pathname, "/") && baseRec.Host == "" {
		// Opaque base (mailto:, data:): only a fragment can be resolved.
		if strings.HasPrefix(input, "#") {
			r := *baseRec
			r.Hash = encodeFragment(input)
			r.Href = r.Protocol + r.Pathname + r.Search + r.Hash
			return r, true
		}
		return urlRecord{}, false
	}
	return resolveRelative(input, *baseRec)
}

func splitScheme(s string) (scheme, rest string, ok bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		case c == ':' && i > 0:
			return s[:i], s[i+1:], true
		default:
			return "", s, false
		}
	}
	return "", s, false
}

func parseOpaqueURL(scheme, rest string) (urlRecord, bool) {
	frag := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		frag = encodeFragment(rest[i:])
		rest = rest[:i]
	}
	query := ""
	if i := strings.Index(rest, "?"); i >= 0 {
		query = encodeQuery(rest[i:], false)
		rest = rest[:i]
	}
	path := encodeSet(rest, c0Control)
	r := urlRecord{Protocol: scheme + ":", Origin: "null", Pathname: path, Search: query, Hash: frag}
	r.Href = r.Protocol + path + query + frag
	return r, true
}

// parseAuthorityURL parses scheme + "//authority/path?query#frag".
func parseAuthorityURL(scheme, rest string) (urlRecord, bool) {
	special := isSpecialScheme(scheme)
	if scheme == "file" {
		rest = strings.ReplaceAll(rest, "\\", "/")
		rest = strings.TrimPrefix(rest, "//")
		if rest != "" && !strings.HasPrefix(rest, "/") && !strings.Contains(rest, "/") {
			rest = rest + "/"
		}
	} else if special {
		rest = strings.ReplaceAll(rest, "\\", "/")
		rest = strings.TrimLeft(rest, "/")
	} else {
		rest = strings.TrimPrefix(rest, "//")
	}
	frag := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		frag = encodeFragment(rest[i:])
		rest = rest[:i]
	}
	query := ""
	if i := strings.Index(rest, "?"); i >= 0 {
		query = encodeQuery(rest[i:], special)
		rest = rest[:i]
	}
	authority := rest
	path := ""
	if i := strings.Index(rest, "/"); i >= 0 {
		authority = rest[:i]
		path = rest[i:]
	}
	user, pass := "", ""
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		cred := authority[:at]
		authority = authority[at+1:]
		if c := strings.Index(cred, ":"); c >= 0 {
			user, pass = cred[:c], cred[c+1:]
		} else {
			user = cred
		}
		user = encodeSet(user, userinfoSet)
		pass = encodeSet(pass, userinfoSet)
	}
	host, port, ok := splitHostPort(authority)
	if !ok {
		return urlRecord{}, false
	}
	if scheme == "file" {
		port = ""
	}
	host, ok = normalizeHost(host, special)
	if !ok {
		return urlRecord{}, false
	}
	if special && scheme != "file" && host == "" {
		return urlRecord{}, false
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return urlRecord{}, false
		}
		port = strconv.Itoa(n)
		if port == specialDefaultPorts[scheme] {
			port = ""
		}
	}
	if special {
		path = normalizePath(path, true)
	} else if path != "" {
		path = normalizePath(path, false)
	}
	return buildRecord(scheme, user, pass, host, port, path, query, frag), true
}

func buildRecord(scheme, user, pass, host, port, path, query, frag string) urlRecord {
	r := urlRecord{Protocol: scheme + ":", Username: user, Password: pass, Hostname: host, Port: port, Pathname: path, Search: query, Hash: frag}
	r.Host = host
	if port != "" {
		r.Host = host + ":" + port
	}
	var b strings.Builder
	b.WriteString(r.Protocol)
	b.WriteString("//")
	if user != "" || pass != "" {
		b.WriteString(user)
		if pass != "" {
			b.WriteString(":" + pass)
		}
		b.WriteString("@")
	}
	b.WriteString(r.Host)
	b.WriteString(path)
	b.WriteString(query)
	b.WriteString(frag)
	r.Href = b.String()
	if isSpecialScheme(scheme) && scheme != "file" {
		r.Origin = r.Protocol + "//" + r.Host
	} else {
		r.Origin = "null"
	}
	return r
}

func splitHostPort(authority string) (host, port string, ok bool) {
	if strings.HasPrefix(authority, "[") {
		end := strings.Index(authority, "]")
		if end < 0 {
			return "", "", false
		}
		host = authority[:end+1]
		tail := authority[end+1:]
		if tail == "" {
			return host, "", true
		}
		if !strings.HasPrefix(tail, ":") {
			return "", "", false
		}
		return host, tail[1:], validPort(tail[1:])
	}
	if i := strings.LastIndex(authority, ":"); i >= 0 {
		return authority[:i], authority[i+1:], validPort(authority[i+1:])
	}
	return authority, "", true
}

func validPort(p string) bool {
	for i := 0; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return false
		}
	}
	return true
}

func normalizeHost(host string, special bool) (string, bool) {
	if strings.HasPrefix(host, "[") {
		return strings.ToLower(host), true
	}
	if host == "" {
		return "", true
	}
	if !special {
		return encodeSet(host, c0Control), true
	}
	if d, err := percentDecode(host); err == nil {
		host = d
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", false
	}
	host = strings.ToLower(ascii)
	if strings.ContainsAny(host, " #%/:<>?@[\\]^|\x00") {
		return "", false
	}
	return host, true
}

func percentDecode(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(v))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// normalizePath resolves dot segments and percent-encodes the path.
func normalizePath(p string, special bool) string {
	if special {
		p = strings.ReplaceAll(p, "\\", "/")
	}
	if p == "" {
		if special {
			return "/"
		}
		return ""
	}
	segs := strings.Split(p, "/")
	var out []string
	for i, seg := range segs {
		if i == 0 && seg == "" {
			continue
		}
		lower := strings.ToLower(seg)
		last := i == len(segs)-1
		switch lower {
		case ".", "%2e":
			if last {
				out = append(out, "")
			}
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, encodeSet(seg, pathSet))
		}
	}
	return "/" + strings.Join(out, "/")
}

func resolveRelative(ref string, base urlRecord) (urlRecord, bool) {
	special := isSpecialScheme(strings.TrimSuffix(base.Protocol, ":"))
	if special {
		ref = strings.ReplaceAll(ref, "\\", "/")
	}
	scheme := strings.TrimSuffix(base.Protocol, ":")
	if strings.HasPrefix(ref, "//") {
		return parseAuthorityURL(scheme, ref)
	}
	frag := ""
	if i := strings.Index(ref, "#"); i >= 0 {
		frag = encodeFragment(ref[i:])
		ref = ref[:i]
	}
	var query string
	hasQuery := false
	if i := strings.Index(ref, "?"); i >= 0 {
		query = encodeQuery(ref[i:], special)
		hasQuery = true
		ref = ref[:i]
	}
	path := base.Pathname
	switch {
	case ref == "":
		if !hasQuery {
			query = base.Search
		}
	case strings.HasPrefix(ref, "/"):
		path = normalizePath(ref, special)
	default:
		dir := base.Pathname
		if i := strings.LastIndex(dir, "/"); i >= 0 {
			dir = dir[:i+1]
		} else {
			dir = "/"
		}
		path = normalizePath(dir+ref, special)
	}
	r := buildRecord(scheme, base.Username, base.Password, base.Hostname, base.Port, path, query, frag)
	return r, true
}

type charSet func(b byte) bool

func c0Control(b byte) bool { return b <= 0x1f || b > 0x7e }
func fragmentSet(b byte) bool {
	return c0Control(b) || b == ' ' || b == '"' || b == '<' || b == '>' || b == '`'
}
func querySet(b byte) bool {
	return c0Control(b) || b == ' ' || b == '"' || b == '#' || b == '<' || b == '>'
}
func pathSet(b byte) bool {
	return querySet(b) || b == '?' || b == '`' || b == '{' || b == '}' || b == '^'
}
func userinfoSet(b byte) bool {
	return pathSet(b) || b == '/' || b == ':' || b == ';' || b == '=' || b == '@' || (b >= '[' && b <= ']') || b == '|'
}

func encodeSet(s string, set charSet) string {
	var b strings.Builder
	const hexd = "0123456789ABCDEF"
	for i := 0; i < len(s); i++ {
		c := s[i]
		if set(c) {
			b.WriteByte('%')
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&15])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func encodeFragment(s string) string {
	if s == "#" {
		return ""
	}
	return "#" + encodeSet(strings.TrimPrefix(s, "#"), fragmentSet)
}

func encodeQuery(s string, special bool) string {
	if s == "?" {
		return ""
	}
	set := querySet
	if special {
		set = func(b byte) bool { return querySet(b) || b == '\'' }
	}
	return "?" + encodeSet(strings.TrimPrefix(s, "?"), set)
}

func urlParseHost(args []any) (any, error) {
	input, base := "", ""
	if len(args) > 0 {
		input, _ = args[0].(string)
	}
	if len(args) > 1 {
		base, _ = args[1].(string)
	}
	rec, ok := parseWHATWGURL(input, base)
	if !ok {
		return "", nil
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
