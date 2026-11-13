package gobind

import (
	"fmt"
	"go/format"
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	wireImport   = "github.com/kratejs/krate/packages/native/wire"
	schemeImport = "github.com/kratejs/krate/packages/native/scheme"
)

var reservedNames = map[string]bool{
	"wire": true, "krate": true, "scheme": true, "fmt": true, "math": true, "sort": true,
	"e": true, "d": true, "v": true, "x": true, "k": true, "i": true, "j": true, "r": true,
	"out": true, "args": true, "ctx": true, "err": true, "res": true, "ch": true, "ok": true,
	"fn": true, "emit": true, "keys": true, "len": true, "make": true, "append": true,
	"string": true, "int": true, "bool": true, "error": true, "nil": true, "new": true,
}

var argNameRe = regexp.MustCompile(`^a[0-9]+$`)

type codec struct {
	name string
	w    *wtype
	enc  bool
	dec  bool

	encDone, decDone bool
}

// gen writes the glue for one user package.
type gen struct {
	pkg     *userPkg
	imports map[string]string // import path -> alias
	aliases map[string]bool
	uses    map[string]bool
	codecs  map[string]*codec
	names   map[string]bool
	order   []*codec
	helpers map[string]string // helper name -> source
}

func newGen(pkg *userPkg) *gen {
	return &gen{
		pkg: pkg, imports: map[string]string{}, aliases: map[string]bool{},
		uses: map[string]bool{}, codecs: map[string]*codec{}, names: map[string]bool{},
		helpers: map[string]string{},
	}
}

func (g *gen) alias(path, name string) string {
	if a, ok := g.imports[path]; ok {
		return a
	}
	cand := name
	for i := 2; reservedNames[cand] || argNameRe.MatchString(cand) || g.aliases[cand]; i++ {
		cand = name + strconv.Itoa(i)
	}
	g.imports[path] = cand
	g.aliases[cand] = true
	return cand
}

func (g *gen) qual(p *types.Package) string { return g.alias(p.Path(), p.Name()) }

func (g *gen) typeStr(t types.Type) string { return types.TypeString(t, g.qual) }

func (g *gen) use(names ...string) {
	for _, n := range names {
		g.uses[n] = true
	}
}

// needsFn reports whether w gets its own codec functions.
func needsFn(w *wtype) bool {
	if w.kind == kTime {
		return false
	}
	if w.named != nil {
		return true
	}
	switch w.kind {
	case kSlice, kArray, kMap, kPtr, kStruct:
		return true
	}
	return false
}

func scalarName(w *wtype) string {
	switch w.kind {
	case kBytes:
		return "Bytes"
	case kTime:
		return "Time"
	}
	if w.basic != nil {
		n := w.basic.Name()
		return strings.ToUpper(n[:1]) + n[1:]
	}
	return "Value"
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]+`)

// namedBase is the identifier-safe name of a named type, with type arguments.
func namedBase(n *types.Named) string {
	if n.TypeArgs().Len() == 0 {
		return n.Obj().Name()
	}
	s := n.Obj().Name()
	for i := 0; i < n.TypeArgs().Len(); i++ {
		s += "_" + typeName(n.TypeArgs().At(i))
	}
	return strings.Trim(nonAlnum.ReplaceAllString(s, "_"), "_")
}

func baseName(w *wtype) string {
	if w.named != nil {
		return namedBase(w.named)
	}
	switch w.kind {
	case kSlice:
		return "SliceOf" + baseName(w.elem)
	case kArray:
		return "ArrayOf" + baseName(w.elem)
	case kMap:
		return "MapOf" + baseName(w.elem)
	case kPtr:
		return "PtrTo" + baseName(w.elem)
	case kStruct:
		return "Struct"
	}
	return scalarName(w)
}

func (g *gen) codecFor(w *wtype) *codec {
	key := typeKey(w.gt)
	if c, ok := g.codecs[key]; ok {
		return c
	}
	base := baseName(w)
	name := base
	for i := 2; g.names[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	g.names[name] = true
	c := &codec{name: name, w: w}
	g.codecs[key] = c
	g.order = append(g.order, c)
	return c
}

func (g *gen) encName(w *wtype) string {
	c := g.codecFor(w)
	c.enc = true
	return "encode" + c.name
}

func (g *gen) decName(w *wtype) string {
	c := g.codecFor(w)
	c.dec = true
	return "decode" + c.name
}

// enc returns a statement that encodes the value x of type w.
func (g *gen) enc(w *wtype, x string) string {
	if needsFn(w) {
		return g.encName(w) + "(e, " + x + ")"
	}
	return g.encScalar(w, x)
}

func (g *gen) encScalar(w *wtype, x string) string {
	switch w.kind {
	case kBool:
		return "e.Bool(" + bareConv(w, "bool", x) + ")"
	case kString:
		return "e.String(" + bareConv(w, "string", x) + ")"
	case kInt:
		return "e.Int(" + bareConv(w, "int64", x) + ")"
	case kUint:
		return "e.Uint(" + bareConv(w, "uint64", x) + ")"
	case kFloat:
		return "e.Float(" + bareConv(w, "float64", x) + ")"
	case kBytes:
		return "e.Bytes(" + bareConv(w, "[]byte", x) + ")"
	case kTime:
		return "e.Time(" + x + ")"
	}
	return "// unreachable"
}

// bareConv converts x to the plain Go type when w is not already it.
func bareConv(w *wtype, plain, x string) string {
	if w.named == nil && w.kind == kBytes {
		return x
	}
	if t, ok := w.gt.(*types.Basic); ok && t.Name() == plain {
		return x
	}
	return plain + "(" + x + ")"
}

// dec returns an expression that decodes a value of type w as (T, error).
func (g *gen) dec(w *wtype) string {
	if needsFn(w) {
		return g.decName(w) + "(d)"
	}
	return g.decScalar(w)
}

func (g *gen) decScalar(w *wtype) string {
	switch w.kind {
	case kBool:
		return "d.Bool()"
	case kString:
		return "d.String()"
	case kTime:
		return "d.Time()"
	case kBytes:
		g.helper("decodeBytes", "")
		return "decodeBytes(d)"
	case kInt, kUint, kFloat:
		switch w.basic.Kind() {
		case types.Int64:
			return "d.Int()"
		case types.Uint64:
			return "d.Uint()"
		case types.Float64:
			return "d.Float()"
		}
		canon := types.Typ[w.basic.Kind()].Name()
		name := "decode" + capitalize(canon)
		g.helper(name, canon)
		return name + "(d)"
	}
	return ""
}

// helper registers a narrow-number decoder.
func (g *gen) helper(name, goType string) {
	if _, ok := g.helpers[name]; ok {
		return
	}
	var b strings.Builder
	switch {
	case name == "decodeBytes":
		b.WriteString("func decodeBytes(d *wire.Decoder) ([]byte, error) {\n\tb, err := d.Bytes()\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn append([]byte(nil), b...), nil\n}\n")
	case goType == "float32":
		b.WriteString("func decodeFloat32(d *wire.Decoder) (float32, error) {\n\tx, err := d.Float()\n\treturn float32(x), err\n}\n")
	default:
		src, lo, hi := "d.Int()", "math.Min"+capitalize(goType), "math.Max"+capitalize(goType)
		if strings.HasPrefix(goType, "uint") {
			src = "d.Uint()"
		}
		fmt.Fprintf(&b, "func %s(d *wire.Decoder) (%s, error) {\n\tx, err := %s\n\tif err != nil {\n\t\treturn 0, err\n\t}\n", name, goType, src)
		if strings.HasPrefix(goType, "uint") {
			fmt.Fprintf(&b, "\tif x > %s {\n", hi)
		} else {
			fmt.Fprintf(&b, "\tif x < %s || x > %s {\n", lo, hi)
		}
		fmt.Fprintf(&b, "\t\treturn 0, fmt.Errorf(\"%%d overflows %s\", x)\n\t}\n\treturn %s(x), nil\n}\n", goType, goType)
		g.use("fmt", "math")
	}
	g.use("wire")
	g.helpers[name] = b.String()
}

func capitalize(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func omitCond(w *wtype, x string) string {
	switch w.kind {
	case kBool:
		return x
	case kString:
		return x + ` != ""`
	case kInt, kUint, kFloat:
		return x + " != 0"
	case kBytes, kSlice, kMap, kArray:
		return "len(" + x + ") > 0"
	case kPtr:
		return x + " != nil"
	}
	return "true"
}

// encSource and decSource write one direction of c.
func (g *gen) encSource(c *codec) string {
	var b strings.Builder
	g.use("wire")
	fmt.Fprintf(&b, "func encode%s(e *wire.Encoder, v %s) {\n", c.name, g.typeStr(c.w.gt))
	g.encBody(&b, c.w)
	b.WriteString("}\n\n")
	return b.String()
}

func (g *gen) decSource(c *codec) string {
	var b strings.Builder
	t := g.typeStr(c.w.gt)
	g.use("wire")
	fmt.Fprintf(&b, "func decode%s(d *wire.Decoder) (%s, error) {\n", c.name, t)
	g.decBody(&b, c.w, t)
	b.WriteString("}\n\n")
	return b.String()
}

func (g *gen) encBody(b *strings.Builder, w *wtype) {
	switch w.kind {
	case kSlice, kArray:
		b.WriteString("\te.BeginArray()\n\tfor _, x := range v {\n\t\t" + g.enc(w.elem, "x") + "\n\t}\n\te.EndArray()\n")
	case kMap:
		g.use("sort")
		kt := g.typeStr(w.key)
		b.WriteString("\te.BeginObject()\n\tkeys := make([]" + kt + ", 0, len(v))\n\tfor k := range v {\n\t\tkeys = append(keys, k)\n\t}\n")
		b.WriteString("\tsort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })\n\tfor _, k := range keys {\n\t\te.Key(string(k))\n")
		b.WriteString("\t\t" + g.enc(w.elem, "v[k]") + "\n\t}\n\te.EndObject()\n")
	case kPtr:
		b.WriteString("\tif v == nil {\n\t\te.Null()\n\t\treturn\n\t}\n\t" + g.enc(w.elem, "*v") + "\n")
	case kStruct:
		b.WriteString("\te.BeginObject()\n")
		for _, f := range w.fields {
			x := "v." + f.sel
			ind := "\t"
			if f.omitEmpty && omitCond(f.t, x) != "true" {
				b.WriteString("\tif " + omitCond(f.t, x) + " {\n")
				ind = "\t\t"
			}
			b.WriteString(ind + "e.Key(" + strconv.Quote(f.json) + ")\n" + ind + g.enc(f.t, x) + "\n")
			if ind == "\t\t" {
				b.WriteString("\t}\n")
			}
		}
		b.WriteString("\te.EndObject()\n")
	default:
		b.WriteString("\t" + g.encScalar(w, "v") + "\n")
	}
}

func (g *gen) decBody(b *strings.Builder, w *wtype, t string) {
	switch w.kind {
	case kSlice:
		g.use("fmt")
		b.WriteString("\tif d.IsNull() {\n\t\treturn nil, nil\n\t}\n\tif err := d.BeginArray(); err != nil {\n\t\treturn nil, err\n\t}\n")
		b.WriteString("\tout := " + t + "{}\n\tfor d.More() {\n\t\tx, err := " + g.dec(w.elem) + "\n")
		b.WriteString("\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf(\"[%d]: %w\", len(out), err)\n\t\t}\n\t\tout = append(out, x)\n\t}\n")
		b.WriteString("\tif err := d.EndArray(); err != nil {\n\t\treturn nil, err\n\t}\n\treturn out, nil\n")
	case kArray:
		g.use("fmt")
		n := strconv.FormatInt(w.size, 10)
		b.WriteString("\tvar out " + t + "\n\tif err := d.BeginArray(); err != nil {\n\t\treturn out, err\n\t}\n\ti := 0\n\tfor d.More() {\n")
		b.WriteString("\t\tif i >= " + n + " {\n\t\t\treturn out, fmt.Errorf(\"more than " + n + " elements\")\n\t\t}\n")
		b.WriteString("\t\tx, err := " + g.dec(w.elem) + "\n\t\tif err != nil {\n\t\t\treturn out, fmt.Errorf(\"[%d]: %w\", i, err)\n\t\t}\n\t\tout[i] = x\n\t\ti++\n\t}\n")
		b.WriteString("\terr := d.EndArray()\n\treturn out, err\n")
	case kMap:
		g.use("fmt")
		kt := g.typeStr(w.key)
		b.WriteString("\tif d.IsNull() {\n\t\treturn nil, nil\n\t}\n\tif err := d.BeginObject(); err != nil {\n\t\treturn nil, err\n\t}\n\tout := make(" + t + ")\n\tfor d.More() {\n")
		b.WriteString("\t\tk, err := d.Key()\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tx, err := " + g.dec(w.elem) + "\n")
		b.WriteString("\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf(\"key %q: %w\", k, err)\n\t\t}\n\t\tout[" + kt + "(k)] = x\n\t}\n")
		b.WriteString("\tif err := d.EndObject(); err != nil {\n\t\treturn nil, err\n\t}\n\treturn out, nil\n")
	case kPtr:
		b.WriteString("\tif d.IsNull() {\n\t\treturn nil, nil\n\t}\n\tx, err := " + g.dec(w.elem) + "\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn &x, nil\n")
	case kStruct:
		g.use("fmt")
		b.WriteString("\tvar v " + t + "\n\tvar err error\n\tif err = d.BeginObject(); err != nil {\n\t\treturn v, err\n\t}\n\tfor d.More() {\n")
		b.WriteString("\t\tvar k string\n\t\tif k, err = d.Key(); err != nil {\n\t\t\treturn v, err\n\t\t}\n\t\tswitch k {\n")
		for _, f := range w.fields {
			b.WriteString("\t\tcase " + strconv.Quote(f.json) + ":\n\t\t\tv." + f.sel + ", err = " + g.dec(f.t) + "\n")
		}
		b.WriteString("\t\tdefault:\n\t\t\terr = d.Skip()\n\t\t}\n\t\tif err != nil {\n\t\t\treturn v, fmt.Errorf(\"field %q: %w\", k, err)\n\t\t}\n\t}\n")
		b.WriteString("\terr = d.EndObject()\n\treturn v, err\n")
	default:
		b.WriteString("\tx, err := " + g.decScalar(w) + "\n\treturn " + t + "(x), err\n")
	}
}

// argDecode writes the argument-reading prologue shared by calls and streams.
func (g *gen) argDecode(b *strings.Builder, it *hostItem) {
	g.use("krate", "wire")
	b.WriteString("\tconst fn = " + strconv.Quote(it.id) + "\n\tvar err error\n")
	b.WriteString("\tif err = d.BeginArray(); err != nil {\n\t\treturn krate.Errorf(\"bad_args\", \"%s: %w\", fn, err)\n\t}\n")
	for i, p := range it.params {
		label := "argument " + strconv.Itoa(i)
		if p.name != "" && p.name != "_" {
			label += " (" + p.name + ")"
		}
		fmt.Fprintf(b, "\tif !d.More() {\n\t\treturn krate.Errorf(\"bad_args\", \"%%s: missing %s\", fn)\n\t}\n", label)
		fmt.Fprintf(b, "\ta%d, err := %s\n", i, g.dec(p.w))
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn krate.Errorf(\"bad_args\", \"%%s: %s: %%w\", fn, err)\n\t}\n", label)
	}
	b.WriteString("\tif err = d.EndArray(); err != nil {\n\t\treturn krate.Errorf(\"bad_args\", \"%s: %w\", fn, err)\n\t}\n")
}

func (g *gen) callExpr(pkgAlias string, it *hostItem) string {
	var as []string
	if it.ctx {
		as = append(as, "ctx")
	}
	for i := range it.params {
		as = append(as, "a"+strconv.Itoa(i))
	}
	return pkgAlias + "." + it.name + "(" + strings.Join(as, ", ") + ")"
}

func (g *gen) funcSource(pkgAlias string, it *hostItem) string {
	var b strings.Builder
	g.use("krate", "wire")
	switch it.kind {
	case itemFunc:
		fmt.Fprintf(&b, "func call%s(ctx krate.Ctx, d *wire.Decoder, e *wire.Encoder) error {\n", it.name)
		g.argDecode(&b, it)
		call := g.callExpr(pkgAlias, it)
		switch {
		case it.result == nil && it.hasErr:
			b.WriteString("\tif err = " + call + "; err != nil {\n\t\treturn err\n\t}\n\te.Null()\n")
		case it.result == nil:
			b.WriteString("\t" + call + "\n\te.Null()\n")
		case it.hasErr:
			b.WriteString("\tres, err := " + call + "\n\tif err != nil {\n\t\treturn err\n\t}\n\t" + g.enc(it.result, "res") + "\n")
		default:
			b.WriteString("\tres := " + call + "\n\t" + g.enc(it.result, "res") + "\n")
		}
		b.WriteString("\treturn nil\n}\n\n")
	case itemStream:
		fmt.Fprintf(&b, "func stream%s(ctx krate.Ctx, d *wire.Decoder, emit func(write func(*wire.Encoder)) error) error {\n", it.name)
		g.argDecode(&b, it)
		call := g.callExpr(pkgAlias, it)
		if it.hasErr {
			b.WriteString("\tch, err := " + call + "\n\tif err != nil {\n\t\treturn err\n\t}\n")
		} else {
			b.WriteString("\tch := " + call + "\n")
		}
		b.WriteString("\tfor {\n\t\tselect {\n\t\tcase <-ctx.Done():\n\t\t\treturn ctx.Err()\n\t\tcase v, ok := <-ch:\n\t\t\tif !ok {\n\t\t\t\treturn nil\n\t\t\t}\n")
		b.WriteString("\t\t\tif err := emit(func(e *wire.Encoder) { " + g.enc(it.result, "v") + " }); err != nil {\n\t\t\t\treturn err\n\t\t\t}\n\t\t}\n\t}\n}\n\n")
	}
	return b.String()
}

// glueSource generates the glue file for the items of one package.
func glueSource(pkg *userPkg, items []*hostItem) ([]byte, error) {
	g := newGen(pkg)
	up := g.alias(pkg.importPath, pkg.name)
	g.use("scheme")
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })

	var funcs, reg strings.Builder
	for _, it := range items {
		switch it.kind {
		case itemFunc:
			funcs.WriteString(g.funcSource(up, it))
			reg.WriteString("\tr.Func(" + strconv.Quote(it.id) + ", call" + it.name + ")\n")
		case itemStream:
			funcs.WriteString(g.funcSource(up, it))
			reg.WriteString("\tr.Stream(" + strconv.Quote(it.id) + ", stream" + it.name + ")\n")
		case itemSignal:
			g.use("wire")
			reg.WriteString("\tr.Signal(" + strconv.Quote(it.id) + ", scheme.SignalOf(" + up + "." + it.name +
				", func(e *wire.Encoder, v " + g.typeStr(it.result.gt) + ") { " + g.enc(it.result, "v") + " }))\n")
		}
	}

	// Codec bodies can request more codecs and more directions, so repeat
	// until nothing new is asked for.
	srcs := map[string]string{}
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(g.order); i++ {
			c := g.order[i]
			if c.enc && !c.encDone {
				c.encDone, changed = true, true
				srcs[c.name+"/0"] = g.encSource(c)
			}
			if c.dec && !c.decDone {
				c.decDone, changed = true, true
				srcs[c.name+"/1"] = g.decSource(c)
			}
		}
	}
	var codecs []string
	for n := range srcs {
		codecs = append(codecs, n)
	}
	sort.Strings(codecs)

	var b strings.Builder
	b.WriteString("// Code generated by krate. DO NOT EDIT.\n\npackage glue\n\nimport (\n")
	std := []string{}
	for _, n := range []string{"fmt", "math", "sort"} {
		if g.uses[n] {
			std = append(std, n)
		}
	}
	for _, n := range std {
		b.WriteString("\t" + strconv.Quote(n) + "\n")
	}
	if len(std) > 0 {
		b.WriteString("\n")
	}
	type imp struct{ alias, path string }
	var imps []imp
	for p, a := range g.imports {
		imps = append(imps, imp{a, p})
	}
	if g.uses["krate"] {
		imps = append(imps, imp{"krate", krateImportPath})
	}
	if g.uses["scheme"] {
		imps = append(imps, imp{"scheme", schemeImport})
	}
	if g.uses["wire"] {
		imps = append(imps, imp{"wire", wireImport})
	}
	sort.Slice(imps, func(i, j int) bool { return imps[i].path < imps[j].path })
	for _, im := range imps {
		b.WriteString("\t" + im.alias + " " + strconv.Quote(im.path) + "\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("// Register adds the host functions, streams and signals the UI imports.\nfunc Register(r *scheme.Registry) {\n" + reg.String() + "}\n\n")
	b.WriteString(funcs.String())
	for _, n := range codecs {
		b.WriteString(srcs[n])
	}
	var hn []string
	for n := range g.helpers {
		hn = append(hn, n)
	}
	sort.Strings(hn)
	for _, n := range hn {
		b.WriteString(g.helpers[n] + "\n")
	}
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("formatting generated glue for %s: %w\n%s", pkg.importPath, err, b.String())
	}
	return out, nil
}
