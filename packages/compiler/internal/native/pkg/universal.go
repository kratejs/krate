package pkg

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// ArchUniversal is the Payload.Arch of a macOS app that holds both arm64 and
// x86_64 code. Its update target is "darwin-universal".
const ArchUniversal = "universal"

// Fat (universal) Mach-O constants. The header is big endian.
const (
	fatMagic   = 0xcafebabe
	fatAlign   = 14 // log2 of the slice alignment Apple uses for arm64 (16 KiB)
	fatArchLen = 20
)

// thinSlice is one architecture of a universal file.
type thinSlice struct {
	cpu, sub uint32
	path     string
	size     int64
}

// thinInfo reads the cpu type and subtype of a thin (single architecture)
// Mach-O file.
func thinInfo(p string) (thinSlice, error) {
	f, err := os.Open(p)
	if err != nil {
		return thinSlice{}, err
	}
	defer f.Close()
	var h [12]byte
	if _, err := io.ReadFull(f, h[:]); err != nil {
		return thinSlice{}, fmt.Errorf("%s is not a Mach-O file", p)
	}
	var order binary.ByteOrder
	switch binary.BigEndian.Uint32(h[:4]) {
	case 0xfeedfacf, 0xfeedface:
		order = binary.BigEndian
	case 0xcffaedfe, 0xcefaedfe:
		order = binary.LittleEndian
	case fatMagic:
		return thinSlice{}, fmt.Errorf("%s is already a universal binary", p)
	default:
		return thinSlice{}, fmt.Errorf("%s is not a Mach-O file", p)
	}
	st, err := f.Stat()
	if err != nil {
		return thinSlice{}, err
	}
	return thinSlice{cpu: order.Uint32(h[4:8]), sub: order.Uint32(h[8:12]), path: p, size: st.Size()}, nil
}

// cpuOrder sorts x86_64 before arm64, the order lipo writes.
func cpuOrder(cpu uint32) int {
	switch macho.Cpu(cpu) {
	case macho.CpuAmd64:
		return 0
	case macho.CpuArm64:
		return 1
	}
	return 2
}

func alignUp(n int64, a int64) int64 { return (n + a - 1) / a * a }

// WriteFat merges thin Mach-O files into one universal binary at out, in pure
// Go (what lipo -create does): a big-endian fat header, one fat_arch entry per
// slice, and each slice at an offset aligned to 2^14. Two slices of the same
// cpu type and subtype are an error.
func WriteFat(out string, thin []string) error {
	if len(thin) < 2 {
		return fmt.Errorf("a universal binary needs at least two architectures, got %d", len(thin))
	}
	slices := make([]thinSlice, 0, len(thin))
	seen := map[[2]uint32]string{}
	for _, p := range thin {
		s, err := thinInfo(p)
		if err != nil {
			return err
		}
		k := [2]uint32{s.cpu, s.sub}
		if prev, dup := seen[k]; dup {
			return fmt.Errorf("%s and %s are built for the same architecture", prev, p)
		}
		seen[k] = p
		slices = append(slices, s)
	}
	sort.SliceStable(slices, func(i, j int) bool { return cpuOrder(slices[i].cpu) < cpuOrder(slices[j].cpu) })

	align := int64(1) << fatAlign
	offsets := make([]int64, len(slices))
	off := alignUp(int64(8+fatArchLen*len(slices)), align)
	for i, s := range slices {
		offsets[i] = off
		off = alignUp(off+s.size, align)
	}
	if offsets[len(offsets)-1]+slices[len(slices)-1].size > 1<<32-1 {
		return fmt.Errorf("the slices are larger than 4 GiB, which a 32-bit fat header cannot address")
	}
	var hdr bytes.Buffer
	be := binary.BigEndian
	_ = binary.Write(&hdr, be, uint32(fatMagic))
	_ = binary.Write(&hdr, be, uint32(len(slices)))
	for i, s := range slices {
		for _, v := range []uint32{s.cpu, s.sub, uint32(offsets[i]), uint32(s.size), fatAlign} {
			_ = binary.Write(&hdr, be, v)
		}
	}
	f, err := createFile(out)
	if err != nil {
		return err
	}
	pos := int64(0)
	pad := func(to int64) error {
		for pos < to {
			n := min(to-pos, 4096)
			if _, err := f.Write(make([]byte, n)); err != nil {
				return err
			}
			pos += n
		}
		return nil
	}
	if _, err := f.Write(hdr.Bytes()); err != nil {
		f.Close()
		return err
	}
	pos = int64(hdr.Len())
	for i, s := range slices {
		if err := pad(offsets[i]); err != nil {
			f.Close()
			return err
		}
		src, err := os.Open(s.path)
		if err != nil {
			f.Close()
			return err
		}
		n, err := io.Copy(f, src)
		src.Close()
		if err != nil {
			f.Close()
			return err
		}
		pos += n
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(out, 0o755)
}

// FatArchitectures lists the GOARCH names ("amd64", "arm64") in a universal
// Mach-O file, in file order; a thin file lists its own.
func FatArchitectures(p string) ([]string, error) {
	name := func(c macho.Cpu) string {
		switch c {
		case macho.CpuAmd64:
			return "amd64"
		case macho.CpuArm64:
			return "arm64"
		}
		return c.String()
	}
	if fat, err := macho.OpenFat(p); err == nil {
		defer fat.Close()
		var out []string
		for _, a := range fat.Arches {
			out = append(out, name(a.Cpu))
		}
		return out, nil
	}
	f, err := macho.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return []string{name(f.Cpu)}, nil
}

// MergeUniversal combines the arm64 and amd64 payloads of a macOS app into a
// universal payload under work: the executable and every Mach-O file the two
// payloads share by relative path (Go host children, compiled Node, Bun and
// Deno hosts, plugin helpers) become universal files; other files must be
// identical in both. A file only one side has, or a non-Mach-O file that
// differs, is an error with a hint, because the two builds then disagree about
// what the app contains.
func MergeUniversal(arm64, amd64 Payload, work string) (Payload, error) {
	out := Payload{OS: "darwin", Arch: ArchUniversal, ExtraFiles: map[string]string{}}
	if err := os.RemoveAll(work); err != nil {
		return out, fail(diag.CodeNativePkgWrite, work, err.Error(), "")
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return out, fail(diag.CodeNativePkgWrite, work, err.Error(), "")
	}
	exe := filepath.Join(work, filepath.Base(arm64.Executable))
	if err := mergeFile(arm64.Executable, amd64.Executable, exe); err != nil {
		return out, err
	}
	out.Executable = exe

	names := map[string]bool{}
	for k := range arm64.ExtraFiles {
		names[k] = true
	}
	for k := range amd64.ExtraFiles {
		names[k] = true
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a, aok := arm64.ExtraFiles[k]
		b, bok := amd64.ExtraFiles[k]
		if !aok || !bok {
			which, other := "arm64", "amd64"
			if !aok {
				which, other = other, which
			}
			return out, fail(diag.CodeNativePkgUniversal, k, fmt.Sprintf("%q exists in the %s build but not in the %s build", k, which, other),
				"build both architectures from the same sources; a host runtime that cannot be built for one architecture has to be switched off for the universal app (see native.host.runtime)")
		}
		dst := filepath.Join(work, "x", filepath.FromSlash(k))
		if err := mergePath(a, b, dst, k); err != nil {
			return out, err
		}
		out.ExtraFiles[k] = dst
	}
	return out, nil
}

// mergePath merges two files or directory trees.
func mergePath(a, b, dst, rel string) error {
	sa, err := os.Lstat(a)
	if err != nil {
		return fail(diag.CodeNativePkgInput, a, err.Error(), "")
	}
	sb, err := os.Lstat(b)
	if err != nil {
		return fail(diag.CodeNativePkgInput, b, err.Error(), "")
	}
	switch {
	case sa.Mode()&fs.ModeSymlink != 0 || sb.Mode()&fs.ModeSymlink != 0:
		ta, _ := os.Readlink(a)
		tb, _ := os.Readlink(b)
		if sa.Mode()&sb.Mode()&fs.ModeSymlink == 0 || ta != tb {
			return fail(diag.CodeNativePkgUniversal, rel, "the arm64 and amd64 builds differ at this symlink", "build both architectures from the same sources")
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return wrapWrite(dst, err)
		}
		return wrapWrite(dst, os.Symlink(ta, dst))
	case sa.IsDir() != sb.IsDir():
		return fail(diag.CodeNativePkgUniversal, rel, "the arm64 build has a directory where the amd64 build has a file (or the reverse)", "build both architectures from the same sources")
	case sa.IsDir():
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return wrapWrite(dst, err)
		}
		names := map[string]bool{}
		for _, d := range []string{a, b} {
			es, err := os.ReadDir(d)
			if err != nil {
				return fail(diag.CodeNativePkgInput, d, err.Error(), "")
			}
			for _, e := range es {
				names[e.Name()] = true
			}
		}
		keys := make([]string, 0, len(names))
		for n := range names {
			keys = append(keys, n)
		}
		sort.Strings(keys)
		for _, n := range keys {
			pa, pb := filepath.Join(a, n), filepath.Join(b, n)
			_, ea := os.Lstat(pa)
			_, eb := os.Lstat(pb)
			if ea != nil || eb != nil {
				side := "arm64"
				if ea != nil {
					side = "amd64"
				}
				return fail(diag.CodeNativePkgUniversal, rel+"/"+n, "the "+side+" build does not have this file", "build both architectures from the same sources")
			}
			if err := mergePath(pa, pb, filepath.Join(dst, n), rel+"/"+n); err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return wrapWrite(dst, err)
	}
	return mergeFile(a, b, dst)
}

// mergeFile writes dst from the arm64 file a and the amd64 file b: a
// universal Mach-O when both are thin Mach-O files, a copy when the bytes are
// identical, an error otherwise.
func mergeFile(a, b, dst string) error {
	if isMachO(a) && isMachO(b) {
		ia, ea := thinInfo(a)
		ib, eb := thinInfo(b)
		if ea == nil && eb == nil {
			if macho.Cpu(ia.cpu) != macho.CpuArm64 || macho.Cpu(ib.cpu) != macho.CpuAmd64 {
				return fail(diag.CodeNativePkgUniversal, a, fmt.Sprintf("%s is not arm64 or %s is not x86_64", filepath.Base(a), filepath.Base(b)),
					"the arm64 directory must hold arm64 code and the amd64 directory x86_64 code")
			}
			if err := WriteFat(dst, []string{a, b}); err != nil {
				return fail(diag.CodeNativePkgUniversal, dst, err.Error(), "")
			}
			return nil
		}
		// A file that is already universal in both builds: keep arm64's copy.
	}
	da, err := os.ReadFile(a)
	if err != nil {
		return fail(diag.CodeNativePkgInput, a, err.Error(), "")
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return fail(diag.CodeNativePkgInput, b, err.Error(), "")
	}
	if !bytes.Equal(da, db) && !isMachO(a) {
		return fail(diag.CodeNativePkgUniversal, filepath.Base(a), "the arm64 and amd64 builds contain different data for this file, and it is not a Mach-O executable that could be merged",
			"build both architectures from the same sources and the same build date")
	}
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(a); err == nil {
		mode = st.Mode().Perm()
	}
	return wrapWrite(dst, os.WriteFile(dst, da, mode))
}

// DiscoverUniversal finds the arm64 and amd64 builds under the native output
// directory and merges them into work. With only one of them present the error
// says which build to run.
func DiscoverUniversal(outDir, productName, work string) (Payload, error) {
	arm, err := Discover(outDir, "darwin", "arm64", productName)
	if err != nil {
		return Payload{}, universalMissing(err, "darwin/arm64")
	}
	amd, err := Discover(outDir, "darwin", "amd64", productName)
	if err != nil {
		return Payload{}, universalMissing(err, "darwin/amd64")
	}
	return MergeUniversal(arm, amd, work)
}

func universalMissing(err error, target string) error {
	if pe, ok := err.(*Error); ok && pe.Diag.Code == diag.CodeNativePkgInput {
		return fail(diag.CodeNativePkgUniversal, pe.Diag.File, "a universal app needs a build for "+target+": "+pe.Diag.Message,
			"build both: krate native build --release --arch universal (or --target darwin/arm64,darwin/amd64)")
	}
	return err
}
