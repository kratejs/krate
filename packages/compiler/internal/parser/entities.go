package parser

import (
	"html"
	"strings"
	"sync"
)

// jsxEntityNames is the named character reference set JSX supports: the
// HTML 4 / XHTML 1.0 entities (the same list as Babel's xhtml.js). HTML5-only
// names (`&rarrb;`, `&NotEqual;`, ...) are not decoded in JSX.
const jsxEntityNames = `quot amp apos lt gt nbsp iexcl cent pound curren yen brvbar sect uml copy
ordf laquo not shy reg macr deg plusmn sup2 sup3 acute micro para middot cedil sup1 ordm raquo
frac14 frac12 frac34 iquest Agrave Aacute Acirc Atilde Auml Aring AElig Ccedil Egrave Eacute
Ecirc Euml Igrave Iacute Icirc Iuml ETH Ntilde Ograve Oacute Ocirc Otilde Ouml times Oslash
Ugrave Uacute Ucirc Uuml Yacute THORN szlig agrave aacute acirc atilde auml aring aelig ccedil
egrave eacute ecirc euml igrave iacute icirc iuml eth ntilde ograve oacute ocirc otilde ouml
divide oslash ugrave uacute ucirc uuml yacute thorn yuml OElig oelig Scaron scaron Yuml fnof
circ tilde Alpha Beta Gamma Delta Epsilon Zeta Eta Theta Iota Kappa Lambda Mu Nu Xi Omicron Pi
Rho Sigma Tau Upsilon Phi Chi Psi Omega alpha beta gamma delta epsilon zeta eta theta iota kappa
lambda mu nu xi omicron pi rho sigmaf sigma tau upsilon phi chi psi omega thetasym upsih piv
ensp emsp thinsp zwnj zwj lrm rlm ndash mdash lsquo rsquo sbquo ldquo rdquo bdquo dagger Dagger
bull hellip permil prime Prime lsaquo rsaquo oline frasl euro image weierp real trade alefsym
larr uarr rarr darr harr crarr lArr uArr rArr dArr hArr forall part exist empty nabla isin
notin ni prod sum minus lowast radic prop infin ang and or cap cup int there4 sim cong asymp ne
equiv le ge sub sup nsub sube supe oplus otimes perp sdot lceil rceil lfloor rfloor lang rang
loz spades clubs hearts diams`

var (
	entityOnce  sync.Once
	entityTable map[string]string
)

// jsxEntities returns the entity name to text table. Values come from the
// standard library's HTML table; `lang`/`rang` use their HTML 4 code points
// (U+2329/U+232A), as Babel does.
func jsxEntities() map[string]string {
	entityOnce.Do(func() {
		names := strings.Fields(jsxEntityNames)
		entityTable = make(map[string]string, len(names))
		for _, n := range names {
			entityTable[n] = html.UnescapeString("&" + n + ";")
		}
		entityTable["lang"] = string(rune(0x2329))
		entityTable["rang"] = string(rune(0x232A))
	})
	return entityTable
}
