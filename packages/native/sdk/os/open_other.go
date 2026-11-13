//go:build !linux

package sdkos

import "errors"

func portalOpen(string) error { return errors.New("sdkos: no OpenURI portal on this OS") }
