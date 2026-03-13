package lyrics_test

import (
	"context"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
)

// stubProvider implements Provider for testing.
type stubProvider struct {
	name   string
	result lyrics.ProviderResult
	ok     bool
	calls  int
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Search(_ context.Context, _, _, _ string, _ float64) (lyrics.ProviderResult, bool, error) {
	s.calls++
	return s.result, s.ok, nil
}

func TestProviderResult_fields(t *testing.T) {
	pr := lyrics.ProviderResult{
		PlainLyrics:  "plain",
		SyncedLyrics: "[00:01.00] synced",
		Instrumental: true,
	}
	if pr.PlainLyrics != "plain" {
		t.Errorf("PlainLyrics = %q", pr.PlainLyrics)
	}
	if !pr.Instrumental {
		t.Error("Instrumental should be true")
	}
}
