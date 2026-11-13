package syntaxhighlight

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// A Table is one chroma lexer turned into data a small JavaScript tokenizer
// can run: the rule list of every state with each pattern already translated
// to a JavaScript RegExp source. Tables are produced from the lexers inside
// the binary, so they always match its chroma version.

// Emitter codes in the table format. A plain number is a token type (an index
// into Types); -1 emits nothing. Arrays start with one of these codes.
const (
	emByGroups   = 0 // [0, e1, e2, ...]
	emUsingSelf  = 1 // [1, state]
	emUsing      = 2 // [2, lexerName]
	emUsingGroup = 3 // [3, nameGroup, codeGroup, e1, e2, ...]
	emUsingMap   = 4 // [4, lexerName, [fromType, toType]...]
)

// Mutator steps: a state index pushes that state, -1 pops one, -2 pushes the
// current state again.
const (
	mutPop  = -1
	mutSelf = -2
)

// Rule is one lexer rule. An empty Pattern matches the empty string.
type Rule struct {
	Pattern string
	Emit    interface{}
	Mut     []int
}

// Table is the exported form of a lexer.
type Table struct {
	Name     string
	Types    []string
	Rules    [][]*Rule // by state index; state 0 is root
	States   []string  // state names, for diagnostics
	EnsureNL bool
	// Frontmatter marks the markdown lexer, which lexes a leading YAML block
	// with the YAML lexer first.
	Frontmatter bool
	// Deps are the names of other tables this lexer calls into.
	Deps []string
	// Fences marks lexers that pick a sublexer by name at run time
	// (UsingByGroup); the tokenizer needs a name resolver for them.
	Fences bool
}

// xmlNode is a parsed XML element.
type xmlNode struct {
	Name     string
	Attrs    []xml.Attr
	Children []*xmlNode
	Text     string
}

func (n *xmlNode) attr(name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

func (n *xmlNode) attrs(name string) []string {
	var out []string
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			out = append(out, a.Value)
		}
	}
	return out
}

func (n *xmlNode) child(name string) *xmlNode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func parseXML(data []byte) (*xmlNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []*xmlNode
	var root *xmlNode
	for {
		tok, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{Name: t.Name.Local, Attrs: append([]xml.Attr(nil), t.Attr...)}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	return root, nil
}

type rawRule struct {
	pattern string
	emitter *xmlNode // nil, or an emitter element
	mutator *xmlNode // nil, or a mutator element
	special func(*exporter) (interface{}, error)
}

type exporter struct {
	name      string
	flags     string
	types     []string
	typeIndex map[string]int
	deps      map[string]bool
	fences    bool
	states    map[string][]rawRule
	stateIdx  map[string]int
	stateOrd  []string
	flat      map[string][]rawRule
	visiting  map[string]bool
}

func (x *exporter) typeFor(name string) (int, error) {
	tt, err := chroma.TokenTypeString(name)
	if err != nil {
		return 0, fmt.Errorf("unknown token type %q", name)
	}
	if tt == chroma.Ignore {
		return -1, nil
	}
	class := classOf(tt)
	if i, ok := x.typeIndex[class]; ok {
		return i, nil
	}
	x.types = append(x.types, class)
	x.typeIndex[class] = len(x.types) - 1
	return len(x.types) - 1, nil
}

// classOf is the CSS class chroma's HTML formatter writes for a token type: the
// nearest standard ancestor's class.
func classOf(t chroma.TokenType) string {
	for t != 0 {
		if cls, ok := chroma.StandardTypes[t]; ok {
			return cls
		}
		t = t.Parent()
	}
	return chroma.StandardTypes[t]
}

// errNeedsSpecial reports a lexer whose rules cannot be serialised.
var errNeedsSpecial = errors.New("lexer has rules that cannot be serialised")

// ExportLexer turns the named chroma lexer into a Table.
func ExportLexer(name string) (*Table, error) {
	lex := lexers.Get(name)
	if lex == nil {
		return nil, fmt.Errorf("no chroma lexer %q", name)
	}
	frontmatter := false
	rl, ok := lex.(*chroma.RegexLexer)
	if !ok {
		// The markdown lexer wraps a RegexLexer to lex YAML frontmatter first.
		v := reflect.ValueOf(lex)
		if v.Kind() == reflect.Ptr && v.Elem().Kind() == reflect.Struct {
			if f := v.Elem().FieldByName("Lexer"); f.IsValid() && f.CanInterface() {
				if inner, ok := f.Interface().(*chroma.RegexLexer); ok && strings.EqualFold(lex.Config().Name, "markdown") {
					rl, frontmatter = inner, true
				}
			}
		}
		if rl == nil {
			return nil, fmt.Errorf("lexer %q is %T, not a regexp lexer: %w", name, lex, errNeedsSpecial)
		}
	}
	cfg := rl.Config()
	x := &exporter{
		name:      cfg.Name,
		typeIndex: map[string]int{},
		deps:      map[string]bool{},
		states:    map[string][]rawRule{},
		stateIdx:  map[string]int{},
		flat:      map[string][]rawRule{},
		visiting:  map[string]bool{},
	}
	if !cfg.NotMultiline {
		x.flags += "m"
	}
	if cfg.CaseInsensitive {
		x.flags += "i"
	}
	if cfg.DotAll {
		x.flags += "s"
	}
	if err := x.loadRules(rl); err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.Name, err)
	}
	if _, ok := x.states["root"]; !ok {
		return nil, fmt.Errorf("%s: no root state", cfg.Name)
	}
	t, err := x.build(cfg, frontmatter)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.Name, err)
	}
	return t, nil
}

// specialRules are rules whose emitters are Go functions in chroma. Each is
// keyed by lexer name and pattern and is described here in table form.
var specialRules = map[string]map[string]func(*exporter) (interface{}, error){
	"Go": {
		// Raw strings: the contents are lexed as a Go text template, and tokens
		// of type Other in it become String.
		"(`)([^`]*)(`)": func(x *exporter) (interface{}, error) {
			str, err := x.typeFor("LiteralString")
			if err != nil {
				return nil, err
			}
			other, err := x.typeFor("Other")
			if err != nil {
				return nil, err
			}
			x.deps["Go Text Template"] = true
			return []interface{}{emByGroups, str, []interface{}{emUsingMap, "Go Text Template", []int{other, str}}, str}, nil
		},
	},
}

// loadRules reads the lexer's rules. A fully serialisable lexer goes through
// chroma's own XML encoder in one pass; one that is not (Go function emitters)
// is read rule by rule, with the known special cases supplied by hand.
func (x *exporter) loadRules(rl *chroma.RegexLexer) error {
	data, err := chroma.Marshal(rl)
	if err == nil {
		doc, err := parseXML(data)
		if err != nil {
			return err
		}
		rules := doc.child("rules")
		if rules == nil {
			return fmt.Errorf("no rules in serialised lexer")
		}
		for _, st := range rules.Children {
			name, _ := st.attr("name")
			x.states[name] = nil
			for _, rn := range st.Children {
				raw := rawRule{}
				raw.pattern, _ = rn.attr("pattern")
				for _, c := range rn.Children {
					switch c.Name {
					case "include", "combined", "push", "pop", "mutators":
						raw.mutator = c
					default:
						raw.emitter = c
					}
				}
				x.states[name] = append(x.states[name], raw)
			}
		}
		return nil
	}
	if !errors.Is(err, chroma.ErrNotSerialisable) {
		return err
	}
	rules, err := rl.Rules()
	if err != nil {
		return err
	}
	for state, rs := range rules {
		x.states[state] = nil
		for _, r := range rs {
			raw, err := x.rawRuleOf(rl.Config().Name, rules, r)
			if err != nil {
				return fmt.Errorf("state %s: %w", state, err)
			}
			x.states[state] = append(x.states[state], raw)
		}
	}
	return nil
}

// patternOverrides replace rules whose regular expression backtracks badly on
// some input with a form that matches exactly the same text. A JavaScript
// engine has no match timeout, so the original would stall or hang the page,
// where chroma's regexp2 gives up after 250 ms. TestParityCorpus compares the
// result with chroma.
var patternOverrides = map[string]map[string]string{
	"Rust": {
		// SCREAMING_CASE constants. The group is repeated only to demand a second
		// capital; spelling that out takes linear time instead of 2^n.
		`\b(r#)?_?([A-Z][A-Z0-9_]*){2,}\b`: `\b(r#)?_?([A-Z][A-Z0-9_]*[A-Z][A-Z0-9_]*)\b`,
	},
	"YAML": {
		// Numbers. \d+\.?\d* can split a run of digits at every position, which
		// is quadratic when the \b after it fails. Only a dot may separate them.
		`\b[+\-]?(0x[\da-f]+|0o[0-7]+|(\d+\.?\d*|\.?\d+)(e[\+\-]?\d+)?|\.inf|\.nan)\b`: `\b[+\-]?(0x[\da-f]+|0o[0-7]+|(\d+(?:\.\d*)?|\.\d+)(e[\+\-]?\d+)?|\.inf|\.nan)\b`,
	},
}

func (x *exporter) rawRuleOf(lexer string, all chroma.Rules, r chroma.Rule) (rawRule, error) {
	if sp, ok := specialRules[lexer][r.Pattern]; ok {
		raw := rawRule{pattern: r.Pattern, special: sp}
		if r.Mutator != nil {
			m, err := ruleXML(all, chroma.Rule{Mutator: r.Mutator})
			if err != nil {
				return raw, err
			}
			raw.mutator = m.mutator
		}
		return raw, nil
	}
	return ruleXML(all, r)
}

// ruleXML serialises one rule with chroma's own XML encoder. The other states
// of the lexer exist (empty) so that include and combined rules resolve.
func ruleXML(all chroma.Rules, r chroma.Rule) (rawRule, error) {
	lex, err := chroma.NewLexer(&chroma.Config{Name: "x"}, func() chroma.Rules {
		rules := chroma.Rules{}
		for s := range all {
			rules[s] = nil
		}
		rules["root"] = []chroma.Rule{r}
		return rules
	})
	if err != nil {
		return rawRule{}, err
	}
	data, err := chroma.Marshal(lex)
	if err != nil {
		return rawRule{}, fmt.Errorf("%w (pattern %q)", err, r.Pattern)
	}
	doc, err := parseXML(data)
	if err != nil {
		return rawRule{}, err
	}
	var rule *xmlNode
	for _, st := range doc.child("rules").Children {
		if n, _ := st.attr("name"); n == "root" && len(st.Children) == 1 {
			rule = st.Children[0]
		}
	}
	if rule == nil {
		return rawRule{}, fmt.Errorf("unexpected serialisation of rule %q", r.Pattern)
	}
	out := rawRule{pattern: r.Pattern}
	for _, c := range rule.Children {
		switch c.Name {
		case "include", "combined", "push", "pop", "mutators":
			out.mutator = c
		default:
			out.emitter = c
		}
	}
	return out, nil
}

// flatten expands includes, in place, so every state is a plain rule list.
func (x *exporter) flatten(state string) ([]rawRule, error) {
	if f, ok := x.flat[state]; ok {
		return f, nil
	}
	rs, ok := x.states[state]
	if !ok {
		return nil, fmt.Errorf("unknown state %q", state)
	}
	if x.visiting[state] {
		return nil, fmt.Errorf("include cycle through state %q", state)
	}
	x.visiting[state] = true
	defer delete(x.visiting, state)
	var out []rawRule
	for _, r := range rs {
		if r.mutator != nil && r.mutator.Name == "include" {
			target, _ := r.mutator.attr("state")
			inc, err := x.flatten(target)
			if err != nil {
				return nil, err
			}
			out = append(out, inc...)
			continue
		}
		out = append(out, r)
	}
	x.flat[state] = out
	return out, nil
}

func (x *exporter) stateIndex(name string) int {
	if i, ok := x.stateIdx[name]; ok {
		return i
	}
	x.stateOrd = append(x.stateOrd, name)
	x.stateIdx[name] = len(x.stateOrd) - 1
	return len(x.stateOrd) - 1
}

// mutatorSteps turns a mutator element into table steps.
func (x *exporter) mutatorSteps(m *xmlNode) ([]int, error) {
	if m == nil {
		return nil, nil
	}
	switch m.Name {
	case "push":
		states := m.attrs("state")
		if len(states) == 0 {
			return []int{mutSelf}, nil
		}
		var out []int
		for _, s := range states {
			if s == "#pop" {
				out = append(out, mutPop)
				continue
			}
			if _, ok := x.states[s]; !ok {
				return nil, fmt.Errorf("push of unknown state %q", s)
			}
			out = append(out, x.stateIndex(s))
		}
		return out, nil
	case "pop":
		d, _ := m.attr("depth")
		n, err := strconv.Atoi(d)
		if err != nil {
			return nil, fmt.Errorf("bad pop depth %q", d)
		}
		out := make([]int, n)
		for i := range out {
			out[i] = mutPop
		}
		return out, nil
	case "mutators":
		var out []int
		for _, c := range m.Children {
			steps, err := x.mutatorSteps(c)
			if err != nil {
				return nil, err
			}
			out = append(out, steps...)
		}
		return out, nil
	case "combined":
		states := m.attrs("state")
		name := "__combined_" + strings.Join(states, "__")
		if _, ok := x.states[name]; !ok {
			var combined []rawRule
			for _, s := range states {
				fl, err := x.flatten(s)
				if err != nil {
					return nil, err
				}
				combined = append(combined, fl...)
			}
			x.states[name] = combined
			x.flat[name] = combined
		}
		return []int{x.stateIndex(name)}, nil
	}
	return nil, fmt.Errorf("unsupported mutator %q", m.Name)
}

// emitterValue turns an emitter element into its table value.
func (x *exporter) emitterValue(e *xmlNode) (interface{}, error) {
	switch e.Name {
	case "token":
		t, _ := e.attr("type")
		return x.typeFor(t)
	case "bygroups":
		out := []interface{}{emByGroups}
		for _, c := range e.Children {
			v, err := x.emitterValue(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case "usingself":
		s, _ := e.attr("state")
		if _, ok := x.states[s]; !ok {
			return nil, fmt.Errorf("usingself of unknown state %q", s)
		}
		return []interface{}{emUsingSelf, x.stateIndex(s)}, nil
	case "using":
		l, _ := e.attr("lexer")
		target := lexers.Get(l)
		if target == nil {
			return nil, fmt.Errorf("using unknown lexer %q", l)
		}
		name := target.Config().Name
		x.deps[name] = true
		return []interface{}{emUsing, name}, nil
	case "usingbygroup":
		x.fences = true
		ng, cg := 0, 0
		var ems []interface{}
		for _, c := range e.Children {
			switch c.Name {
			case "sublexer_name_group":
				ng, _ = strconv.Atoi(strings.TrimSpace(c.Text))
			case "code_group":
				cg, _ = strconv.Atoi(strings.TrimSpace(c.Text))
			case "emitters":
				for _, ec := range c.Children {
					v, err := x.emitterValue(ec)
					if err != nil {
						return nil, err
					}
					ems = append(ems, v)
				}
			}
		}
		return append([]interface{}{emUsingGroup, ng, cg}, ems...), nil
	}
	return nil, fmt.Errorf("unsupported emitter %q", e.Name)
}

func (x *exporter) build(cfg *chroma.Config, frontmatter bool) (*Table, error) {
	x.stateIndex("root")
	// Flatten every state up front so combined states are created before the
	// state list is frozen.
	names := make([]string, 0, len(x.states))
	for s := range x.states {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		if _, err := x.flatten(s); err != nil {
			return nil, err
		}
	}
	t := &Table{Name: cfg.Name, EnsureNL: cfg.EnsureNL, Frontmatter: frontmatter}
	done := map[string]bool{}
	var rulesByState = map[string][]*Rule{}
	emit := func(state string) error {
		if done[state] {
			return nil
		}
		done[state] = true
		var out []*Rule
		for _, r := range x.flat[state] {
			rule := &Rule{}
			if r.pattern != "" {
				pattern := r.pattern
				if o, ok := patternOverrides[x.name][pattern]; ok {
					pattern = o
				}
				res, err := transpileRule(pattern, x.flags)
				if err != nil {
					return fmt.Errorf("state %s: pattern %q: %w", state, r.pattern, err)
				}
				rule.Pattern = res.Src
				if r.emitter != nil && r.emitter.Name == "bygroups" && len(r.emitter.Children) != res.Groups {
					return fmt.Errorf("state %s: pattern %q has %d groups for %d emitters", state, r.pattern, res.Groups, len(r.emitter.Children))
				}
			}
			switch {
			case r.special != nil:
				v, err := r.special(x)
				if err != nil {
					return err
				}
				rule.Emit = v
			case r.emitter != nil:
				v, err := x.emitterValue(r.emitter)
				if err != nil {
					return fmt.Errorf("state %s: pattern %q: %w", state, r.pattern, err)
				}
				rule.Emit = v
			}
			steps, err := x.mutatorSteps(r.mutator)
			if err != nil {
				return fmt.Errorf("state %s: pattern %q: %w", state, r.pattern, err)
			}
			rule.Mut = steps
			out = append(out, rule)
		}
		rulesByState[state] = out
		return nil
	}
	// Emit in index order; mutators and emitters may register new states.
	for i := 0; i < len(x.stateOrd); i++ {
		if err := emit(x.stateOrd[i]); err != nil {
			return nil, err
		}
	}
	for i, s := range x.stateOrd {
		_ = i
		t.Rules = append(t.Rules, rulesByState[s])
		t.States = append(t.States, s)
	}
	t.Types = x.types
	for d := range x.deps {
		t.Deps = append(t.Deps, d)
	}
	sort.Strings(t.Deps)
	t.Fences = x.fences
	return t, nil
}

// JSON encodes the table compactly: {"n":name,"t":[classes],"s":[[rules]],...}
// where each rule is [pattern] or [pattern, emitter, steps].
func (t *Table) JSON() []byte {
	rules := make([]interface{}, len(t.Rules))
	for i, rs := range t.Rules {
		list := make([]interface{}, 0, len(rs))
		for _, r := range rs {
			item := []interface{}{r.Pattern}
			switch {
			case len(r.Mut) > 0:
				item = append(item, r.Emit, r.Mut)
			case r.Emit != nil:
				item = append(item, r.Emit)
			}
			list = append(list, item)
		}
		rules[i] = list
	}
	obj := map[string]interface{}{"n": t.Name, "t": t.Types, "s": rules}
	if t.EnsureNL {
		obj["e"] = 1
	}
	if t.Frontmatter {
		obj["f"] = 1
	}
	if t.Fences {
		obj["g"] = 1
	}
	if len(t.Deps) > 0 {
		obj["d"] = t.Deps
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(obj)
	return bytes.TrimRight(buf.Bytes(), "\n")
}
