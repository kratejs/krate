package pkg

import (
	"debug/macho"
)

// lcCodeSignature is the LC_CODE_SIGNATURE load command.
const lcCodeSignature = 0x1d

// machoHasSignature reports whether the Mach-O file at p (thin or universal)
// carries a code signature load command. The Go linker adds an ad hoc
// signature to darwin/arm64 executables.
func machoHasSignature(p string) (bool, error) {
	if fat, err := macho.OpenFat(p); err == nil {
		defer fat.Close()
		for _, a := range fat.Arches {
			if !loadsSignature(a.File) {
				return false, nil
			}
		}
		return len(fat.Arches) > 0, nil
	}
	f, err := macho.Open(p)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return loadsSignature(f), nil
}

func loadsSignature(f *macho.File) bool {
	for _, l := range f.Loads {
		raw := l.Raw()
		if len(raw) >= 4 && f.ByteOrder.Uint32(raw) == lcCodeSignature {
			return true
		}
	}
	return false
}

// arm64SliceSigned reports whether the arm64 slice of the Mach-O file p (thin
// or universal) carries a code signature; false when there is none.
func arm64SliceSigned(p string) (bool, error) {
	if fat, err := macho.OpenFat(p); err == nil {
		defer fat.Close()
		for _, a := range fat.Arches {
			if a.Cpu == macho.CpuArm64 {
				return loadsSignature(a.File), nil
			}
		}
		return false, nil
	}
	return machoHasSignature(p)
}
