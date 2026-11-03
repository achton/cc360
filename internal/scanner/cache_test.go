package scanner

import (
	"os"
	"testing"
	"time"
)

// An unchanged transcript comes from the cache; any change to its size or
// mtime parses it again.
func TestLoadTranscriptUsesCacheUntilFileChanges(t *testing.T) {
	path := writeJSONL(t, []string{tUser, tBot})
	scanPaths := []string{"/home/user"}

	first := loadTranscript(path, scanPaths, nil)
	if first == nil || first.FileSize == 0 || first.FileModNano == 0 {
		t.Fatalf("parsed session has no stamp: %+v", first)
	}

	cached := *first
	cached.Title = "from cache"
	cache := Cache{path: cached}
	if got := loadTranscript(path, scanPaths, cache); got.Title != "from cache" {
		t.Errorf("unchanged file: Title = %q, want the cached one", got.Title)
	}

	later := time.Unix(0, first.FileModNano).Add(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if got := loadTranscript(path, scanPaths, cache); got.Title == "from cache" {
		t.Error("touched file was not parsed again")
	}
}
