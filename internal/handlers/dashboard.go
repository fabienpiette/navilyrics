package handlers

import (
	"context"
	"log"
	"net/http"
	"sync"

	"github.com/user/navilyrics/pkg/navidrome"
)

// statsCache holds pre-computed library stats, refreshed at boot and on demand.
type statsCache struct {
	mu        sync.RWMutex
	ready     bool
	total     int
	hasLyrics int
	missing   int
	pct       int
}

func (sc *statsCache) refresh(ctx context.Context, nd *navidrome.Client) error {
	songs, err := nd.AllSongs(ctx)
	if err != nil {
		return err
	}
	total := len(songs)
	has := 0
	for _, s := range songs {
		if s.HasLyrics {
			has++
		}
	}
	pct := 0
	if total > 0 {
		pct = has * 100 / total
	}
	sc.mu.Lock()
	sc.total = total
	sc.hasLyrics = has
	sc.missing = total - has
	sc.pct = pct
	sc.ready = true
	sc.mu.Unlock()
	return nil
}

func (sc *statsCache) snapshot() (total, has, missing, pct int, ready bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.total, sc.hasLyrics, sc.missing, sc.pct, sc.ready
}

type dashboardData struct {
	ActiveTab string
	Version   string
	Total     int
	HasLyrics int
	Missing   int
	Pct       int
	Ready     bool
}

// Dashboard renders the / page with cached song counts (no blocking Navidrome call).
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	total, has, missing, pct, ready := h.stats.snapshot()
	h.render(w, "dashboard.html", dashboardData{
		ActiveTab: "dashboard",
		Version:   h.version,
		Total:     total,
		HasLyrics: has,
		Missing:   missing,
		Pct:       pct,
		Ready:     ready,
	})
}

// DashboardStats recomputes library stats and returns only the stats partial.
// Called by the HTMX refresh button.
func (h *Handler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	if err := h.stats.refresh(r.Context(), h.nd); err != nil {
		log.Printf("stats: refresh failed: %v", err)
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}
	total, has, missing, pct, ready := h.stats.snapshot()
	h.renderPartial(w, "dashboard_stats.html", dashboardData{
		Total:     total,
		HasLyrics: has,
		Missing:   missing,
		Pct:       pct,
		Ready:     ready,
	})
}
