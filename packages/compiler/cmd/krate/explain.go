package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const diagnosticsDocsURL = "https://krate.js.org/docs/reference/diagnostics/"

var codeArgRe = regexp.MustCompile(`^(?i:kr)?[-_ ]?(\d{4})$`)

// normalizeCode turns "kr7110", "KR-7110" or "7110" into "KR7110".
func normalizeCode(arg string) (diag.Code, bool) {
	m := codeArgRe.FindStringSubmatch(strings.TrimSpace(arg))
	if m == nil {
		return "", false
	}
	return diag.Code("KR" + m[1]), true
}

// codeDocsURL links to the code's entry on the diagnostics reference page.
func codeDocsURL(code diag.Code) string {
	return diagnosticsDocsURL + "#" + strings.ToLower(string(code))
}

// explainEntry is the machine-readable form of an explained code.
type explainEntry struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Explanation string `json:"explanation"`
	DocsURL     string `json:"docsUrl"`
}

// writeExplain prints the explanation for one code. It returns an error text
// when the argument is not a registered code, with the nearest codes as hints.
func writeExplain(w io.Writer, arg string, asJSON bool) error {
	code, ok := normalizeCode(arg)
	if !ok {
		return fmt.Errorf("%q is not a diagnostic code (expected KR followed by four digits, such as KR7110)", arg)
	}
	info, found := diag.Lookup(code)
	if !found {
		msg := fmt.Sprintf("%s is not a registered diagnostic code", code)
		if near := nearbyCodes(code, 5); len(near) > 0 {
			msg += "; nearest codes: " + strings.Join(near, ", ")
		}
		return fmt.Errorf("%s (see %s)", msg, diagnosticsDocsURL)
	}
	if asJSON {
		data, err := json.MarshalIndent(explainEntry{string(info.Code), info.Title, info.Explanation, codeDocsURL(code)}, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}
	fmt.Fprintf(w, "%s%s%s  %s\n\n", cBold, info.Code, cReset, info.Title)
	fmt.Fprintf(w, "%s\n\n", strings.TrimSpace(info.Explanation))
	fmt.Fprintf(w, "%sDocs:%s %s\n", cGray, cReset, codeDocsURL(code))
	return nil
}

// nearbyCodes lists up to n registered codes numerically closest to code, in
// ascending order.
func nearbyCodes(code diag.Code, n int) []string {
	target, err := strconv.Atoi(string(code)[2:])
	if err != nil {
		return nil
	}
	all := diag.Codes()
	dist := func(c diag.Code) int {
		v, _ := strconv.Atoi(string(c)[2:])
		if v > target {
			return v - target
		}
		return target - v
	}
	sort.SliceStable(all, func(i, j int) bool { return dist(all[i].Code) < dist(all[j].Code) })
	if len(all) > n {
		all = all[:n]
	}
	out := make([]string, 0, len(all))
	for _, info := range all {
		out = append(out, string(info.Code))
	}
	sort.Strings(out)
	return out
}

// runExplain implements `krate explain <code>`.
func runExplain(flags cliFlags, args []string) {
	if len(args) < 2 {
		printCommandUsage(os.Stderr, "explain")
		exit(1)
	}
	if err := writeExplain(os.Stdout, args[1], flags.JSON); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}
}
