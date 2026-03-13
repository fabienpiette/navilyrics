package lyrics

import (
	"context"

	"github.com/user/navilyrics/pkg/genius"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/netease"
)

// ProviderResult is the common result type returned by all providers.
type ProviderResult struct {
	PlainLyrics  string
	SyncedLyrics string
	Instrumental bool
}

// Provider is the interface all lyrics sources must implement.
type Provider interface {
	// Name returns the provider's identifier (e.g. "lrclib", "netease", "genius").
	Name() string
	// Search finds lyrics. album may be empty. Returns (zero, false, nil) if not found.
	Search(ctx context.Context, artist, title, album string, duration float64) (ProviderResult, bool, error)
}

// lrclibProvider wraps lrclib.Client with exact-Get-then-fuzzy-Search cascade.
type lrclibProvider struct{ c *lrclib.Client }

// NewLRCLibProvider creates a Provider backed by lrclib.Client.
func NewLRCLibProvider(c *lrclib.Client) Provider { return &lrclibProvider{c: c} }

func (p *lrclibProvider) Name() string { return "lrclib" }

func (p *lrclibProvider) Search(ctx context.Context, artist, title, album string, duration float64) (ProviderResult, bool, error) {
	resp, ok, err := p.c.Get(ctx, artist, title, album, duration)
	if err != nil {
		return ProviderResult{}, false, err
	}
	if !ok {
		resp, ok, err = p.c.Search(ctx, artist, title, duration)
		if err != nil {
			return ProviderResult{}, false, err
		}
	}
	if !ok {
		return ProviderResult{}, false, nil
	}
	return ProviderResult{
		PlainLyrics:  resp.PlainLyrics,
		SyncedLyrics: resp.SyncedLyrics,
		Instrumental: resp.Instrumental,
	}, true, nil
}

// netEaseProvider wraps netease.Client.
type netEaseProvider struct{ c *netease.Client }

// NewNetEaseProvider creates a Provider backed by netease.Client.
func NewNetEaseProvider(c *netease.Client) Provider { return &netEaseProvider{c: c} }

func (p *netEaseProvider) Name() string { return "netease" }

func (p *netEaseProvider) Search(ctx context.Context, artist, title, _ string, duration float64) (ProviderResult, bool, error) {
	resp, ok, err := p.c.Search(ctx, artist, title, duration)
	if err != nil {
		return ProviderResult{}, false, err
	}
	if !ok {
		return ProviderResult{}, false, nil
	}
	return ProviderResult{
		PlainLyrics:  resp.PlainLyrics,
		SyncedLyrics: resp.SyncedLyrics,
	}, true, nil
}

// geniusProvider wraps genius.Client. SyncedLyrics is always empty.
type geniusProvider struct{ c *genius.Client }

// NewGeniusProvider creates a Provider backed by genius.Client.
func NewGeniusProvider(c *genius.Client) Provider { return &geniusProvider{c: c} }

func (p *geniusProvider) Name() string { return "genius" }

func (p *geniusProvider) Search(ctx context.Context, artist, title, _ string, _ float64) (ProviderResult, bool, error) {
	resp, ok, err := p.c.Search(ctx, artist, title)
	if err != nil {
		return ProviderResult{}, false, err
	}
	if !ok {
		return ProviderResult{}, false, nil
	}
	return ProviderResult{PlainLyrics: resp.PlainLyrics}, true, nil
}
