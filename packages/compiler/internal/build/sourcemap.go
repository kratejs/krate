package build

import (
	"encoding/json"
	"fmt"
	"strings"
)

// sourceMap is the JSON shape of a source map (v3). Only the fields Krate
// populates are listed.
type sourceMap struct {
	Version        int      `json:"version"`
	File           string   `json:"file"`
	SourceRoot     string   `json:"sourceRoot"`
	Sources        []string `json:"sources"`
	SourcesContent []string `json:"sourcesContent,omitempty"`
	Names          []string `json:"names"`
	Mappings       string   `json:"mappings"`
}

// generateSourcemap builds a valid line-level source map. Generated and
// original line counts are mapped 1:1 where possible; because the per-page
// hydration bundle is compiler-generated (there is no literal original
// TypeScript the lines correspond to), the *generated* code is embedded as
// `sourcesContent` so devtools can still display a coherent file rather than a
// misleading self-reference. Consumers that need true column mappings for
// esbuild-built assets get real maps directly from esbuild.
func generateSourcemap(generated, sourcePath, sourceContent string) string {
	genLines := strings.Split(generated, "\n")
	srcLines := strings.Split(sourceContent, "\n")

	var mappings strings.Builder
	for i := range genLines {
		if i > 0 {
			mappings.WriteString(";")
		}
		// Map each generated line to the same line in the source (clamped).
		line := i
		if line >= len(srcLines) {
			line = len(srcLines) - 1
		}
		if line < 0 {
			line = 0
		}
		mappings.WriteString(vlqEncode([]int{0, line, 0, 0}))
	}

	sm := sourceMap{
		Version:        3,
		File:           strings.ReplaceAll(sourcePath, "\\", "/"),
		SourceRoot:     "",
		Sources:        []string{strings.ReplaceAll(sourcePath, "\\", "/")},
		SourcesContent: []string{sourceContent},
		Names:          []string{},
		Mappings:       mappings.String(),
	}
	data, err := json.Marshal(sm)
	if err != nil {
		// Fall back to a minimal valid map rather than emitting invalid JSON.
		return `{"version":3,"file":"","sources":[],"names":[],"mappings":""}`
	}
	return string(data)
}

// appendSourceMappingURL appends a `//# sourceMappingURL=` comment pointing at
// the given map filename, unless one is already present.
func appendSourceMappingURL(js, mapFile string) string {
	if strings.Contains(js, "sourceMappingURL=") {
		return js
	}
	if !strings.HasSuffix(js, "\n") {
		js += "\n"
	}
	return js + "//# sourceMappingURL=" + mapFile + "\n"
}

// vlqEncode encodes a slice of integers using Base64 VLQ encoding.
func vlqEncode(values []int) string {
	var result strings.Builder
	for _, v := range values {
		result.WriteString(encodeVLQSegment(v))
	}
	return result.String()
}

// encodeVLQSegment encodes a single integer using Base64 VLQ.
func encodeVLQSegment(value int) string {
	// VLQ encoding: encode in 5-bit chunks, LSB first.
	// Each chunk is 5 bits, sign bit is LSB, continuation bit is bit 5.
	var v uint
	if value < 0 {
		v = uint((-value)<<1) | 1
	} else {
		v = uint(value << 1)
	}

	var result strings.Builder
	for {
		chunk := int(v) & 0x1F
		v >>= 5
		if v > 0 {
			chunk |= 0x20 // continuation bit
		}
		result.WriteByte(base64VLQ(chunk))
		if v == 0 {
			break
		}
	}
	return result.String()
}

var vlqChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func base64VLQ(v int) byte {
	if v < 0 || v >= len(vlqChars) {
		return 'A'
	}
	return vlqChars[v]
}

var _ = fmt.Sprintf
