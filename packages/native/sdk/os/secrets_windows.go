//go:build windows

package sdkos

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func platformBackend() secretBackend { return credManager{} }

var (
	advapi32     = windows.NewLazySystemDLL("advapi32.dll")
	procCredWrit = advapi32.NewProc("CredWriteW")
	procCredRead = advapi32.NewProc("CredReadW")
	procCredDel  = advapi32.NewProc("CredDeleteW")
	procCredFree = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	// maxCredBlob is CRED_MAX_CREDENTIAL_BLOB_SIZE.
	maxCredBlob = 5 * 512
)

// credential mirrors CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// credManager stores generic credentials in the Windows Credential Manager,
// one per (appId, key), named "<appId>/<key>".
type credManager struct{}

func target(service, key string) (*uint16, error) {
	return windows.UTF16PtrFromString(service + "/" + key)
}

func (credManager) set(ctx context.Context, service, key, value string) error {
	if len(value) > maxCredBlob {
		return fmt.Errorf("sdkos: Windows credentials hold at most %d bytes", maxCredBlob)
	}
	t, err := target(service, key)
	if err != nil {
		return err
	}
	user, _ := windows.UTF16PtrFromString(key)
	blob := []byte(value)
	c := credential{Type: credTypeGeneric, TargetName: t, UserName: user, Persist: credPersistLocalMachine}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
		c.CredentialBlobSize = uint32(len(blob))
	}
	r, _, e := procCredWrit.Call(uintptr(unsafe.Pointer(&c)), 0)
	if r == 0 {
		return fmt.Errorf("sdkos: CredWriteW: %w", e)
	}
	return nil
}

func (credManager) get(ctx context.Context, service, key string) (string, error) {
	t, err := target(service, key)
	if err != nil {
		return "", err
	}
	var out *credential
	r, _, e := procCredRead.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		if errors.Is(e, windows.ERROR_NOT_FOUND) {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("sdkos: CredReadW: %w", e)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(out)))
	if out.CredentialBlobSize == 0 || out.CredentialBlob == nil {
		return "", nil
	}
	return string(unsafe.Slice(out.CredentialBlob, out.CredentialBlobSize)), nil
}

func (credManager) remove(ctx context.Context, service, key string) error {
	t, err := target(service, key)
	if err != nil {
		return err
	}
	r, _, e := procCredDel.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0)
	if r == 0 {
		if errors.Is(e, windows.ERROR_NOT_FOUND) {
			return nil
		}
		return fmt.Errorf("sdkos: CredDeleteW: %w", e)
	}
	return nil
}
