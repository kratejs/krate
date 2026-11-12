package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// blob returns deterministic binary-looking bytes.
func blob(seed int64, n int) string {
	rng := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Intn(24))
	}
	return string(b)
}

// editBlob changes a few spots and inserts bytes, like a recompiled binary.
func editBlob(s string) string {
	b := []byte(s)
	for i := 100; i < len(b); i += 997 {
		b[i] ^= 0x55
	}
	at := len(b) / 3
	return string(b[:at]) + "NEWCODE-NEWCODE-NEWCODE" + string(b[at:])
}

type deltaFix struct {
	t          *testing.T
	kind       string
	cfg        Config
	srv        *httptest.Server
	key        ed25519.PrivateKey
	oldFiles   map[string]string
	newFiles   map[string]string
	newFull    []byte
	patchBytes []byte
	patchSig   string
	patchFrom  string
	fullHits   atomic.Int32
	patchHits  atomic.Int32
	installDir string // tree: install dir; bundle: parent of the .app; file: dir of the AppImage
	target     string // tree: dir; bundle: .app path; file: the AppImage
	rollout    *Rollout
	critical   bool
	minVersion string
}

func (f *deltaFix) files(version string) map[string]string {
	exe := blob(1, 6000)
	assets := blob(2, 3000)
	m := map[string]string{"acme": exe, "app/": "", "app/app.json": `{"version":"` + version + `"}`, "app/assets.bin": assets, "keep.txt": "same"}
	if version == "1.1.0" {
		m["acme"] = editBlob(exe)
		m["app/new.txt"] = "brand new file"
		delete(m, "app/assets.bin")
		m["app/assets2.bin"] = assets
	}
	switch f.kind {
	case KindBundle:
		out := map[string]string{}
		for k, v := range m {
			out["Acme.app/Contents/"+k] = v
		}
		return out
	case KindFile:
		return map[string]string{"": exe + version + blob(5, 2000)}
	}
	return m
}

func newDeltaFix(t *testing.T, kind string) *deltaFix {
	t.Helper()
	f := &deltaFix{t: t, kind: kind, patchFrom: "1.0.0"}
	priv, pub := mustKeys(t)
	f.key = priv
	f.oldFiles, f.newFiles = f.files("1.0.0"), f.files("1.1.0")
	work := t.TempDir()
	var oldArt, newArt string
	ext := ".tar.gz"
	pack := func(files map[string]string) []byte { return tgz(t, files) }
	switch kind {
	case KindBundle:
		ext = ".zip"
		pack = func(files map[string]string) []byte {
			clean := map[string]string{}
			for k, v := range files {
				if !strings.HasSuffix(k, "/") {
					clean[k] = v
				}
			}
			return zipOf(t, clean, map[string]string{"Acme.app/Contents/link": "app/app.json"})
		}
	case KindFile:
		ext = ".AppImage"
		pack = func(files map[string]string) []byte { return []byte(files[""]) }
	}
	oldArt = filepath.Join(work, "acme-1.0.0-linux-amd64"+ext)
	newArt = filepath.Join(work, "acme-1.1.0-linux-amd64"+ext)
	if err := os.WriteFile(oldArt, pack(f.oldFiles), 0o644); err != nil {
		t.Fatal(err)
	}
	f.newFull = pack(f.newFiles)
	if err := os.WriteFile(newArt, f.newFull, 0o644); err != nil {
		t.Fatal(err)
	}
	exeName := "acme"
	meta := PatchMeta{AppID: "com.acme.app", Target: "linux-amd64", From: "1.0.0", To: "1.1.0"}
	info, err := GeneratePatch(oldArt, newArt, filepath.Join(work, "p.krpatch"), PatchOptions{Kind: kind, ExeName: exeName, Meta: meta, Key: priv})
	if err != nil {
		t.Fatal(err)
	}
	f.patchBytes, _ = os.ReadFile(info.Path)
	f.patchSig = info.Signature

	// Install the old version.
	root := t.TempDir()
	switch kind {
	case KindTree:
		f.installDir, f.target = root, root
		if err := extractArchive(OSFS{}, oldArt, root); err != nil {
			t.Fatal(err)
		}
	case KindBundle:
		f.installDir, f.target = root, filepath.Join(root, "Acme.app")
		if err := extractArchive(OSFS{}, oldArt, root); err != nil {
			t.Fatal(err)
		}
	case KindFile:
		f.installDir, f.target = root, filepath.Join(root, "Acme.AppImage")
		if err := os.WriteFile(f.target, []byte(f.oldFiles[""]), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		fe := Feed{Version: "1.1.0", URL: f.srv.URL + "/full/acme-1.1.0-linux-amd64" + ext, Size: int64(len(f.newFull)),
			Signature: signAs(t, priv, Meta{AppID: "com.acme.app", Target: "linux-amd64", Version: "1.1.0"}, f.newFull),
			Rollout:   f.rollout, Critical: f.critical, MinVersion: f.minVersion}
		sum := sha512.Sum512(f.patchBytes)
		fe.Patches = []FeedPatch{{From: f.patchFrom, URL: "/patch/p.krpatch", Signature: f.patchSig, Size: int64(len(f.patchBytes)), SHA512: hex.EncodeToString(sum[:])}}
		if err := json.NewEncoder(w).Encode(fe); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	mux.HandleFunc("/full/", func(w http.ResponseWriter, r *http.Request) {
		f.fullHits.Add(1)
		if _, err := w.Write(f.newFull); err != nil {
			t.Errorf("write: %v", err)
		}
	})
	mux.HandleFunc("/patch/", func(w http.ResponseWriter, r *http.Request) {
		f.patchHits.Add(1)
		if _, err := w.Write(f.patchBytes); err != nil {
			t.Errorf("write: %v", err)
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	exe := filepath.Join(f.target, "acme")
	if kind == KindBundle {
		exe = filepath.Join(f.target, "Contents", "acme")
	}
	if kind == KindFile {
		exe = filepath.Join(f.installDir, "inner")
	}
	f.cfg = Config{
		URL: f.srv.URL + "/feed", AppID: "com.acme.app", PublicKey: pub, Version: "1.0.0", Target: "linux-amd64",
		CacheDir: t.TempDir(), StateDir: t.TempDir(), Insecure: true, Exe: exe, GOOS: "linux",
	}
	switch kind {
	case KindBundle:
		f.cfg.GOOS = "darwin"
	case KindFile:
		f.cfg.AppImage = f.target
	}
	return f
}

// assertInstalled checks the installed tree equals version 1.1.0.
func (f *deltaFix) assertInstalled() {
	f.t.Helper()
	switch f.kind {
	case KindFile:
		got, err := os.ReadFile(f.target)
		if err != nil || string(got) != f.newFiles[""] {
			f.t.Fatalf("AppImage not updated: %v", err)
		}
		return
	case KindTree:
		for name, body := range f.newFiles {
			if strings.HasSuffix(name, "/") {
				continue
			}
			if got := readFile(f.t, filepath.Join(f.target, name)); got != body {
				f.t.Fatalf("%s differs after the update", name)
			}
		}
		if _, err := os.Stat(filepath.Join(f.target, "app", "assets.bin")); err == nil {
			f.t.Fatal("removed file survived")
		}
	case KindBundle:
		for name, body := range f.newFiles {
			if strings.HasSuffix(name, "/") {
				continue
			}
			if got := readFile(f.t, filepath.Join(f.installDir, name)); got != body {
				f.t.Fatalf("%s differs after the update", name)
			}
		}
		if l, err := os.Readlink(filepath.Join(f.target, "Contents", "link")); err != nil || l != "app/app.json" {
			f.t.Fatalf("symlink lost: %q %v", l, err)
		}
	}
}

func (f *deltaFix) update() (*Update, error) {
	return Check(context.Background(), f.cfg)
}

func TestDeltaUpdateEveryKind(t *testing.T) {
	for _, kind := range []string{KindTree, KindBundle, KindFile} {
		t.Run(kind, func(t *testing.T) {
			f := newDeltaFix(t, kind)
			u, err := f.update()
			if err != nil || u == nil {
				t.Fatalf("%v %v", u, err)
			}
			if !u.HasDelta() {
				t.Fatal("the feed's patch was not picked")
			}
			if err := u.Download(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			if !u.Delta() || f.fullHits.Load() != 0 || f.patchHits.Load() != 1 {
				t.Fatalf("delta=%v full=%d patch=%d err=%v", u.Delta(), f.fullHits.Load(), f.patchHits.Load(), u.DeltaError())
			}
			if err := u.Apply(); err != nil {
				t.Fatal(err)
			}
			f.assertInstalled()
			st := LoadState(f.cfg.StateDir)
			if st.Phase != PhasePending || st.To != "1.1.0" || st.From != "1.0.0" || st.Prev == nil {
				t.Fatalf("state %+v", st)
			}
			if _, err := os.Stat(st.Prev.Dir); err != nil {
				t.Fatalf("previous version not kept: %v", err)
			}
		})
	}
}

func TestDeltaFallsBackToFull(t *testing.T) {
	flipByte := func(f *deltaFix) { f.patchBytes[len(f.patchBytes)-5] ^= 0xff }
	cases := map[string]func(f *deltaFix){
		"corrupt patch data": flipByte,
		"truncated patch":    func(f *deltaFix) { f.patchBytes = f.patchBytes[:len(f.patchBytes)/2] },
		"wrong signature":    func(f *deltaFix) { f.patchSig = signAs(f.t, f.key, testMeta, []byte("x")) },
		"signed by a stranger": func(f *deltaFix) {
			other, _ := mustKeys(f.t)
			s, _ := SignPatch(other, PatchMeta{AppID: "com.acme.app", Target: "linux-amd64", From: "1.0.0", To: "1.1.0"}, bytes.NewReader(f.patchBytes))
			f.patchSig = s
		},
		"installed file modified": func(f *deltaFix) {
			p := filepath.Join(f.target, "acme")
			if f.kind == KindBundle {
				p = filepath.Join(f.target, "Contents", "acme")
			}
			if f.kind == KindFile {
				p = f.target
			}
			b, _ := os.ReadFile(p)
			b[10] ^= 1
			if err := os.WriteFile(p, b, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"installed file missing": func(f *deltaFix) {
			p := filepath.Join(f.target, "acme")
			if f.kind == KindBundle {
				p = filepath.Join(f.target, "Contents", "acme")
			}
			if f.kind == KindFile {
				p = f.target
			}
			os.Remove(p)
			if f.kind == KindFile {
				if err := os.WriteFile(p, []byte("a different file entirely"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
		},
	}
	for name, mutate := range cases {
		for _, kind := range []string{KindTree, KindFile} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				f := newDeltaFix(t, kind)
				mutate(f)
				u, err := f.update()
				if err != nil || u == nil {
					t.Fatalf("%v %v", u, err)
				}
				if err := u.Download(context.Background(), nil); err != nil {
					t.Fatal(err)
				}
				if u.Delta() || u.DeltaError() == nil || f.fullHits.Load() != 1 {
					t.Fatalf("delta=%v err=%v full=%d", u.Delta(), u.DeltaError(), f.fullHits.Load())
				}
				// The installed exe may have been broken on purpose; the full
				// artifact still replaces everything.
				if err := u.Apply(); err != nil {
					t.Fatal(err)
				}
				f.assertInstalled()
			})
		}
	}
}

func TestDeltaLeavesNoStageOnFailure(t *testing.T) {
	f := newDeltaFix(t, KindTree)
	f.patchBytes[len(f.patchBytes)-5] ^= 0xff
	// Re-sign so only the data inside is damaged, which the file hashes catch.
	f.patchSig, _ = SignPatch(f.key, PatchMeta{AppID: "com.acme.app", Target: "linux-amd64", From: "1.0.0", To: "1.1.0"}, bytes.NewReader(f.patchBytes))
	u, _ := f.update()
	if err := u.Download(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if u.Delta() || u.DeltaError() == nil {
		t.Fatalf("a damaged but signed patch was applied: %v", u.DeltaError())
	}
	left, _ := filepath.Glob(filepath.Join(f.target, ".krate-new-*"))
	if len(left) != 0 {
		t.Fatalf("staging directory left behind: %v", left)
	}
}

func TestDeltaIgnoresPatchForOtherVersion(t *testing.T) {
	f := newDeltaFix(t, KindTree)
	f.patchFrom = "0.9.0"
	u, _ := f.update()
	if u.HasDelta() {
		t.Fatal("a patch from another version was picked")
	}
	if err := u.Download(context.Background(), nil); err != nil || f.patchHits.Load() != 0 || f.fullHits.Load() != 1 {
		t.Fatalf("%v patch=%d full=%d", err, f.patchHits.Load(), f.fullHits.Load())
	}
}

func TestOldClientIgnoresNewFeedFields(t *testing.T) {
	doc := `{"version":"1.2.0","url":"https://x/a.tar.gz","signature":"` + strings.Repeat("A", 86) + `==","patches":[{"from":"1.0.0","future":1}],"rollout":{"percentage":10,"future":true},"critical":true,"min_version":"1.1.0","brand_new":{}}`
	if _, err := ParseFeed([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	// And an old feed (no new fields) is accepted by Check: TestCheck covers it.
}

func TestPatchRejectsHostilePaths(t *testing.T) {
	f := newDeltaFix(t, KindTree)
	// Rewrite the manifest with an escaping path, keeping the blobs.
	m, start, err := func() (*patchManifest, int64, error) {
		p := filepath.Join(t.TempDir(), "p")
		if err := os.WriteFile(p, f.patchBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		fh, _ := os.Open(p)
		defer fh.Close()
		return readManifest(fh)
	}()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../evil", "/abs", "a/../../b", "a//b", ""} {
		m2 := *m
		m2.Entries = append([]patchEntry(nil), m.Entries...)
		m2.Entries[0].Path = bad
		data := rebuildPatch(t, &m2, f.patchBytes[start:])
		pp := filepath.Join(t.TempDir(), "x.krpatch")
		if err := os.WriteFile(pp, data, 0o644); err != nil {
			t.Fatal(err)
		}
		stage := filepath.Join(f.target, ".krate-new-x")
		if err := buildPatched(OSFS{}, pp, KindTree, f.target, stage, "", "1.0.0", "1.1.0"); err == nil {
			t.Errorf("path %q accepted", bad)
		}
		os.RemoveAll(stage)
	}
}

func rebuildPatch(t *testing.T, m *patchManifest, data []byte) []byte {
	t.Helper()
	j, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	out := []byte(PatchMagic)
	out = append(out, byte(len(j)>>24), byte(len(j)>>16), byte(len(j)>>8), byte(len(j)))
	out = append(out, j...)
	return append(out, data...)
}

func TestPatchSignatureBindsMeta(t *testing.T) {
	k, _ := mustKeys(t)
	pub := k.Public().(ed25519.PublicKey)
	data := []byte("patch bytes")
	m := PatchMeta{AppID: "a", Target: "linux-amd64", From: "1.0.0", To: "1.1.0"}
	sig, err := SignPatch(k, m, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPatch(pub, m, bytes.NewReader(data), sig); err != nil {
		t.Fatal(err)
	}
	for name, mm := range map[string]PatchMeta{
		"app":    {AppID: "b", Target: m.Target, From: m.From, To: m.To},
		"target": {AppID: m.AppID, Target: "windows-amd64", From: m.From, To: m.To},
		"from":   {AppID: m.AppID, Target: m.Target, From: "0.9.0", To: m.To},
		"to":     {AppID: m.AppID, Target: m.Target, From: m.From, To: "1.2.0"},
	} {
		if err := VerifyPatch(pub, mm, bytes.NewReader(data), sig); err == nil {
			t.Errorf("%s change not detected", name)
		}
	}
	if err := VerifyPatch(pub, m, bytes.NewReader(append(data, 'x')), sig); err == nil {
		t.Error("data change not detected")
	}
	// A full-artifact signature is not a patch signature.
	full, _ := Sign(k, Meta{AppID: "a", Target: "linux-amd64", Version: "1.1.0"}, bytes.NewReader(data))
	if err := VerifyPatch(pub, m, bytes.NewReader(data), full); err == nil {
		t.Error("an artifact signature verified as a patch signature")
	}
}

func TestPatchIsSmallerThanFull(t *testing.T) {
	f := newDeltaFix(t, KindTree)
	if int64(len(f.patchBytes)) >= int64(len(f.newFull))/3 {
		t.Fatalf("patch %d bytes, full %d", len(f.patchBytes), len(f.newFull))
	}
}

func TestPatchRejectsEscapingBasePath(t *testing.T) {
	f := newDeltaFix(t, KindTree)
	p := filepath.Join(t.TempDir(), "p")
	if err := os.WriteFile(p, f.patchBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	fh, _ := os.Open(p)
	m, start, err := readManifest(fh)
	fh.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Entries {
		if m.Entries[i].Op == "same" {
			m.Entries[i].From = "../../etc/passwd"
		}
	}
	pp := filepath.Join(t.TempDir(), "x.krpatch")
	if err := os.WriteFile(pp, rebuildPatch(t, m, f.patchBytes[start:]), 0o644); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(f.target, ".krate-new-x")
	if err := buildPatched(OSFS{}, pp, KindTree, f.target, stage, "", "1.0.0", "1.1.0"); err == nil {
		t.Fatal("escaping base path accepted")
	}
	os.RemoveAll(stage)
}
