//go:build !linux && !darwin && !windows

package sdkos

import "os"

func osVersion() string    { return "" }
func systemLocale() string { return localeFromEnv(os.Getenv) }
