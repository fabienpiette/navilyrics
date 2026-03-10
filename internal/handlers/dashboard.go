package handlers

import (
	"context"
	"net/http"
)

type dashboardData struct {
	ActiveTab  string
	Version    string
	Total      int
	HasLyrics  int
	Missing    int
	Pct        int
}

// Dashboard renders the / page with song counts.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	songs, err := h.nd.AllSongs(ctx)
	if err != nil {
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}

	total := len(songs)
	hasLyrics := 0
	for _, s := range songs {
		if s.HasLyrics {
			hasLyrics++
		}
	}
	missing := total - hasLyrics
	pct := 0
	if total > 0 {
		pct = hasLyrics * 100 / total
	}

	data := dashboardData{
		ActiveTab: "dashboard",
		Version:   h.version,
		Total:     total,
		HasLyrics: hasLyrics,
		Missing:   missing,
		Pct:       pct,
	}
	h.render(w, "dashboard.html", data)
}
