package css

import "strings"

// containerUtilityCSS resolves the container-query container utilities:
// `@container`, `@container/name`, `@container-normal`, `@container-size`
// and `@container-inline-size`, each with an optional `/name`.
func containerUtilityCSS(base string) (string, bool) {
	rest, ok := strings.CutPrefix(base, "@container")
	if !ok {
		return "", false
	}
	rest, name, hasName := strings.Cut(rest, "/")
	if hasName {
		name = strings.TrimSpace(decodeArbitrary(strings.TrimSuffix(strings.TrimPrefix(name, "["), "]")))
		if name == "" {
			return "", false
		}
	}
	typ := "inline-size"
	switch rest {
	case "":
	case "-normal":
		typ = "normal"
	case "-size":
		typ = "size"
	case "-inline-size":
	default:
		return "", false
	}
	css := "container-type: " + typ + ";"
	if hasName {
		css += " container-name: " + name + ";"
	}
	return css, true
}
