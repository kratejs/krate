package build

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/ogimage"
	"github.com/kratejs/krate/packages/compiler/internal/sitepath"
)

// Metadata routes: sitemap.ts, robots.ts and manifest.ts return data that is
// rendered to sitemap.xml, robots.txt and manifest.webmanifest; opengraph-image
// and twitter-image files (an ImageResponse component, or a static image) become
// PNGs. All run once at build time. Image routes feed og:image and twitter:image
// into the metadata of the segment they live in and of everything below it.

type metaRouteKind string

const (
	mrSitemap  metaRouteKind = "sitemap"
	mrRobots   metaRouteKind = "robots"
	mrManifest metaRouteKind = "manifest"
	mrOG       metaRouteKind = "opengraph-image"
	mrTwitter  metaRouteKind = "twitter-image"
)

type metaRouteFile struct {
	Kind   metaRouteKind
	Path   string
	URLDir string // pages-relative URL directory ("" for the root, "blog/[slug]")
	Static bool   // a ready-made image file, not a module
}

var metaScriptExts = map[string]bool{".ts": true, ".js": true, ".mjs": true}
var metaImageSrcExts = map[string]bool{".tsx": true, ".jsx": true, ".ts": true, ".js": true}
var metaStaticImageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// metaRouteOf classifies a file base name (with extension).
func metaRouteOf(name string) (metaRouteKind, bool, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	switch metaRouteKind(stem) {
	case mrSitemap, mrRobots, mrManifest:
		return metaRouteKind(stem), false, metaScriptExts[ext]
	case mrOG, mrTwitter:
		if metaStaticImageExts[ext] {
			return metaRouteKind(stem), true, true
		}
		return metaRouteKind(stem), false, metaImageSrcExts[ext]
	}
	return "", false, false
}

// isMetadataRouteFile reports whether a file name under the pages directory is
// a metadata route rather than a page. (sitemap.tsx stays a page: an HTML sitemap.)
func isMetadataRouteFile(name string) bool {
	_, _, ok := metaRouteOf(name)
	return ok
}

// findMetadataRouteFiles lists the metadata route files under the pages directory.
func findMetadataRouteFiles(pagesDir string) []metaRouteFile {
	var out []metaRouteFile
	exts := map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}
	_ = fsutil.WalkExt(pagesDir, exts, nil, func(path string, _ os.FileInfo) error {
		rel, err := filepath.Rel(pagesDir, path)
		if err != nil || pathHasSlot(filepath.ToSlash(rel)) {
			return nil
		}
		kind, static, ok := metaRouteOf(filepath.Base(path))
		if !ok {
			return nil
		}
		out = append(out, metaRouteFile{Kind: kind, Path: path, URLDir: segmentURLDir(segmentDirKey(path, pagesDir)), Static: static})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// imageRef is one image a convention file produced.
type imageRef struct {
	URL    string `json:"url"` // may hold [param] placeholders
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Alt    string `json:"alt,omitempty"`
	Type   string `json:"type,omitempty"`
}

type imageSet struct {
	og, twitter []imageRef
}

// metaImages is the build's index of convention images by URL directory.
type metaImages struct {
	mu  sync.Mutex
	dir map[string]*imageSet
}

func (m *metaImages) add(urlDir string, kind metaRouteKind, ref imageRef) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dir == nil {
		m.dir = map[string]*imageSet{}
	}
	s := m.dir[urlDir]
	if s == nil {
		s = &imageSet{}
		m.dir[urlDir] = s
	}
	if kind == mrOG {
		s.og = append(s.og, ref)
	} else {
		s.twitter = append(s.twitter, ref)
	}
}

// filesJSON is the Metadata contribution of the images in urlDir ("" when none):
// the shape metadata.ts merges as a segment's `files`.
func (m *metaImages) filesJSON(urlDir string) string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.dir[urlDir]
	if s == nil {
		return ""
	}
	files := map[string]any{}
	if len(s.og) > 0 {
		files["openGraph"] = map[string]any{"images": s.og}
	}
	if len(s.twitter) > 0 {
		files["twitter"] = map[string]any{"images": s.twitter}
	}
	b, err := json.Marshal(files)
	if err != nil || len(files) == 0 {
		return ""
	}
	return string(b)
}

// buildMetadataRoutes runs every metadata route file and writes its output. It
// runs before the pages are built, because their <head> needs the image URLs.
func (b *Builder) buildMetadataRoutes(pages []string) []error {
	files := findMetadataRouteFiles(b.Cfg.PagesDir)
	b.metaImages = &metaImages{}
	if len(files) == 0 {
		if err := b.writeMetaRoutes(nil); err != nil {
			return []error{err}
		}
		return nil
	}
	bnd := b.newBundler()
	var errs []error
	var dynamic []metaRouteEntry
	for _, f := range files {
		var err error
		switch f.Kind {
		case mrSitemap, mrRobots, mrManifest:
			if isDyn, revalidate := b.dynamicMetaRoute(f); isDyn {
				var entry metaRouteEntry
				if entry, err = b.buildDynamicDataRoute(f, revalidate); err == nil {
					dynamic = append(dynamic, entry)
				}
				break
			}
			err = b.buildDataRoute(f, bnd)
		default:
			if f.Static {
				err = b.copyStaticImageRoute(f)
			} else {
				err = b.buildImageRoute(f, pages, bnd)
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if err := b.writeMetaRoutes(dynamic); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func (b *Builder) display(path string) string {
	if r, err := filepath.Rel(b.Root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}

func (b *Builder) routeErr(f metaRouteFile, msg, hint string) error {
	return diag.Diagnostic{File: b.display(f.Path), Code: diag.CodeMetadataRoute, Message: msg, Hint: hint}
}

func joinURLDir(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func (b *Builder) writeOut(rel string, data []byte) error {
	dest := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := fsutil.WriteFileIfChanged(dest, data, 0o644); err != nil {
		return err
	}
	printf("    %s+%s %s\n", cGreen, cReset, rel)
	return nil
}

// dataRouteBootstrap imports a sitemap, robots or manifest module and prints
// its rendered files as JSON.
func dataRouteBootstrap(abs string, kind metaRouteKind) string {
	render := map[metaRouteKind]string{mrSitemap: "renderSitemapXML", mrRobots: "renderRobotsTXT", mrManifest: "renderManifestJSON"}[kind]
	file := map[metaRouteKind]string{mrSitemap: "sitemap.xml", mrRobots: "robots.txt", mrManifest: "manifest.webmanifest"}[kind]
	return fmt.Sprintf(`import * as mod from %q;
import { %s as render } from "@krate/runtime/server";
const produce = typeof mod.default === "function" ? mod.default : () => mod.default;
const out = [];
const add = (path, value) => out.push({ path, body: Buffer.from(render(value)).toString("base64") });
if (%q === "sitemap" && typeof mod.generateSitemaps === "function") {
  for (const item of await mod.generateSitemaps()) add("sitemap/" + item.id + ".xml", await produce({ id: String(item.id) }));
} else {
  add(%q, await produce());
}
console.log(JSON.stringify(out));
`, filepath.ToSlash(abs), render, string(kind), file)
}

func (b *Builder) buildDataRoute(f metaRouteFile, bnd *bundler.Bundler) error {
	abs, err := filepath.Abs(f.Path)
	if err != nil {
		return err
	}
	out, err := b.runBootstrapBundle(abs, dataRouteBootstrap(abs, f.Kind), "metaroute", bnd, nil)
	if err != nil {
		return b.routeErr(f, "running "+filepath.Base(f.Path)+" failed: "+err.Error(), "the file runs once at build time on Node with the project's environment; it must default-export a function (or a value) shaped like Next.js's MetadataRoute."+titleCase(string(f.Kind)))
	}
	var files []struct {
		Path string `json:"path"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &files); err != nil {
		return b.routeErr(f, "unreadable output: "+err.Error(), "")
	}
	for _, file := range files {
		data, err := base64.StdEncoding.DecodeString(file.Body)
		if err != nil {
			return b.routeErr(f, "unreadable output: "+err.Error(), "")
		}
		if err := b.writeOut(joinURLDir(f.URLDir, file.Path), data); err != nil {
			return err
		}
	}
	return nil
}

// imageRouteBootstrap runs an opengraph-image module for each param set and
// prints the renderer specs.
func imageRouteBootstrap(abs, root string, sets []map[string]string) string {
	setsJSON, _ := json.Marshal(sets)
	rootJSON, _ := json.Marshal(root)
	return fmt.Sprintf(`import * as mod from %q;
import { inlineImages } from "@krate/runtime/og";
const sets = %s;
const root = %s;
const thenable = (p) => Object.assign(Promise.resolve(p), p);
const out = [];
for (const params of sets) {
  const metas = typeof mod.generateImageMetadata === "function" ? await mod.generateImageMetadata({ params: thenable(params) }) : [{}];
  for (const meta of metas) {
    const res = await mod.default({ params: thenable(params), id: meta.id !== undefined ? thenable(String(meta.id)) : undefined });
    const spec = res && res.__krateImage;
    if (!spec) throw new Error("the default export must return an ImageResponse");
    await inlineImages(spec, root);
    const size = meta.size || mod.size || {};
    out.push({
      params,
      id: meta.id === undefined ? "" : String(meta.id),
      alt: meta.alt !== undefined ? meta.alt : mod.alt || "",
      type: meta.contentType || mod.contentType || "image/png",
      width: size.width || spec.width,
      height: size.height || spec.height,
      spec,
    });
  }
}
console.log(JSON.stringify(out));
`, filepath.ToSlash(abs), setsJSON, rootJSON)
}

// paramSetsFor returns the param sets an image route in urlDir is built for:
// one empty set for a static directory, else the generateStaticParams entries
// of the page that serves the directory.
func (b *Builder) paramSetsFor(f metaRouteFile, pages []string) ([]map[string]string, bool) {
	if !strings.Contains(f.URLDir, "[") {
		return []map[string]string{{}}, true
	}
	for _, p := range pages {
		out := pageToOutput(p, b.Cfg.PagesDir)
		if out == "" || out != f.URLDir {
			continue
		}
		res := b.resolveStaticParamsPageOnce(p)
		if len(res.expanded) == 0 {
			continue
		}
		var sets []map[string]string
		for _, e := range res.expanded {
			sets = append(sets, e.Params)
		}
		return sets, true
	}
	return nil, false
}

func fillPattern(pattern string, params map[string]string) string {
	var parts []string
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "" {
			continue
		}
		if rs := parseRouteSeg(seg); rs.kind != segStatic {
			if v, ok := params[rs.text]; ok {
				if v != "" {
					parts = append(parts, v)
				}
				continue
			}
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, "/")
}

func (b *Builder) buildImageRoute(f metaRouteFile, pages []string, bnd *bundler.Bundler) error {
	abs, err := filepath.Abs(f.Path)
	if err != nil {
		return err
	}
	sets, ok := b.paramSetsFor(f, pages)
	if !ok {
		d := diag.Diagnostic{File: b.display(f.Path), Code: diag.CodeImageRouteParams, Severity: diag.SeverityWarning,
			Message: filepath.Base(f.Path) + " is in a dynamic segment (" + f.URLDir + ") but the segment's page has no generateStaticParams, so no image was built",
			Hint:    "export generateStaticParams from the page of " + f.URLDir + "; one image is built per entry"}
		printWarningsOnce(os.Stderr, []diag.Diagnostic{d})
		return nil
	}
	out, err := b.runBootstrapBundle(abs, imageRouteBootstrap(abs, b.Root, sets), "imageroute", bnd, nil)
	if err != nil {
		return b.routeErr(f, "running "+filepath.Base(f.Path)+" failed: "+err.Error(), "the file runs once per generateStaticParams entry at build time on Node; its default export (a function) must return new ImageResponse(<jsx />, { width, height })")
	}
	var entries []struct {
		Params map[string]string `json:"params"`
		ID     string            `json:"id"`
		Alt    string            `json:"alt"`
		Type   string            `json:"type"`
		Width  int               `json:"width"`
		Height int               `json:"height"`
		Spec   json.RawMessage   `json:"spec"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return b.routeErr(f, "unreadable output: "+err.Error(), "")
	}
	name := string(f.Kind)
	seenURL := map[string]bool{}
	for _, e := range entries {
		spec, err := ogimage.ParseSpec(e.Spec)
		if err != nil {
			return b.routeErr(f, err.Error(), "")
		}
		png, err := ogimage.Render(spec)
		if err != nil {
			var oe *ogimage.Error
			if errors.As(err, &oe) {
				return diag.Diagnostic{File: b.display(f.Path), Code: diag.CodeImageUnsupported, Message: oe.Error(),
					Hint: "draw with the supported subset (see the Metadata routes guide), or pass a font with options.fonts for other characters"}
			}
			return b.routeErr(f, "drawing the image failed: "+err.Error(), "")
		}
		file := name
		if e.ID != "" {
			file += "-" + e.ID
		}
		if err := b.writeOut(joinURLDir(fillPattern(f.URLDir, e.Params), file+".png"), png); err != nil {
			return err
		}
		url := sitepath.Join(b.Cfg.BaseURLPath(), "/"+joinURLDir(f.URLDir, file+".png"))
		if !seenURL[url] {
			seenURL[url] = true
			b.metaImages.add(f.URLDir, f.Kind, imageRef{URL: url, Width: e.Width, Height: e.Height, Alt: e.Alt, Type: "image/png"})
		}
	}
	return nil
}

func (b *Builder) copyStaticImageRoute(f metaRouteFile) error {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(f.Path))
	rel := joinURLDir(f.URLDir, string(f.Kind)+ext)
	if strings.Contains(f.URLDir, "[") {
		return b.routeErr(f, "a static image cannot be in a dynamic segment", "use an "+string(f.Kind)+".tsx that returns an ImageResponse")
	}
	if err := b.writeOut(rel, data); err != nil {
		return err
	}
	ref := imageRef{URL: sitepath.Join(b.Cfg.BaseURLPath(), "/"+rel), Type: map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}[ext]}
	if cfg, _, err := image.DecodeConfig(strings.NewReader(string(data))); err == nil {
		ref.Width, ref.Height = cfg.Width, cfg.Height
	}
	if alt, err := os.ReadFile(strings.TrimSuffix(f.Path, filepath.Ext(f.Path)) + ".alt.txt"); err == nil {
		ref.Alt = strings.TrimSpace(string(alt))
	}
	b.metaImages.add(f.URLDir, f.Kind, ref)
	return nil
}

// titleCase upper-cases the first letter of an ASCII word.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
