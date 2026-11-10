package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var versionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// semverParts is a parsed version. pre is the prerelease tag, "" for a release.
type semverParts struct {
	major, minor, patch int
	pre                 string
}

func parseSemver(v string) (semverParts, bool) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return semverParts{}, false
	}
	n := func(s string) int { i, _ := strconv.Atoi(s); return i }
	return semverParts{n(m[1]), n(m[2]), n(m[3]), m[4]}, true
}

// compareSemver returns -1, 0 or 1 when a is older than, equal to or newer than
// b. A prerelease is older than its release. The second result is false when
// either side is not a version.
func compareSemver(a, b string) (int, bool) {
	pa, ok1 := parseSemver(a)
	pb, ok2 := parseSemver(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	cmp := func(x, y int) int {
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	for _, c := range []int{cmp(pa.major, pb.major), cmp(pa.minor, pb.minor), cmp(pa.patch, pb.patch)} {
		if c != 0 {
			return c, true
		}
	}
	switch {
	case pa.pre == pb.pre:
		return 0, true
	case pa.pre == "":
		return 1, true
	case pb.pre == "":
		return -1, true
	}
	return strings.Compare(pa.pre, pb.pre), true
}

var distTagRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// parseUpgradeTarget validates the version argument of `krate upgrade`: a
// version (with or without a leading v) or a dist-tag such as latest or next.
func parseUpgradeTarget(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "latest", nil
	}
	if _, ok := parseSemver(arg); ok {
		return strings.TrimPrefix(arg, "v"), nil
	}
	if distTagRe.MatchString(arg) {
		return arg, nil
	}
	return "", fmt.Errorf("%q is not a version or a dist-tag (expected 0.5.1, latest, next, ...)", arg)
}

// registryBase is the npm registry the version lookups use: the registry the
// package manager is configured with, else the public one.
func registryBase() string {
	for _, key := range []string{"npm_config_registry", "NPM_CONFIG_REGISTRY"} {
		if v := os.Getenv(key); v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	return "https://registry.npmjs.org"
}

// registryVersion asks the registry which version a dist-tag of pkg points at.
func registryVersion(ctx context.Context, base, pkg, tag string) (string, error) {
	u := base + "/" + url.PathEscape(pkg) + "/" + url.PathEscape(tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry answered %s for %s@%s", resp.Status, pkg, tag)
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", err
	}
	if _, ok := parseSemver(doc.Version); !ok {
		return "", fmt.Errorf("registry returned no usable version for %s@%s", pkg, tag)
	}
	return doc.Version, nil
}
