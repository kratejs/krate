//go:build darwin

package sdkos

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
)

func platformBackend() secretBackend { return &keychain{} }

// OSStatus values used here.
const (
	errSecSuccess       = 0
	errSecItemNotFound  = -25300
	errSecDuplicateItem = -25299

	kCFStringEncodingUTF8 = 0x08000100
)

// keychain stores generic passwords in the login keychain through
// Security.framework, called with purego (no cgo).
type keychain struct {
	once sync.Once
	err  error

	cfStringCreate func(alloc uintptr, cstr *byte, enc uint32) uintptr
	cfDataCreate   func(alloc uintptr, b *byte, n int) uintptr
	cfDictCreate   func(alloc uintptr, capacity int, keyCB, valCB uintptr) uintptr
	cfDictSet      func(d, k, v uintptr)
	cfRelease      func(o uintptr)
	cfDataLen      func(d uintptr) int
	cfDataPtr      func(d uintptr) unsafe.Pointer

	secAdd    func(attrs uintptr, result *uintptr) int32
	secCopy   func(query uintptr, result *uintptr) int32
	secDelete func(query uintptr) int32
	secUpdate func(query, attrs uintptr) int32

	kClass, kClassGeneric, kService, kAccount, kValueData uintptr
	kReturnData, kMatchLimit, kMatchLimitOne, kTrue       uintptr
	keyCB, valCB                                          uintptr
}

// deref reads the pointer-sized value stored at the data symbol addr.
func deref(addr uintptr) uintptr {
	p := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	return *(*uintptr)(p)
}

func (k *keychain) load() error {
	k.once.Do(func() {
		const rtld = purego.RTLD_NOW | purego.RTLD_GLOBAL
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", rtld)
		if err != nil {
			k.err = err
			return
		}
		sec, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", rtld)
		if err != nil {
			k.err = err
			return
		}
		purego.RegisterLibFunc(&k.cfStringCreate, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&k.cfDataCreate, cf, "CFDataCreate")
		purego.RegisterLibFunc(&k.cfDictCreate, cf, "CFDictionaryCreateMutable")
		purego.RegisterLibFunc(&k.cfDictSet, cf, "CFDictionarySetValue")
		purego.RegisterLibFunc(&k.cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&k.cfDataLen, cf, "CFDataGetLength")
		purego.RegisterLibFunc(&k.cfDataPtr, cf, "CFDataGetBytePtr")
		purego.RegisterLibFunc(&k.secAdd, sec, "SecItemAdd")
		purego.RegisterLibFunc(&k.secCopy, sec, "SecItemCopyMatching")
		purego.RegisterLibFunc(&k.secDelete, sec, "SecItemDelete")
		purego.RegisterLibFunc(&k.secUpdate, sec, "SecItemUpdate")

		sym := func(lib uintptr, name string) uintptr {
			a, e := purego.Dlsym(lib, name)
			if e != nil && k.err == nil {
				k.err = e
			}
			return a
		}
		k.keyCB = sym(cf, "kCFTypeDictionaryKeyCallBacks") // the struct's address
		k.valCB = sym(cf, "kCFTypeDictionaryValueCallBacks")
		k.kTrue = deref(sym(cf, "kCFBooleanTrue"))
		k.kClass = deref(sym(sec, "kSecClass"))
		k.kClassGeneric = deref(sym(sec, "kSecClassGenericPassword"))
		k.kService = deref(sym(sec, "kSecAttrService"))
		k.kAccount = deref(sym(sec, "kSecAttrAccount"))
		k.kValueData = deref(sym(sec, "kSecValueData"))
		k.kReturnData = deref(sym(sec, "kSecReturnData"))
		k.kMatchLimit = deref(sym(sec, "kSecMatchLimit"))
		k.kMatchLimitOne = deref(sym(sec, "kSecMatchLimitOne"))
	})
	return k.err
}

// refs collects CF objects to release.
type refs struct {
	k    *keychain
	objs []uintptr
}

func (r *refs) add(o uintptr) uintptr { r.objs = append(r.objs, o); return o }

func (r *refs) release() {
	for _, o := range r.objs {
		if o != 0 {
			r.k.cfRelease(o)
		}
	}
}

func (r *refs) str(s string) uintptr {
	b := append([]byte(s), 0)
	return r.add(r.k.cfStringCreate(0, &b[0], kCFStringEncodingUTF8))
}

func (r *refs) data(b []byte) uintptr {
	var p *byte
	if len(b) > 0 {
		p = &b[0]
	}
	return r.add(r.k.cfDataCreate(0, p, len(b)))
}

func (r *refs) dict() uintptr { return r.add(r.k.cfDictCreate(0, 0, r.k.keyCB, r.k.valCB)) }

// query builds the identity of one secret.
func (r *refs) query(service, key string) uintptr {
	d := r.dict()
	r.k.cfDictSet(d, r.k.kClass, r.k.kClassGeneric)
	r.k.cfDictSet(d, r.k.kService, r.str(service))
	r.k.cfDictSet(d, r.k.kAccount, r.str(key))
	return d
}

func status(op string, st int32) error {
	if st == errSecSuccess {
		return nil
	}
	return fmt.Errorf("sdkos: keychain %s failed (OSStatus %d)", op, st)
}

func (k *keychain) set(ctx context.Context, service, key, value string) error {
	if err := k.load(); err != nil {
		return fmt.Errorf("sdkos: loading Security.framework (%v): %w", err, krate.ErrUnsupported)
	}
	r := &refs{k: k}
	defer r.release()
	val := r.data([]byte(value))

	add := r.query(service, key)
	k.cfDictSet(add, k.kValueData, val)
	st := k.secAdd(add, nil)
	if st == errSecDuplicateItem {
		upd := r.dict()
		k.cfDictSet(upd, k.kValueData, val)
		st = k.secUpdate(r.query(service, key), upd)
	}
	return status("write", st)
}

func (k *keychain) get(ctx context.Context, service, key string) (string, error) {
	if err := k.load(); err != nil {
		return "", fmt.Errorf("sdkos: loading Security.framework (%v): %w", err, krate.ErrUnsupported)
	}
	r := &refs{k: k}
	defer r.release()
	q := r.query(service, key)
	k.cfDictSet(q, k.kReturnData, k.kTrue)
	k.cfDictSet(q, k.kMatchLimit, k.kMatchLimitOne)
	var res uintptr
	st := k.secCopy(q, &res)
	if st == errSecItemNotFound {
		return "", ErrSecretNotFound
	}
	if err := status("read", st); err != nil {
		return "", err
	}
	if res == 0 {
		return "", errors.New("sdkos: keychain returned no data")
	}
	r.add(res)
	n := k.cfDataLen(res)
	if n == 0 {
		return "", nil
	}
	return string(unsafe.Slice((*byte)(k.cfDataPtr(res)), n)), nil
}

func (k *keychain) remove(ctx context.Context, service, key string) error {
	if err := k.load(); err != nil {
		return fmt.Errorf("sdkos: loading Security.framework (%v): %w", err, krate.ErrUnsupported)
	}
	r := &refs{k: k}
	defer r.release()
	st := k.secDelete(r.query(service, key))
	if st == errSecItemNotFound {
		return nil
	}
	return status("delete", st)
}
