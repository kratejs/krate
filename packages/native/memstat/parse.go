//go:build !darwin

package memstat

import (
	"slices"
)

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
