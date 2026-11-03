package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want version
		ok   bool
	}{
		{"v0.9.0", version{0, 9, 0}, true},
		{"0.10.2", version{0, 10, 2}, true},
		{"v0.9.0-3-gabc1234-dirty", version{0, 9, 0}, true},
		{"dev", version{}, false},
		{"v1.2", version{}, false},
		{"", version{}, false},
	}
	for _, tt := range tests {
		got, ok := parseVersion(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
	if !(version{0, 10, 0}).after(version{0, 9, 9}) {
		t.Error("0.10.0 must be after 0.9.9")
	}
}

// fakeGitHub serves tag as the latest release and counts the requests.
func fakeGitHub(t *testing.T, tag string, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("User-Agent") == "" {
			t.Error("request without a User-Agent; GitHub rejects those")
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example/%s"}`, tag, tag)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func testChecker(url string, now *time.Time, dir string) Checker {
	return Checker{
		URL:       url,
		CachePath: filepath.Join(dir, "update-check.json"),
		Client:    http.DefaultClient,
		Now:       func() time.Time { return *now },
	}
}

func TestNewerAsksOncePerDay(t *testing.T) {
	srv, calls := fakeGitHub(t, "v0.10.0", http.StatusOK)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	c := testChecker(srv.URL, &now, t.TempDir())

	rel, ok := c.Newer(context.Background(), "0.9.0")
	if !ok || rel.Version != "0.10.0" || rel.URL != "https://example/v0.10.0" {
		t.Fatalf("got %+v, %v; want 0.10.0", rel, ok)
	}
	now = now.Add(23 * time.Hour)
	if _, ok := c.Newer(context.Background(), "0.9.0"); !ok {
		t.Error("cached answer lost")
	}
	if calls.Load() != 1 {
		t.Errorf("%d requests within a day, want 1", calls.Load())
	}
	now = now.Add(2 * time.Hour)
	c.Newer(context.Background(), "0.9.0")
	if calls.Load() != 2 {
		t.Errorf("%d requests after a day, want 2", calls.Load())
	}
}

func TestNewerIsQuietWhenCurrentOrUnknown(t *testing.T) {
	srv, calls := fakeGitHub(t, "v0.9.0", http.StatusOK)
	now := time.Now()
	c := testChecker(srv.URL, &now, t.TempDir())

	for _, current := range []string{"0.9.0", "v0.9.0-2-gabc", "0.10.0"} {
		if rel, ok := c.Newer(context.Background(), current); ok {
			t.Errorf("current %s: got %+v, want no update", current, rel)
		}
	}
	if _, ok := c.Newer(context.Background(), "dev"); ok {
		t.Error("a dev build must not report an update")
	}
	if calls.Load() != 1 {
		t.Errorf("%d requests, want 1", calls.Load())
	}
}

// Offline or rate limited: stay silent, keep the last known release, and do
// not ask again until the interval has passed.
func TestNewerSurvivesFailures(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	good, _ := fakeGitHub(t, "v0.10.0", http.StatusOK)
	testChecker(good.URL, &now, dir).Newer(context.Background(), "0.9.0")

	now = now.Add(25 * time.Hour)
	bad, calls := fakeGitHub(t, "", http.StatusForbidden)
	c := testChecker(bad.URL, &now, dir)
	if rel, ok := c.Newer(context.Background(), "0.9.0"); !ok || rel.Version != "0.10.0" {
		t.Errorf("got %+v, %v; want the last known release", rel, ok)
	}
	now = now.Add(time.Hour)
	c.Newer(context.Background(), "0.9.0")
	if calls.Load() != 1 {
		t.Errorf("%d requests after a failure, want 1", calls.Load())
	}

	fresh := testChecker(bad.URL, &now, t.TempDir())
	if _, ok := fresh.Newer(context.Background(), "0.9.0"); ok {
		t.Error("a failure with no cache must report no update")
	}
}

// The version is pasted into a shell command, so a tag with anything beyond
// MAJOR.MINOR.PATCH is refused.
func TestNewerRefusesOddTags(t *testing.T) {
	for _, tag := range []string{"v1.2.3-$(id)", "v1.2.3+`id`", "v1.2.3-rc1"} {
		srv, _ := fakeGitHub(t, tag, http.StatusOK)
		now := time.Now()
		c := testChecker(srv.URL, &now, t.TempDir())
		if rel, ok := c.Newer(context.Background(), "0.9.0"); ok {
			t.Errorf("tag %q: got %+v, want no update", tag, rel)
		}
	}
}

func TestCommand(t *testing.T) {
	files := map[string]bool{}
	env := installEnv{
		exists: func(p string) bool { return files[p] },
		goBins: []string{"/home/u/go/bin"},
		goos:   "linux",
		goarch: "amd64",
	}
	tests := []struct {
		name  string
		exe   string
		files []string
		want  string
	}{
		{"homebrew cask", "/opt/homebrew/Caskroom/cc360/0.9.0/cc360", nil,
			"brew upgrade --cask achton/tap/cc360"},
		{"deb", "/usr/bin/cc360", []string{"/var/lib/dpkg/info/cc360.list"},
			"curl -fsSLo /tmp/cc360_1.0.0_linux_amd64.deb https://github.com/achton/cc360/releases/download/v1.0.0/cc360_1.0.0_linux_amd64.deb && sudo dpkg -i /tmp/cc360_1.0.0_linux_amd64.deb"},
		{"apk", "/usr/bin/cc360", []string{"/lib/apk/db/installed"},
			"curl -fsSLo /tmp/cc360_1.0.0_linux_amd64.apk https://github.com/achton/cc360/releases/download/v1.0.0/cc360_1.0.0_linux_amd64.apk && sudo apk add --allow-untrusted /tmp/cc360_1.0.0_linux_amd64.apk"},
		{"rpm", "/usr/bin/cc360", nil,
			"sudo rpm -U https://github.com/achton/cc360/releases/download/v1.0.0/cc360_1.0.0_linux_amd64.rpm"},
		{"go install", "/home/u/go/bin/cc360", nil,
			"GOBIN=/home/u/go/bin go install github.com/achton/cc360@latest"},
		{"install script default dir", "/usr/local/bin/cc360", nil,
			"curl -fsSL https://raw.githubusercontent.com/achton/cc360/main/install.sh | CC360_INSTALL_DIR=/usr/local/bin sh"},
		{"install script own dir", "/home/u/bin/cc360", nil,
			"curl -fsSL https://raw.githubusercontent.com/achton/cc360/main/install.sh | CC360_INSTALL_DIR=/home/u/bin sh"},
		{"install script dir with a space", "/home/u/my bin/cc360", nil,
			"curl -fsSL https://raw.githubusercontent.com/achton/cc360/main/install.sh | CC360_INSTALL_DIR='/home/u/my bin' sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clear(files)
			for _, f := range tt.files {
				files[f] = true
			}
			if got := command(tt.exe, "1.0.0", env); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}
