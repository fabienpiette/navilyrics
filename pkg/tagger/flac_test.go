package tagger_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/user/navilyrics/pkg/tagger"
)

// generateFLACFixture copies a minimal valid FLAC file from the mewkiz/flac
// module test data into testdata/sample.flac.
func generateFLACFixture() {
	// Locate the silence.flac file bundled with the mewkiz/flac module.
	// runtime.GOROOT() gives the Go installation; module cache is elsewhere,
	// but we can find it via go env GOPATH.
	_ = runtime.GOARCH // avoid unused-import lint if we switch approach

	// Try the module cache path directly.
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		// Fall back: use the well-known default location.
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}

	candidates := []string{
		filepath.Join(gopath, "pkg", "mod", "github.com", "mewkiz", "flac@v1.0.13", "meta", "testdata", "silence.flac"),
		// asdf-style Go installations store packages under the installs dir.
		filepath.Join(os.Getenv("HOME"), ".asdf", "installs", "golang", "1.25.3", "packages", "pkg", "mod", "github.com", "mewkiz", "flac@v1.0.13", "meta", "testdata", "silence.flac"),
	}

	for _, src := range candidates {
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		_ = os.WriteFile("testdata/sample.flac", data, 0644)
		return
	}
	// If neither candidate worked, the test will be skipped below.
}

func TestFLACTagger_RoundTrip(t *testing.T) {
	src, err := os.ReadFile("testdata/sample.flac")
	if err != nil {
		t.Skipf("no FLAC fixture: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), "test.flac")
	if err := os.WriteFile(tmp, src, 0644); err != nil {
		t.Fatal(err)
	}

	tgr, err := tagger.ForFile(tmp)
	if err != nil {
		t.Fatal(err)
	}

	const plain = "FLAC line one\nFLAC line two"
	const synced = "[00:01.00] FLAC line one\n[00:02.00] FLAC line two"

	if err := tgr.WriteLyrics(tmp, plain, synced); err != nil {
		t.Fatalf("WriteLyrics: %v", err)
	}

	tgr2, err := tagger.ForFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	gotPlain, gotSynced, err := tgr2.ReadLyrics(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if gotPlain != plain {
		t.Errorf("plain: want %q, got %q", plain, gotPlain)
	}
	if gotSynced != synced {
		t.Errorf("synced: want %q, got %q", synced, gotSynced)
	}
}
