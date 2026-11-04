package css

import "strings"

// filterFnNames are the filter functions with a bare-number or arbitrary
// value (blur and drop-shadow use scales and live in tailwind_extra.go).
var filterFnNames = []string{"brightness", "contrast", "grayscale", "hue-rotate", "invert", "opacity", "saturate", "sepia"}

// filterFnUtility resolves brightness-*, contrast-*, grayscale, hue-rotate-*,
// invert, saturate-*, sepia and their backdrop- forms (plus backdrop-opacity-*
// and the negative hue-rotate). A bare number is a percentage, or degrees for
// hue-rotate, like Tailwind v4.
func filterFnUtility(cls string, neg bool) (string, bool) {
	backdrop := false
	if rest, ok := strings.CutPrefix(cls, "backdrop-"); ok {
		backdrop, cls = true, rest
	}
	for _, name := range filterFnNames {
		if name == "opacity" && !backdrop {
			continue
		}
		var arg string
		switch {
		case cls == name:
			if name != "grayscale" && name != "invert" && name != "sepia" {
				continue
			}
			arg = "100%"
		case strings.HasPrefix(cls, name+"-"):
			key := cls[len(name)+1:]
			if v, isArb := unwrapArbitrary(key); isArb {
				arg = v
			} else if plainNumber(key) {
				arg = key + "%"
				if name == "hue-rotate" {
					arg = key + "deg"
				}
			} else {
				continue
			}
		default:
			continue
		}
		if neg {
			if name != "hue-rotate" {
				return "", false
			}
			arg = "calc(" + arg + " * -1)"
		}
		return applyFilter(name, name+"("+arg+")", backdrop), true
	}
	return "", false
}
