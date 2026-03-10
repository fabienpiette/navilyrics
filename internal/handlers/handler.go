package handlers

import (
	"html/template"
	"io/fs"
	"net/http"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	nd      *navidrome.Client
	proc    *lyrics.Processor
	tmpls   *template.Template
	version string
}

// New creates a Handler. tmpls is a parsed template set (base + all pages).
func New(nd *navidrome.Client, proc *lyrics.Processor, tmpls *template.Template, version string) *Handler {
	return &Handler{nd: nd, proc: proc, tmpls: tmpls, version: version}
}

// baseData returns the common fields used by every page template.
func (h *Handler) baseData(activeTab string) map[string]any {
	return map[string]any{
		"ActiveTab": activeTab,
		"Version":   h.version,
	}
}

// render executes a named template writing to w. On error it sends 500.
func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpls.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// ParseTemplates builds a template set from the given FS (typically web.FS).
func ParseTemplates(fsys fs.FS) (*template.Template, error) {
	return template.ParseFS(fsys, "templates/*.html")
}
