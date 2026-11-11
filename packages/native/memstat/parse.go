package memstat

import (
	"bufio"
	"io"
	"slices"
	"strconv"
	"strings"
)

// parseSmaps reads /proc/<pid>/smaps_rollup text, or /proc/<pid>/smaps text
// (the per-mapping fields are summed). Values are kB in the file and bytes in
// the result.
func parseSmaps(r io.Reader) (Proc, error) {
	var p Proc
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) != 2 || f[1] != "kB" {
			continue
		}
		kb, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		v := kb * 1024
		switch key {
		case "Rss":
			p.RSS += v
		case "Pss":
			p.PSS += v
		case "Private_Dirty":
			p.PrivateDirty += v
		case "Private_Clean":
			p.PrivateClean += v
		case "Anonymous":
			p.Anon += v
		case "Swap":
			p.Swap += v
		}
	}
	return p, sc.Err()
}

// parseStatPPID returns the parent pid from /proc/<pid>/stat text, whose
// second field (the command name) may contain spaces and parentheses.
func parseStatPPID(s string) (int, bool) {
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(f[1])
	return n, err == nil
}

// descendantsOf returns every process below root given a pid to parent map,
// breadth first, without root itself.
func descendantsOf(root int, parent map[int]int) []int {
	kids := map[int][]int{}
	for pid, pp := range parent {
		kids[pp] = append(kids[pp], pid)
	}
	var out []int
	queue := []int{root}
	seen := map[int]bool{root: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		ch := kids[cur]
		slices.Sort(ch)
		for _, c := range ch {
			if seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}
