// Package update tells the user when a newer cc360 release exists, and how to
// install it. It never replaces the binary itself.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	repo          = "achton/cc360"
	latestURL     = "https://api.github.com/repos/" + repo + "/releases/latest"
	checkInterval = 24 * time.Hour
	fetchTimeout  = 5 * time.Second
)

// Release is the newest published cc360 release.
type Release struct {
	Version string // without the leading "v", e.g. "0.9.1"
	URL     string // the release page
}

// Checker asks GitHub for the latest release, at most once per checkInterval.
type Checker struct {
	URL       string
	CachePath string
	Client    *http.Client
	Now       func() time.Time
}

// NewChecker returns a Checker that caches its answer in cacheDir.
func NewChecker(cacheDir string) Checker {
	return Checker{
		URL:       latestURL,
		CachePath: filepath.Join(cacheDir, "update-check.json"),
		Client:    &http.Client{Timeout: fetchTimeout},
		Now:       time.Now,
	}
}

// Newer returns the latest release when it is newer than current. Any failure
// returns false, because a failed check must never get in the way.
func (c Checker) Newer(ctx context.Context, current string) (Release, bool) {
	cur, ok := parseVersion(current)
	if !ok {
		return Release{}, false
	}
	rel, ok := c.latest(ctx)
	if !ok {
		return Release{}, false
	}
	latest, ok := parseVersion(rel.Version)
	if !ok || !latest.after(cur) {
		return Release{}, false
	}
	return rel, true
}

type cacheFile struct {
	CheckedAt time.Time `json:"checkedAt"`
	Version   string    `json:"version"`
	URL       string    `json:"url"`
}

// latest returns the cached release while it is fresh, else asks GitHub. A
// failed request is cached too, so an offline machine asks once a day, not on
// every launch; the last known release is kept.
func (c Checker) latest(ctx context.Context) (Release, bool) {
	cached, haveCache := c.readCache()
	now := c.Now()
	if haveCache && now.Sub(cached.CheckedAt) < checkInterval && !cached.CheckedAt.After(now) {
		return Release{Version: cached.Version, URL: cached.URL}, cached.Version != ""
	}

	rel, err := c.fetch(ctx)
	if err != nil {
		cached.CheckedAt = now
		c.writeCache(cached)
		return Release{Version: cached.Version, URL: cached.URL}, cached.Version != ""
	}
	c.writeCache(cacheFile{CheckedAt: now, Version: rel.Version, URL: rel.URL})
	return rel, true
}

func (c Checker) fetch(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cc360-update-check")
	resp, err := c.Client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, err
	}
	if _, ok := parseVersion(body.TagName); !ok {
		return Release{}, fmt.Errorf("unexpected tag %q", body.TagName)
	}
	return Release{Version: strings.TrimPrefix(body.TagName, "v"), URL: body.HTMLURL}, nil
}

func (c Checker) readCache() (cacheFile, bool) {
	data, err := os.ReadFile(c.CachePath)
	if err != nil {
		return cacheFile{}, false
	}
	var f cacheFile
	if json.Unmarshal(data, &f) != nil {
		return cacheFile{}, false
	}
	return f, true
}

func (c Checker) writeCache(f cacheFile) {
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(c.CachePath), 0o755) != nil {
		return
	}
	_ = os.WriteFile(c.CachePath, data, 0o644)
}

// version is the MAJOR.MINOR.PATCH core of a version string.
type version [3]int

// parseVersion reads "v1.2.3", "1.2.3" or a `git describe` string such as
// "v1.2.3-4-gabcdef-dirty", which compares as its release. "dev" and anything
// else unparseable return false.
func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var v version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		v[i] = n
	}
	return v, true
}

func (v version) after(o version) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] > o[i]
		}
	}
	return false
}

// installEnv is what Command inspects on the machine, so tests can fake it.
type installEnv struct {
	exists func(path string) bool
	goBins []string
	goos   string
	goarch string
}

func realEnv() installEnv {
	var bins []string
	if b := os.Getenv("GOBIN"); b != "" {
		bins = append(bins, b)
	}
	if p := os.Getenv("GOPATH"); p != "" {
		for _, dir := range filepath.SplitList(p) {
			bins = append(bins, filepath.Join(dir, "bin"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		bins = append(bins, filepath.Join(home, "go", "bin"))
	}
	return installEnv{
		exists: func(p string) bool { _, err := os.Stat(p); return err == nil },
		goBins: bins,
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}
}

// Command returns a shell command that upgrades the cc360 binary at exe to
// version (without the "v"), chosen by how that binary was installed.
func Command(exe, version string) string {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return command(exe, version, realEnv())
}

func command(exe, ver string, env installEnv) string {
	dir := filepath.Dir(exe)
	asset := func(ext string) string {
		return fmt.Sprintf("https://github.com/%s/releases/download/v%s/cc360_%s_%s_%s.%s",
			repo, ver, ver, env.goos, env.goarch, ext)
	}

	switch {
	case strings.Contains(exe, "/Caskroom/") || strings.Contains(exe, "/Cellar/"):
		return "brew upgrade --cask achton/tap/cc360"
	case dir == "/usr/bin" && env.exists("/var/lib/dpkg/info/cc360.list"):
		deb := fmt.Sprintf("cc360_%s_%s_%s.deb", ver, env.goos, env.goarch)
		return fmt.Sprintf("curl -fsSLo /tmp/%s %s && sudo dpkg -i /tmp/%s", deb, asset("deb"), deb)
	case dir == "/usr/bin" && env.exists("/lib/apk/db/installed"):
		apk := fmt.Sprintf("cc360_%s_%s_%s.apk", ver, env.goos, env.goarch)
		return fmt.Sprintf("curl -fsSLo /tmp/%s %s && sudo apk add --allow-untrusted /tmp/%s", apk, asset("apk"), apk)
	case dir == "/usr/bin":
		return "sudo rpm -U " + asset("rpm")
	}
	for _, b := range env.goBins {
		if filepath.Clean(b) == dir {
			return "go install github.com/" + repo + "@latest"
		}
	}
	script := "curl -fsSL https://raw.githubusercontent.com/" + repo + "/main/install.sh | "
	if dir == "/usr/local/bin" {
		return script + "sh"
	}
	return script + "CC360_INSTALL_DIR=" + shellQuote(dir) + " sh"
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
