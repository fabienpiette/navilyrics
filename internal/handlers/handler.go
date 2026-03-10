package handlers

import (
	"html/template"
	"io/fs"
	"net/http"
	"path"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	nd      *navidrome.Client
	proc    *lyrics.Processor
	tmpls   map[string]*template.Template
	version string
}

// New creates a Handler. tmpls is the per-page template map from ParseTemplates.
func New(nd *navidrome.Client, proc *lyrics.Processor, tmpls map[string]*template.Template, version string) *Handler {
	return &Handler{nd: nd, proc: proc, tmpls: tmpls, version: version}
}

// render executes the named page template (always entering via base.html).
func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	t, ok := h.tmpls[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// ParseTemplates builds a per-page template map from the given FS.
// Each page gets its own clone of base.html so {{define}} blocks don't
// overwrite each other across pages (the standard Go template pitfall).
func ParseTemplates(fsys fs.FS) (map[string]*template.Template, error) {
	base, err := template.ParseFS(fsys, "templates/base.html")
	if err != nil {
		return nil, err
	}

	pages, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, err
	}

	tmpls := make(map[string]*template.Template, len(pages))
	for _, p := range pages {
		name := path.Base(p)
		if name == "base.html" {
			continue
		}
		t, err := template.Must(base.Clone()).ParseFS(fsys, p)
		if err != nil {
			return nil, err
		}
		tmpls[name] = t
	}
	return tmpls, nil
}
