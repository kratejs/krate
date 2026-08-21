package build

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// generateSourcemap produces a basic inline sourcemap in JSON format.
// It maps lines in the minified output back to lines in the original source.
// This is a line-level mapping (not character-precise VLQ).
func generateSourcemap(minified, original, filename string) string {
	minLines := strings.Split(minified, "\n")
	origLines := strings.Split(original, "\n")

	var mappings strings.Builder
	for i := range minLines {
		if i > 0 {
			mappings.WriteString(";")
		}
		if i < len(origLines) {
			mappings.WriteString(vlqEncode([]int{0, i, 0, 0}))
		}
	}

	// Use forward slashes in paths for cross-platform compatibility
	filePath := strings.ReplaceAll(filename, "\\", "/")

	sm := fmt.Sprintf(`{
  "version":3,
  "file":"%s",
  "sourceRoot":"",
  "sources":["%s"],
  "names":[],
  "mappings":"%s"
}`, filePath, filePath, mappings.String())

	return sm
}

// GenerateInlineSourcemap creates a data URL with the base64-encoded sourcemap
// that can be appended to a JS file.
func generateInlineSourcemap(minified, original, filename string) string {
	sm := generateSourcemap(minified, original, filename)
	encoded := base64.StdEncoding.EncodeToString([]byte(sm))
	return "//# sourceMappingURL=data:application/json;base64," + encoded
}

// vlqEncode encodes a slice of integers using Base64 VLQ encoding.
// This is a simplified implementation for line-level mappings.
func vlqEncode(values []int) string {
	var result strings.Builder
	for _, v := range values {
		result.WriteString(encodeVLQSegment(v))
	}
	return result.String()
}

// encodeVLQSegment encodes a single integer using Base64 VLQ.
func encodeVLQSegment(value int) string {
	// VLQ encoding: encode in 5-bit chunks, LSB first
	// Each chunk is 5 bits, sign bit is LSB, continuation bit is bit 5

	// Make room for sign bit
	var v uint
	if value < 0 {
		v = uint((-value) << 1) | 1
	} else {
		v = uint(value << 1)
	}

	var result strings.Builder
	for {
		// Take bottom 5 bits
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
