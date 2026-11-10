// Package qr encodes short text as a QR code and renders it for a terminal.
// It supports byte mode with error correction level L, versions 1 to 10 (up
// to 271 bytes), which is plenty for a dev server URL.
package qr

import (
	"errors"
	"strings"
)

// Matrix is a square QR symbol; true is a dark module.
type Matrix [][]bool

// ecBlocks describes the level L layout of one version.
type ecBlocks struct {
	ecPerBlock int
	g1Blocks   int
	g1Data     int
	g2Blocks   int
	g2Data     int
}

func (b ecBlocks) dataCodewords() int { return b.g1Blocks*b.g1Data + b.g2Blocks*b.g2Data }

// versions[v-1] is the level L structure of version v.
var versions = [10]ecBlocks{
	{7, 1, 19, 0, 0},
	{10, 1, 34, 0, 0},
	{15, 1, 55, 0, 0},
	{20, 1, 80, 0, 0},
	{26, 1, 108, 0, 0},
	{18, 2, 68, 0, 0},
	{20, 2, 78, 0, 0},
	{24, 2, 97, 0, 0},
	{30, 2, 116, 0, 0},
	{18, 2, 68, 2, 69},
}

// alignmentCenters[v-1] are the alignment pattern center coordinates.
var alignmentCenters = [10][]int{
	{}, {6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34}, {6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50},
}

// ErrTooLong is returned when the text does not fit in version 10.
var ErrTooLong = errors.New("qr: text too long")

// Encode builds the QR symbol for text.
func Encode(text string) (Matrix, error) {
	data := []byte(text)
	version := 0
	for v := 1; v <= len(versions); v++ {
		countBits := 8
		if v >= 10 {
			countBits = 16
		}
		if 4+countBits+8*len(data) <= 8*versions[v-1].dataCodewords() {
			version = v
			break
		}
	}
	if version == 0 {
		return nil, ErrTooLong
	}
	info := versions[version-1]
	codewords := interleave(dataCodewords(data, version, info.dataCodewords()), info)
	size := 17 + 4*version

	m := newBuilder(size, version)
	m.drawFunctionPatterns()
	m.placeData(codewords)

	best, bestScore := -1, 0
	var bestGrid [][]bool
	for mask := 0; mask < 8; mask++ {
		grid := m.withMask(mask)
		if s := penalty(grid); best < 0 || s < bestScore {
			best, bestScore, bestGrid = mask, s, grid
		}
	}
	return Matrix(bestGrid), nil
}

// dataCodewords packs the byte mode header, the data, the terminator and the
// pad bytes into exactly total codewords.
func dataCodewords(data []byte, version, total int) []byte {
	var bits []bool
	put := func(v, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, v>>uint(i)&1 == 1)
		}
	}
	put(0b0100, 4)
	if version >= 10 {
		put(len(data), 16)
	} else {
		put(len(data), 8)
	}
	for _, b := range data {
		put(int(b), 8)
	}
	capacity := total * 8
	for n := 0; n < 4 && len(bits) < capacity; n++ {
		bits = append(bits, false)
	}
	for len(bits)%8 != 0 {
		bits = append(bits, false)
	}
	out := make([]byte, 0, total)
	for i := 0; i < len(bits); i += 8 {
		var b byte
		for j := 0; j < 8; j++ {
			b <<= 1
			if bits[i+j] {
				b |= 1
			}
		}
		out = append(out, b)
	}
	for pad := byte(0xEC); len(out) < total; pad ^= 0xEC ^ 0x11 {
		out = append(out, pad)
	}
	return out
}

// interleave splits the data into blocks, appends each block's error
// correction codewords and interleaves them in the order the standard needs.
func interleave(data []byte, info ecBlocks) []byte {
	var blocks, ecs [][]byte
	pos := 0
	take := func(n int) {
		block := data[pos : pos+n]
		pos += n
		blocks = append(blocks, block)
		ecs = append(ecs, reedSolomon(block, info.ecPerBlock))
	}
	for i := 0; i < info.g1Blocks; i++ {
		take(info.g1Data)
	}
	for i := 0; i < info.g2Blocks; i++ {
		take(info.g2Data)
	}
	var out []byte
	longest := info.g1Data
	if info.g2Blocks > 0 {
		longest = info.g2Data
	}
	for i := 0; i < longest; i++ {
		for _, b := range blocks {
			if i < len(b) {
				out = append(out, b[i])
			}
		}
	}
	for i := 0; i < info.ecPerBlock; i++ {
		for _, e := range ecs {
			out = append(out, e[i])
		}
	}
	return out
}

var gfExp [512]byte
var gfLog [256]int

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

// reedSolomon returns the n error correction codewords for data.
func reedSolomon(data []byte, n int) []byte {
	// Generator polynomial: product of (x - alpha^i) for i in 0..n-1.
	gen := []byte{1}
	for i := 0; i < n; i++ {
		next := make([]byte, len(gen)+1)
		for j, c := range gen {
			next[j] ^= c
			next[j+1] ^= gfMul(c, gfExp[i])
		}
		gen = next
	}
	rem := make([]byte, n)
	for _, d := range data {
		factor := d ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		for i := 0; i < n; i++ {
			rem[i] ^= gfMul(gen[i+1], factor)
		}
	}
	return rem
}

// builder lays out the function patterns and the data modules.
type builder struct {
	size, version int
	dark          [][]bool
	function      [][]bool // true where a module is not data
}

func newBuilder(size, version int) *builder {
	b := &builder{size: size, version: version}
	b.dark = make([][]bool, size)
	b.function = make([][]bool, size)
	for i := range b.dark {
		b.dark[i] = make([]bool, size)
		b.function[i] = make([]bool, size)
	}
	return b
}

func (b *builder) set(x, y int, dark bool) {
	b.dark[y][x] = dark
	b.function[y][x] = true
}

func (b *builder) drawFunctionPatterns() {
	for i := 0; i < b.size; i++ {
		b.set(6, i, i%2 == 0)
		b.set(i, 6, i%2 == 0)
	}
	b.finder(3, 3)
	b.finder(b.size-4, 3)
	b.finder(3, b.size-4)
	centers := alignmentCenters[b.version-1]
	for _, cy := range centers {
		for _, cx := range centers {
			last := len(centers) - 1
			if (cx == centers[0] && cy == centers[0]) || (cx == centers[0] && cy == centers[last]) || (cx == centers[last] && cy == centers[0]) {
				continue
			}
			b.alignment(cx, cy)
		}
	}
	b.reserveFormat()
	if b.version >= 7 {
		b.drawVersion()
	}
}

// finder draws a finder pattern and its separator centered at (cx, cy).
func (b *builder) finder(cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= b.size || y >= b.size {
				continue
			}
			dist := max(abs(dx), abs(dy))
			b.set(x, y, dist != 2 && dist != 4)
		}
	}
}

func (b *builder) alignment(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			b.set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (b *builder) reserveFormat() {
	for i := 0; i < 9; i++ {
		if i != 6 {
			b.function[8][i] = true
			b.function[i][8] = true
		}
	}
	for i := 0; i < 8; i++ {
		b.function[8][b.size-1-i] = true
		b.function[b.size-1-i][8] = true
	}
	b.set(8, b.size-8, true) // the always-dark module
}

func (b *builder) drawVersion() {
	rem := b.version
	for i := 0; i < 12; i++ {
		rem = rem<<1 ^ (rem>>11)*0x1F25
	}
	bits := b.version<<12 | rem
	for i := 0; i < 18; i++ {
		dark := bits>>uint(i)&1 == 1
		a, c := b.size-11+i%3, i/3
		b.set(a, c, dark)
		b.set(c, a, dark)
	}
}

// placeData writes the codeword bits in the zigzag order, skipping function
// modules. Remainder bits stay light.
func (b *builder) placeData(codewords []byte) {
	i := 0
	for right := b.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < b.size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				upward := (right+1)&2 == 0
				y := vert
				if upward {
					y = b.size - 1 - vert
				}
				if b.function[y][x] || i >= len(codewords)*8 {
					continue
				}
				b.dark[y][x] = codewords[i>>3]>>(7-uint(i&7))&1 == 1
				i++
			}
		}
	}
}

// withMask returns a copy of the symbol with the mask and the format bits applied.
func (b *builder) withMask(mask int) [][]bool {
	grid := make([][]bool, b.size)
	for y := range grid {
		grid[y] = append([]bool(nil), b.dark[y]...)
		for x := range grid[y] {
			if !b.function[y][x] && maskBit(mask, x, y) {
				grid[y][x] = !grid[y][x]
			}
		}
	}
	// Format information: error correction level L (01) and the mask.
	data := 0b01<<3 | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = rem<<1 ^ (rem>>9)*0x537
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return bits>>uint(i)&1 == 1 }
	for i := 0; i <= 5; i++ {
		grid[i][8] = bit(i)
	}
	grid[7][8] = bit(6)
	grid[8][8] = bit(7)
	grid[8][7] = bit(8)
	for i := 9; i < 15; i++ {
		grid[8][14-i] = bit(i)
	}
	for i := 0; i < 8; i++ {
		grid[8][b.size-1-i] = bit(i)
	}
	for i := 8; i < 15; i++ {
		grid[b.size-15+i][8] = bit(i)
	}
	grid[b.size-8][8] = true
	return grid
}

func maskBit(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (x/3+y/2)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	}
	return ((x+y)%2+x*y%3)%2 == 0
}

// penalty scores a masked symbol by the four rules of the standard; the mask
// with the lowest score is used.
func penalty(g [][]bool) int {
	n := len(g)
	score := 0
	line := func(get func(i int) bool) {
		run := 1
		for i := 1; i < n; i++ {
			if get(i) == get(i-1) {
				run++
				continue
			}
			if run >= 5 {
				score += run - 2
			}
			run = 1
		}
		if run >= 5 {
			score += run - 2
		}
		// Finder-like 1:1:3:1:1 patterns with four light modules on a side.
		for i := 0; i+11 <= n; i++ {
			if finderLike(get, i, false) || finderLike(get, i, true) {
				score += 40
			}
		}
	}
	for y := 0; y < n; y++ {
		line(func(i int) bool { return g[y][i] })
	}
	for x := 0; x < n; x++ {
		line(func(i int) bool { return g[i][x] })
	}
	for y := 0; y+1 < n; y++ {
		for x := 0; x+1 < n; x++ {
			if v := g[y][x]; v == g[y][x+1] && v == g[y+1][x] && v == g[y+1][x+1] {
				score += 3
			}
		}
	}
	dark := 0
	for y := range g {
		for x := range g[y] {
			if g[y][x] {
				dark++
			}
		}
	}
	k := abs(dark*20-n*n*10) / (n * n)
	return score + k*10
}

func finderLike(get func(int) bool, i int, lightFirst bool) bool {
	pattern := [11]bool{true, false, true, true, true, false, true, false, false, false, false}
	for j := 0; j < 11; j++ {
		want := pattern[j]
		if lightFirst {
			want = pattern[10-j]
		}
		if get(i+j) != want {
			return false
		}
	}
	return true
}

// Lines renders the symbol with half-block characters, two module rows per
// text line, and a four module quiet zone. Dark modules are drawn as full or
// half blocks in the foreground color, so callers print each line in black on
// a white background (see ANSILines) for a code every scanner reads.
func (m Matrix) Lines() []string {
	const quiet = 4
	n := len(m)
	at := func(x, y int) bool {
		x -= quiet
		y -= quiet
		return x >= 0 && y >= 0 && x < n && y < n && m[y][x]
	}
	var lines []string
	total := n + 2*quiet
	for y := 0; y < total; y += 2 {
		var b strings.Builder
		for x := 0; x < total; x++ {
			switch top, bottom := at(x, y), at(x, y+1); {
			case top && bottom:
				b.WriteRune('\u2588')
			case top:
				b.WriteRune('\u2580')
			case bottom:
				b.WriteRune('\u2584')
			default:
				b.WriteRune(' ')
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

// ANSILines is Lines in black on white, whatever the terminal theme is.
func (m Matrix) ANSILines() []string {
	lines := m.Lines()
	for i, l := range lines {
		lines[i] = "\x1b[30;47m" + l + "\x1b[0m"
	}
	return lines
}
