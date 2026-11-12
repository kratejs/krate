package build

import (
	_ "embed"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

//go:embed assets/motion.min.js
var motionLibJS string

//go:embed assets/motion-init.js
var motionInitJS string

const motionPendingHead = `<script>(function(){var r=document.documentElement;r.classList.add("krate-motion-pending");setTimeout(function(){r.classList.remove("krate-motion-pending")},3000)})();</script>` + "\n" +
	`<style>.krate-motion-pending [data-motion]:not([data-motion-ready]),.krate-motion-pending [data-motion-initial]:not([data-motion-ready]){opacity:0}</style>` + "\n"

func motionInitSource(c *config.MotionConfig, minify bool) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	src := strings.Replace(motionInitJS, "__MOTION_CONFIG__", string(data), 1)
	if minify {
		src = minifyJSBase(src)
	}
	return src, nil
}

func motionChunkSource(c *config.MotionConfig, minify bool) (string, error) {
	init, err := motionInitSource(c, minify)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(motionLibJS)
	sb.WriteString("\n")
	sb.WriteString(init)
	sb.WriteString("\n")
	return sb.String(), nil
}

func motionHeadHTML(basePath, chunk string, declarative bool) string {
	if chunk == "" {
		return ""
	}
	var sb strings.Builder
	if declarative {
		sb.WriteString(motionPendingHead)
	}
	sb.WriteString("<script defer src=\"" + assetHref(basePath, chunk) + "\"></script>\n")
	return sb.String()
}

func (b *Builder) writeMotionChunk() error {
	b.motionChunk = ""
	if !b.Cfg.MotionEnabled() {
		return nil
	}
	src, err := motionChunkSource(b.Cfg.Motion, b.shouldMinifyJS())
	if err != nil {
		return err
	}
	rel := "chunks/motion." + hashContent([]byte(src)) + ".js"
	if err := writeOutput(filepath.Join(b.Cfg.OutDir, rel), []byte(src)); err != nil {
		return err
	}
	b.motionChunk = rel
	return nil
}
