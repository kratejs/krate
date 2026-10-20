package mcp

import "context"

type progressKey struct{}

type progressFn func(progress, total float64, message string)

// reportProgress emits notifications/progress when the client supplied a
// progressToken. It is a no-op for clients that did not request progress.
func reportProgress(ctx context.Context, progress, total float64, message string) {
	if ctx == nil {
		return
	}
	fn, ok := ctx.Value(progressKey{}).(progressFn)
	if !ok || fn == nil {
		return
	}
	fn(progress, total, message)
}
