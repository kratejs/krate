package migrate

import (
	"bytes"
	"encoding/json"
	"sort"
)

// marshalKeepingOrder writes v (decoded from orig and then edited) as indented
// JSON with the keys of every object in the order orig had them; keys the
// migration added come after, sorted. Characters such as & and < are written
// as they are, not as &, so "a && b" scripts stay readable.
func marshalKeepingOrder(orig []byte, v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ordered(orig, v)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type orderedObject struct {
	keys []string
	vals map[string]any
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := enc.Encode(k); err != nil {
			return nil, err
		}
		buf.Truncate(buf.Len() - 1) // Encode's newline
		buf.WriteByte(':')
		if err := enc.Encode(o.vals[k]); err != nil {
			return nil, err
		}
		buf.Truncate(buf.Len() - 1)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func ordered(orig []byte, v any) any {
	switch t := v.(type) {
	case map[string]any:
		var children map[string]json.RawMessage
		_ = json.Unmarshal(orig, &children)
		seen := map[string]bool{}
		var keys []string
		for _, k := range objectKeyOrder(orig) {
			if _, ok := t[k]; ok && !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
		var added []string
		for k := range t {
			if !seen[k] {
				added = append(added, k)
			}
		}
		sort.Strings(added)
		keys = append(keys, added...)
		vals := make(map[string]any, len(t))
		for _, k := range keys {
			vals[k] = ordered(children[k], t[k])
		}
		return orderedObject{keys: keys, vals: vals}
	case []any:
		var items []json.RawMessage
		_ = json.Unmarshal(orig, &items)
		out := make([]any, len(t))
		for i, item := range t {
			var raw []byte
			if i < len(items) {
				raw = items[i]
			}
			out[i] = ordered(raw, item)
		}
		return out
	}
	return v
}

// objectKeyOrder lists the top-level keys of a JSON object in source order.
func objectKeyOrder(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		k, _ := tok.(string)
		keys = append(keys, k)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}
