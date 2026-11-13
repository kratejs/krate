package plugins

import "go/format"

func formatGo(src string) ([]byte, error) { return format.Source([]byte(src)) }
