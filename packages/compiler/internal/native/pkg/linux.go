package pkg

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/rpmpack"
	"github.com/kratejs/krate/packages/native/desktopentry"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// linuxCategories maps the Apple category the app declares to freedesktop
// menu categories.
var linuxCategories = map[string]string{
	"productivity": "Office;", "developer-tools": "Development;", "games": "Game;",
	"graphics-design": "Graphics;", "music": "AudioVideo;Audio;", "video": "AudioVideo;Video;",
	"social-networking": "Network;", "finance": "Office;Finance;", "education": "Education;",
	"utilities": "Utility;", "news": "Network;News;", "business": "Office;",
}

func desktopCategories(macCat string) string {
	if c, ok := linuxCategories[strings.TrimPrefix(macCat, "public.app-category.")]; ok {
		return c
	}
	return "Utility;"
}

func desktopFile(m Metadata, exec string) []byte {
	return []byte(desktopentry.Entry(desktopentry.Options{
		Name: m.ProductName, Exec: exec, Icon: m.slug(), Comment: m.ProductName,
		Schemes: m.DeepLinks, Categories: desktopCategories(m.MacCategory), StartupWMClass: m.AppID,
	}))
}

// linuxInstallTree lays the app out as an FHS install: a single executable
// goes to /usr/bin, an executable with support files to /usr/lib/<slug> with a
// symlink in /usr/bin. It also returns the Exec path of the launcher.
func linuxInstallTree(o *Options, ic *Icon) (*tree, string, error) {
	m := o.Meta
	name := m.slug()
	t := newTree()
	exec := "/usr/bin/" + name
	if len(o.Payload.ExtraFiles) == 0 {
		t.addFile("usr/bin/"+name, 0o755, o.Payload.Executable)
	} else {
		base := "usr/lib/" + name
		t.addDir(base)
		t.addFile(base+"/"+name, 0o755, o.Payload.Executable)
		for dest, src := range o.Payload.ExtraFiles {
			if err := t.addDisk(base+"/"+cleanPath(dest), src); err != nil {
				return nil, "", fail(diag.CodeNativePkgInput, src, err.Error(), "")
			}
		}
		t.addSymlink("usr/bin/"+name, "../lib/"+name+"/"+name)
	}
	t.addData("usr/share/applications/"+m.AppID+".desktop", 0o644, desktopFile(m, exec))
	for _, s := range hicolorSizes {
		data, err := ic.PNG(s)
		if err != nil {
			return nil, "", fail(diag.CodeNativePkgIcon, "native.icons", err.Error(), "")
		}
		t.addData(fmt.Sprintf("usr/share/icons/hicolor/%dx%d/apps/%s.png", s, s, name), 0o644, data)
	}
	return t, exec, nil
}

// linuxUpdateTree is what the updater unpacks over the install directory:
// the executable and its support files, flat.
func linuxUpdateTree(o *Options) (*tree, error) {
	return o.buildTree(o.Meta.slug(), "", false)
}

func packageLinux(o *Options, ic *Icon, want map[string]bool, stage string, res *Result) error {
	m, p := o.Meta, o.Payload
	ut, err := linuxUpdateTree(o)
	if err != nil {
		return err
	}
	if err := writeDisk(stage, ut); err != nil {
		return wrapWrite(stage, err)
	}
	tgz := filepath.Join(o.OutDir, artifactName(m, p, ".tar.gz"))
	if err := writeTarGzFile(tgz, ut); err != nil {
		return wrapWrite(tgz, err)
	}
	res.add(Artifact{Path: tgz, Format: "tar.gz", Update: true})
	if o.Signer != nil {
		if err := o.Signer.SignFile(tgz, "tar.gz"); err != nil {
			return err
		}
	}

	if want["deb"] || want["rpm"] {
		it, _, err := linuxInstallTree(o, ic)
		if err != nil {
			return err
		}
		if want["deb"] {
			out := filepath.Join(o.OutDir, fmt.Sprintf("%s_%s_%s.deb", m.slug(), debianVersion(m.Version), p.Arch))
			if err := writeDeb(out, m, p, it); err != nil {
				return wrapWrite(out, err)
			}
			res.add(Artifact{Path: out, Format: "deb"})
			if o.Signer != nil {
				if err := o.Signer.SignFile(out, "deb"); err != nil {
					return err
				}
			}
		}
		if want["rpm"] {
			out := filepath.Join(o.OutDir, fmt.Sprintf("%s-%s-1.%s.rpm", m.slug(), rpmVersion(m.Version), rpmArch(p.Arch)))
			if err := writeRPM(out, m, p, it); err != nil {
				return wrapWrite(out, err)
			}
			res.add(Artifact{Path: out, Format: "rpm"})
			if o.Signer != nil {
				if err := o.Signer.SignFile(out, "rpm"); err != nil {
					return err
				}
			}
		}
	}
	if want["appimage"] {
		out := filepath.Join(o.OutDir, artifactName(m, p, ".AppImage"))
		if err := buildAppImage(o, ic, stage, out); err != nil {
			return err
		}
		res.add(Artifact{Path: out, Format: "appimage"})
		if o.Signer != nil {
			if err := o.Signer.SignFile(out, "appimage"); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeTarGzFile(p string, t *tree) error {
	f, err := createFile(p)
	if err != nil {
		return err
	}
	if err := writeTarGz(f, t, "", false); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func debianVersion(v string) string {
	s, _ := splitVersion(v)
	return s.debian()
}

func rpmVersion(v string) string {
	s, _ := splitVersion(v)
	return s.debian() // rpm also sorts "~" before the release
}

func rpmArch(a string) string {
	if a == "arm64" {
		return "aarch64"
	}
	return "x86_64"
}

func debArch(a string) string { return a }

// maintainer builds a "Name <address>" string; dpkg wants an address.
func maintainer(m Metadata) string {
	name := m.Publisher
	if i := strings.Index(name, "="); i >= 0 {
		name = publisherDisplay(name)
	}
	if name == "" {
		name = m.ProductName
	}
	parts := strings.Split(m.AppID, ".")
	domain := "example.invalid"
	if len(parts) >= 2 {
		domain = parts[1] + "." + parts[0]
	}
	return fmt.Sprintf("%s <noreply@%s>", name, domain)
}

// debControl returns the control file.
func debControl(m Metadata, p Payload, installedKB int64) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "Package: %s\n", m.slug())
	fmt.Fprintf(&b, "Version: %s\n", debianVersion(m.Version))
	fmt.Fprintf(&b, "Architecture: %s\n", debArch(p.Arch))
	fmt.Fprintf(&b, "Maintainer: %s\n", maintainer(m))
	fmt.Fprintf(&b, "Installed-Size: %d\n", installedKB)
	b.WriteString("Depends: libwebkit2gtk-4.1-0, libgtk-3-0\n")
	b.WriteString("Section: utils\nPriority: optional\n")
	fmt.Fprintf(&b, "Description: %s desktop application\n", m.ProductName)
	fmt.Fprintf(&b, " %s is built with Krate Native.\n", m.ProductName)
	return []byte(b.String())
}

// arWriter writes a Unix ar archive (the deb container).
type arWriter struct {
	w   io.Writer
	err error
}

func newAr(w io.Writer) *arWriter {
	a := &arWriter{w: w}
	_, a.err = io.WriteString(w, "!<arch>\n")
	return a
}

func (a *arWriter) add(name string, data []byte, mt time.Time) {
	if a.err != nil {
		return
	}
	hdr := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8s%-10d`\n", name, mt.Unix(), 0, 0, "100644", len(data))
	_, a.err = io.WriteString(a.w, hdr)
	if a.err == nil {
		_, a.err = a.w.Write(data)
	}
	if a.err == nil && len(data)%2 == 1 {
		_, a.err = io.WriteString(a.w, "\n")
	}
}

// writeDeb writes a Debian package: debian-binary, control.tar.gz and
// data.tar.gz inside an ar archive.
func writeDeb(out string, m Metadata, p Payload, data *tree) error {
	var total int64
	md5s := &strings.Builder{}
	for _, e := range data.sorted() {
		if e.isDir() || e.isSymlink() {
			continue
		}
		n, err := e.size()
		if err != nil {
			return err
		}
		total += n
		r, err := e.open()
		if err != nil {
			return err
		}
		h := md5.New()
		_, err = io.Copy(h, r)
		r.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(md5s, "%x  %s\n", h.Sum(nil), e.Path)
	}
	ctl := newTree()
	ctl.addData("control", 0o644, debControl(m, p, (total+1023)/1024))
	ctl.addData("md5sums", 0o644, []byte(md5s.String()))
	var ctlTar, dataTar bytes.Buffer
	if err := writeDebTar(&ctlTar, ctl, false); err != nil {
		return err
	}
	if err := writeDebTar(&dataTar, data, true); err != nil {
		return err
	}
	f, err := createFile(out)
	if err != nil {
		return err
	}
	mt := buildTime()
	ar := newAr(f)
	ar.add("debian-binary", []byte("2.0\n"), mt)
	ar.add("control.tar.gz", ctlTar.Bytes(), mt)
	ar.add("data.tar.gz", dataTar.Bytes(), mt)
	if ar.err != nil {
		f.Close()
		return ar.err
	}
	return f.Close()
}

// writeDebTar writes a gzip-compressed tar with "./" names. root owns every
// entry; for the control archive that is what dpkg expects.
func writeDebTar(w io.Writer, t *tree, withRootDir bool) error {
	gz, _ := gzip.NewWriterLevel(w, gzip.BestCompression)
	gz.ModTime = time.Time{}
	if err := writeTar(gz, t, "", true); err != nil {
		return err
	}
	_ = withRootDir
	return gz.Close()
}

// writeRPM writes an rpm package with rpmpack.
func writeRPM(out string, m Metadata, p Payload, data *tree) error {
	v, _ := splitVersion(m.Version)
	meta := rpmpack.RPMMetaData{
		Name: m.slug(), Version: v.debian(), Release: "1", Arch: rpmArch(p.Arch),
		Summary:     m.ProductName + " desktop application",
		Description: m.ProductName + " is built with Krate Native.",
		Licence:     "Proprietary", Packager: maintainer(m), Vendor: m.Publisher,
		Group: "Applications/Productivity", BuildTime: buildTime(),
	}
	r, err := rpmpack.NewRPM(meta)
	if err != nil {
		return err
	}
	for _, dep := range []string{"webkit2gtk4.1", "gtk3"} {
		rel, err := rpmpack.NewRelation(dep)
		if err != nil {
			return err
		}
		r.Requires = append(r.Requires, rel)
	}
	mt := uint32(buildTime().Unix())
	own := map[string]bool{}
	for _, e := range data.sorted() {
		name := "/" + e.Path
		switch {
		case e.isDir():
			// Own only the app's directories, never /usr or /usr/bin.
			if strings.HasPrefix(e.Path, "usr/lib/") || strings.HasPrefix(e.Path, "usr/share/icons/hicolor/") && strings.Count(e.Path, "/") > 4 {
				r.AddFile(rpmpack.RPMFile{Name: name, Mode: 0o40755, Owner: "root", Group: "root", MTime: mt})
				own[name] = true
			}
		case e.isSymlink():
			r.AddFile(rpmpack.RPMFile{Name: name, Body: []byte(e.Link), Mode: 0o120777, Owner: "root", Group: "root", MTime: mt})
		default:
			rd, err := e.open()
			if err != nil {
				return err
			}
			body, err := io.ReadAll(rd)
			rd.Close()
			if err != nil {
				return err
			}
			r.AddFile(rpmpack.RPMFile{Name: name, Body: body, Mode: 0o100000 | uint(e.Mode.Perm()), Owner: "root", Group: "root", MTime: mt})
		}
	}
	r.AllowListDirs(own)
	f, err := createFile(out)
	if err != nil {
		return err
	}
	if err := r.Write(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// buildAppImage assembles an AppDir and runs appimagetool. A pure Go
// AppImage needs a squashfs writer; none is practical to embed, so the tool is
// required.
func buildAppImage(o *Options, ic *Icon, stage, out string) error {
	tool, err := o.need("appimagetool", "appimage",
		"download appimagetool from https://github.com/AppImage/appimagetool/releases (chmod +x, put it in PATH), or drop appimage from native.bundle.linux.formats")
	if err != nil {
		return err
	}
	m, p := o.Meta, o.Payload
	name := m.slug()
	t := newTree()
	base := "usr/lib/" + name
	t.addDir(base)
	t.addFile(base+"/"+name, 0o755, p.Executable)
	for dest, src := range p.ExtraFiles {
		if err := t.addDisk(base+"/"+cleanPath(dest), src); err != nil {
			return fail(diag.CodeNativePkgInput, src, err.Error(), "")
		}
	}
	t.addSymlink("usr/bin/"+name, "../lib/"+name+"/"+name)
	apprun := "#!/bin/sh\nHERE=\"$(dirname \"$(readlink -f \"$0\")\")\"\nexec \"$HERE/" + base + "/" + name + "\" \"$@\"\n"
	t.addData("AppRun", 0o755, []byte(apprun))
	t.addData(name+".desktop", 0o644, desktopFile(m, name))
	for _, s := range []int{256, 512} {
		data, err := ic.PNG(s)
		if err != nil {
			return fail(diag.CodeNativePkgIcon, "native.icons", err.Error(), "")
		}
		if s == 256 {
			t.addData(name+".png", 0o644, data)
			t.addData(".DirIcon", 0o644, data)
		}
		t.addData(fmt.Sprintf("usr/share/icons/hicolor/%dx%d/apps/%s.png", s, s, name), 0o644, data)
	}
	appDir := filepath.Join(stage, name+".AppDir")
	if err := writeDisk(appDir, t); err != nil {
		return wrapWrite(appDir, err)
	}
	arch := "x86_64"
	if p.Arch == "arm64" {
		arch = "aarch64"
	}
	os.Remove(out)
	if b, err := o.run("", []string{"ARCH=" + arch}, tool, appDir, out); err != nil {
		return fail(diag.CodeNativeSignFailed, out, "appimagetool failed:\n"+strings.TrimSpace(string(b)), "the AppDir is kept at "+appDir)
	}
	return nil
}

// SignLinuxArtifacts signs the Linux packages of the app in dir with the
// signer (detached gpg signatures).
func SignLinuxArtifacts(s Signer, dir string, m Metadata) (*Result, error) {
	res := &Result{Stage: dir}
	for _, pat := range []string{"*.deb", "*.rpm", "*.tar.gz", "*.AppImage"} {
		files, _ := filepath.Glob(filepath.Join(dir, m.slug()+pat[1:]))
		more, _ := filepath.Glob(filepath.Join(dir, m.slug()+"_"+pat))
		for _, f := range append(files, more...) {
			if err := s.SignFile(f, strings.TrimPrefix(filepath.Ext(f), ".")); err != nil {
				return nil, err
			}
			res.add(Artifact{Path: f + ".asc", Format: "signature"})
		}
	}
	if len(res.Artifacts) == 0 {
		return nil, fail(diag.CodeNativePkgInput, dir, "no Linux packages to sign", "run krate native package first")
	}
	return res, nil
}
