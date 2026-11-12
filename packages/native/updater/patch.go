package updater

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Delta updates.
//
// A patch turns the files an installed version holds into the files of the
// next version. It is one file (extension .krpatch):
//
//	"KRPATCH1" | uint32 BE manifest length | manifest JSON | data blobs
//
// The manifest lists every entry of the new tree (directories, symlinks and
// files). A file is "same" (unchanged: copied from the installed file), "diff"
// (a bsdiff of the installed file) or "add" (deflated bytes). Every file
// carries the sha512 of its final content and, for same and diff, the sha512
// of the installed file it is made from, so a patch only applies to the exact
// version it was built for. The whole patch file is signed (PatchMeta); the
// client verifies that signature first, then every file hash while it
// builds the new tree in a staging directory. Any failure discards the
// staging directory and the caller downloads the full artifact instead.

// PatchMagic starts every patch file.
const PatchMagic = "KRPATCH1"

// PatchMessagePrefix starts the signed message of a patch.
const PatchMessagePrefix = "krate-patch-v1"

// Update kinds: how an artifact maps onto the installed app.
const (
	// KindTree is Linux and Windows: the entries replace the entries of the
	// install directory.
	KindTree = "tree"
	// KindBundle is macOS: the tree is the .app directory.
	KindBundle = "bundle"
	// KindFile is a single file such as an AppImage.
	KindFile = "file"
)

const (
	maxPatchManifest = 64 << 20
	maxPatchEntries  = 500000
	maxPatchFile     = 1 << 30
)

// PatchMeta is what a patch signature binds the patch bytes to.
type PatchMeta struct {
	AppID, Target string
	// From is the installed version the patch applies to, To the version it
	// produces; both exactly as the feed writes them.
	From, To string
}

func (m PatchMeta) message(sum []byte, size int64) ([]byte, error) {
	for _, f := range []string{m.AppID, m.Target, m.From, m.To} {
		if f == "" || strings.ContainsAny(f, "\r\n") {
			return nil, errors.New("updater: appId, target and versions must be non-empty single-line strings")
		}
	}
	return []byte(PatchMessagePrefix + "\n" + m.AppID + "\n" + m.Target + "\n" + m.From + "\n" + m.To + "\n" +
		hex.EncodeToString(sum) + "\n" + strconv.FormatInt(size, 10)), nil
}

// SignPatch signs the patch data read from r.
func SignPatch(priv ed25519.PrivateKey, m PatchMeta, r io.Reader) (string, error) {
	sum, size, err := digest(r)
	if err != nil {
		return "", err
	}
	msg, err := m.message(sum, size)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg)), nil
}

// VerifyPatch checks a patch signature; any mismatch yields ErrBadSignature.
func VerifyPatch(pub ed25519.PublicKey, m PatchMeta, r io.Reader, signature string) error {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	sum, size, err := digest(r)
	if err != nil {
		return err
	}
	msg, err := m.message(sum, size)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadSignature, err)
	}
	if !ed25519.Verify(pub, msg, sig) {
		return ErrBadSignature
	}
	return nil
}

type patchManifest struct {
	V       int          `json:"v"`
	Kind    string       `json:"kind"`
	From    string       `json:"from"`
	To      string       `json:"to"`
	Entries []patchEntry `json:"entries"`
}

type patchEntry struct {
	Path string `json:"p"`
	Type string `json:"t"` // "f" file, "d" directory, "l" symlink
	Mode uint32 `json:"m,omitempty"`
	Size int64  `json:"n,omitempty"`
	Hash string `json:"h,omitempty"` // sha512 of the final file
	Op   string `json:"o,omitempty"` // "same", "diff", "add"
	Base string `json:"b,omitempty"` // sha512 of the installed file
	// From is the installed path of the base when it differs from Path (a
	// renamed file).
	From string `json:"f,omitempty"`
	Link string `json:"l,omitempty"`
	Off  int64  `json:"off,omitempty"`
	Len  int64  `json:"len,omitempty"`
}

// treeEntry is one item of a materialized tree.
type treeEntry struct {
	rel  string // slash separated, "" for a single file
	typ  string
	mode fs.FileMode
	size int64
	hash string
	link string
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	sum, n, err := digest(f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(sum), n, nil
}

// listTree describes the tree below root (a directory), or the single file
// root itself.
func listTree(root string) ([]treeEntry, error) {
	st, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("updater: %s is not a regular file", root)
		}
		h, n, err := hashFile(root)
		if err != nil {
			return nil, err
		}
		return []treeEntry{{rel: "", typ: "f", mode: st.Mode().Perm(), size: n, hash: h}}, nil
	}
	var out []treeEntry
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch m := info.Mode(); {
		case m.IsDir():
			out = append(out, treeEntry{rel: rel, typ: "d", mode: m.Perm()})
		case m&fs.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out = append(out, treeEntry{rel: rel, typ: "l", link: filepath.ToSlash(l)})
		case m.IsRegular():
			h, n, err := hashFile(p)
			if err != nil {
				return err
			}
			out = append(out, treeEntry{rel: rel, typ: "f", mode: m.Perm(), size: n, hash: h})
		default:
			return fmt.Errorf("updater: %s is a special file", rel)
		}
		return nil
	})
	return out, err
}

// unwrapTree steps into the single wrapper directory (name-1.2.0/...) that
// holds the executable and returns the directory whose entries replace the
// install directory.
func unwrapTree(fsys FS, stage, exeName string) (string, []fs.DirEntry, error) {
	entries, err := fsys.ReadDir(stage)
	if err != nil {
		return "", nil, err
	}
	if len(entries) == 1 && entries[0].IsDir() && entries[0].Name() != "app" {
		if _, err := fsys.Stat(filepath.Join(stage, entries[0].Name(), exeName)); err == nil {
			stage = filepath.Join(stage, entries[0].Name())
			if entries, err = fsys.ReadDir(stage); err != nil {
				return "", nil, err
			}
		}
	}
	return stage, entries, nil
}

// findBundle returns the .app directory of an unpacked macOS artifact.
func findBundle(fsys FS, stage, name string) (string, error) {
	if name != "" {
		next := filepath.Join(stage, name)
		if st, err := fsys.Stat(next); err == nil && st.IsDir() {
			return next, nil
		}
	}
	entries, _ := fsys.ReadDir(stage)
	next := ""
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			if next != "" {
				return "", errors.New("updater: the update contains more than one .app bundle")
			}
			next = filepath.Join(stage, e.Name())
		}
	}
	if next == "" {
		return "", fmt.Errorf("updater: the update does not contain %s", name)
	}
	return next, nil
}

// PatchOptions describe the patch GeneratePatch writes.
type PatchOptions struct {
	// Kind is KindTree, KindBundle or KindFile.
	Kind string
	// ExeName is the executable's file name (needed to find the wrapper
	// directory of a Windows zip).
	ExeName string
	Meta    PatchMeta
	Key     ed25519.PrivateKey
}

// PatchInfo describes a written patch.
type PatchInfo struct {
	Path      string
	Signature string
	SHA512    string
	Size      int64
	// NewSize is the size of the full artifact the patch replaces.
	NewSize int64
}

// materialize unpacks an artifact and returns the root of its tree.
func materialize(artifact, kind, exeName string) (root string, cleanup func(), err error) {
	if !isArchive(artifact) {
		if kind != KindFile {
			return "", nil, fmt.Errorf("updater: %s is not an archive", filepath.Base(artifact))
		}
		return artifact, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "krate-patch-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	fsys := OSFS{}
	if err := extractArchive(fsys, artifact, tmp); err != nil {
		cleanup()
		return "", nil, err
	}
	switch kind {
	case KindTree:
		r, _, err := unwrapTree(fsys, tmp, exeName)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		return r, cleanup, nil
	case KindBundle:
		r, err := findBundle(fsys, tmp, "")
		if err != nil {
			cleanup()
			return "", nil, err
		}
		return r, cleanup, nil
	}
	entries, err := fsys.ReadDir(tmp)
	if err != nil || len(entries) != 1 || entries[0].IsDir() {
		cleanup()
		return "", nil, errors.New("updater: an archive update of a single file must hold exactly one file")
	}
	return filepath.Join(tmp, entries[0].Name()), cleanup, nil
}

func readCapped(p string, limit int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("updater: %s is too large for a delta update", filepath.Base(p))
	}
	return b, nil
}

// GeneratePatch writes the signed patch that turns the files of oldArtifact
// into those of newArtifact (both update artifacts of the same kind). The
// patch is written to outPath.
func GeneratePatch(oldArtifact, newArtifact, outPath string, o PatchOptions) (*PatchInfo, error) {
	if o.Key == nil {
		return nil, errors.New("updater: GeneratePatch needs a signing key")
	}
	if o.Kind != KindTree && o.Kind != KindBundle && o.Kind != KindFile {
		return nil, fmt.Errorf("updater: unknown update kind %q", o.Kind)
	}
	oldRoot, cleanOld, err := materialize(oldArtifact, o.Kind, o.ExeName)
	if err != nil {
		return nil, err
	}
	defer cleanOld()
	newRoot, cleanNew, err := materialize(newArtifact, o.Kind, o.ExeName)
	if err != nil {
		return nil, err
	}
	defer cleanNew()
	oldList, err := listTree(oldRoot)
	if err != nil {
		return nil, err
	}
	newList, err := listTree(newRoot)
	if err != nil {
		return nil, err
	}
	oldBy := map[string]treeEntry{}
	oldByHash := map[string]string{}
	for _, e := range oldList {
		oldBy[e.rel] = e
		if e.typ == "f" && e.size > 0 {
			if _, ok := oldByHash[e.hash]; !ok {
				oldByHash[e.hash] = e.rel
			}
		}
	}

	blobPath := outPath + ".blobs"
	blobs, err := os.Create(blobPath)
	if err != nil {
		return nil, err
	}
	defer os.Remove(blobPath)
	var off int64
	man := patchManifest{V: 1, Kind: o.Kind, From: o.Meta.From, To: o.Meta.To}
	writeBlob := func(b []byte) (int64, int64, error) {
		start := off
		n, err := blobs.Write(b)
		off += int64(n)
		return start, int64(n), err
	}
	for _, e := range newList {
		pe := patchEntry{Path: e.rel, Type: e.typ, Mode: uint32(e.mode)}
		switch e.typ {
		case "l":
			pe.Link = e.link
		case "f":
			pe.Size, pe.Hash = e.size, e.hash
			var nwPath string
			if o.Kind == KindFile && e.rel == "" {
				nwPath = newRoot
			} else {
				nwPath = filepath.Join(newRoot, filepath.FromSlash(e.rel))
			}
			old, hadOld := oldBy[e.rel]
			if hadOld && old.typ == "f" && old.hash == e.hash {
				pe.Op, pe.Base = "same", old.hash
				break
			}
			if from, ok := oldByHash[e.hash]; ok && o.Kind != KindFile {
				// Unchanged content under another name (hashed asset names).
				pe.Op, pe.Base, pe.From = "same", e.hash, from
				break
			}
			if e.size > maxPatchFile {
				return nil, fmt.Errorf("updater: %s is too large for a delta update", e.rel)
			}
			nw, err := readCapped(nwPath, maxPatchFile)
			if err != nil {
				return nil, err
			}
			addBlob, err := deflateBytes(nw)
			if err != nil {
				return nil, err
			}
			best, op := addBlob, "add"
			if hadOld && old.typ == "f" && old.size <= maxDiffBase && e.size > 0 {
				var oldPath string
				if o.Kind == KindFile && e.rel == "" {
					oldPath = oldRoot
				} else {
					oldPath = filepath.Join(oldRoot, filepath.FromSlash(e.rel))
				}
				ob, err := readCapped(oldPath, maxDiffBase)
				if err != nil {
					return nil, err
				}
				if db, err := diffBlob(ob, nw); err == nil && len(db) < len(addBlob) {
					best, op = db, "diff"
					pe.Base = old.hash
				}
			}
			pe.Op = op
			if pe.Off, pe.Len, err = writeBlob(best); err != nil {
				return nil, err
			}
		}
		man.Entries = append(man.Entries, pe)
	}
	if err := blobs.Close(); err != nil {
		return nil, err
	}
	manJSON, err := json.Marshal(man)
	if err != nil {
		return nil, err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return nil, err
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(manJSON)))
	_, err = out.Write(append(append([]byte(PatchMagic), lenBuf[:]...), manJSON...))
	if err == nil {
		var in *os.File
		if in, err = os.Open(blobPath); err == nil {
			_, err = io.Copy(out, in)
			in.Close()
		}
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(outPath)
		return nil, err
	}
	f, err := os.Open(outPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sum, size, err := digest(f)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	sig, err := SignPatch(o.Key, o.Meta, f)
	if err != nil {
		return nil, err
	}
	info := &PatchInfo{Path: outPath, Signature: sig, SHA512: hex.EncodeToString(sum), Size: size}
	if st, err := os.Stat(newArtifact); err == nil {
		info.NewSize = st.Size()
	}
	return info, nil
}

// readManifest parses the header of a patch file.
func readManifest(f *os.File) (*patchManifest, int64, error) {
	var head [len(PatchMagic) + 4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil || string(head[:len(PatchMagic)]) != PatchMagic {
		return nil, 0, errors.New("updater: not a Krate patch file")
	}
	n := binary.BigEndian.Uint32(head[len(PatchMagic):])
	if n == 0 || n > maxPatchManifest {
		return nil, 0, errors.New("updater: patch manifest has an unreasonable size")
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(f, raw); err != nil {
		return nil, 0, errors.New("updater: patch is truncated")
	}
	var m patchManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, 0, fmt.Errorf("updater: patch manifest is damaged: %w", err)
	}
	if m.V != 1 {
		return nil, 0, fmt.Errorf("updater: patch format %d is not supported", m.V)
	}
	if len(m.Entries) > maxPatchEntries {
		return nil, 0, errors.New("updater: patch lists too many entries")
	}
	return &m, int64(len(head)) + int64(n), nil
}

// buildPatched creates the new tree in stage from the patch file and the
// installed files under baseRoot (a directory, or the file itself for
// KindFile). dstName names the single file of KindFile and the bundle of
// KindBundle inside stage. stage must not exist.
func buildPatched(fsys FS, patchPath, kind, baseRoot, stage, dstName, from, to string) error {
	f, err := os.Open(patchPath)
	if err != nil {
		return err
	}
	defer f.Close()
	man, dataStart, err := readManifest(f)
	if err != nil {
		return err
	}
	if man.Kind != kind {
		return fmt.Errorf("updater: the patch is for a %s update, this install needs %s", man.Kind, kind)
	}
	if man.From != from || man.To != to {
		return fmt.Errorf("updater: the patch goes from %s to %s, want %s to %s", man.From, man.To, from, to)
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	dataLen := st.Size() - dataStart

	root := stage
	switch kind {
	case KindBundle:
		root = filepath.Join(stage, dstName)
	case KindFile:
		if len(man.Entries) != 1 || man.Entries[0].Path != "" || man.Entries[0].Type != "f" {
			return errors.New("updater: the patch of a single file must hold exactly one file")
		}
		if err := fsys.MkdirAll(stage, 0o755); err != nil {
			return err
		}
		return patchFile(fsys, f, dataStart, dataLen, &man.Entries[0], baseRoot, filepath.Join(stage, dstName))
	}
	if err := fsys.MkdirAll(root, 0o755); err != nil {
		return err
	}
	w := &writer{fs: fsys, root: root}
	seen := map[string]bool{}
	var total int64
	for i := range man.Entries {
		e := &man.Entries[i]
		clean := path.Clean(e.Path)
		if e.Path == "" || clean != e.Path || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") ||
			strings.ContainsAny(e.Path, "\\\x00") || seen[clean] {
			return fmt.Errorf("updater: patch entry %q is not a clean relative path", e.Path)
		}
		seen[clean] = true
		switch e.Type {
		case "d":
			if err := w.dir(e.Path, fs.FileMode(e.Mode)); err != nil {
				return err
			}
		case "l":
			if err := w.symlink(e.Path, e.Link); err != nil {
				return err
			}
		case "f":
			if total += e.Size; total > maxExtract || e.Size < 0 {
				return errors.New("updater: patch expands to too many bytes")
			}
			p, err := safeJoin(root, e.Path)
			if err != nil {
				return err
			}
			if err := w.noLinks(p, true); err != nil {
				return err
			}
			if err := fsys.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			baseRel := e.Path
			if e.From != "" {
				c := path.Clean(e.From)
				if c != e.From || strings.HasPrefix(c, "/") || c == ".." || strings.HasPrefix(c, "../") || strings.ContainsAny(e.From, "\\\x00") {
					return fmt.Errorf("updater: patch entry %q has an unsafe base path %q", e.Path, e.From)
				}
				baseRel = e.From
			}
			base := filepath.Join(baseRoot, filepath.FromSlash(baseRel))
			if err := patchFile(fsys, f, dataStart, dataLen, e, base, p); err != nil {
				return fmt.Errorf("updater: %s: %w", e.Path, err)
			}
		default:
			return fmt.Errorf("updater: patch entry %q has unknown type %q", e.Path, e.Type)
		}
	}
	return nil
}

// patchFile writes one file of the new tree to dst and checks its hash.
func patchFile(fsys FS, patch *os.File, dataStart, dataLen int64, e *patchEntry, base, dst string) error {
	mode := fs.FileMode(e.Mode).Perm()
	if e.Size < 0 || e.Size > 1<<40 {
		return errors.New("bad size")
	}
	write := func(r io.Reader) error {
		out, err := fsys.Create(dst, mode|0o200)
		if err != nil {
			return err
		}
		h := sha512.New()
		n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(r, e.Size+1))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if n != e.Size || hex.EncodeToString(h.Sum(nil)) != e.Hash {
			return errors.New("the patched file does not match its hash")
		}
		return fsys.Chmod(dst, mode)
	}
	switch e.Op {
	case "same":
		if e.Base != e.Hash {
			return errors.New("bad same entry")
		}
		st, err := os.Lstat(base)
		if err != nil {
			return fmt.Errorf("the installed file is missing: %w", err)
		}
		if !st.Mode().IsRegular() {
			return errors.New("the installed file is not a regular file")
		}
		in, err := os.Open(base)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := write(in); err != nil {
			return fmt.Errorf("the installed file does not match the version the patch is for: %w", err)
		}
		return nil
	case "add", "diff":
		if e.Off < 0 || e.Len < 0 || e.Off > dataLen || e.Len > dataLen-e.Off {
			return errors.New("blob lies outside the patch")
		}
		blob := make([]byte, e.Len)
		if _, err := patch.ReadAt(blob, dataStart+e.Off); err != nil {
			return err
		}
		if e.Op == "add" {
			raw, err := inflateLimited(blob, e.Size)
			if err != nil {
				return err
			}
			return write(bytes.NewReader(raw))
		}
		st, err := os.Lstat(base)
		if err != nil {
			return fmt.Errorf("the installed file is missing: %w", err)
		}
		if !st.Mode().IsRegular() || st.Size() > maxDiffBase {
			return errors.New("the installed file cannot be used as a base")
		}
		old, err := readCapped(base, maxDiffBase)
		if err != nil {
			return err
		}
		sum := sha512.Sum512(old)
		if hex.EncodeToString(sum[:]) != e.Base {
			return errors.New("the installed file does not match the version the patch is for")
		}
		nw, err := applyDiffBlob(old, blob, int(e.Size))
		if err != nil {
			return err
		}
		return write(bytes.NewReader(nw))
	}
	return fmt.Errorf("unknown operation %q", e.Op)
}
