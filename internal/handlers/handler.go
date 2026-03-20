package handlers

import (
	"bytes"
	"context"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/goscribe"
	"github.com/user/navilyrics/pkg/navidrome"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	nd                 *navidrome.Client
	proc               *lyrics.Processor
	tmpls              map[string]*template.Template
	partials           map[string]*template.Template
	version            string
	runs               *RunStore
	stats              *statsCache
	availableProviders []string
	goscribeEnabled    bool
	goscribeClient     *goscribe.Client // nil when goscribe is not configured
	selfURL            string           // base URL of this navilyrics instance (for webhook callbacks)
}

// New creates a Handler and kicks off a background stats refresh.
func New(nd *navidrome.Client, proc *lyrics.Processor, tmpls, partials map[string]*template.Template, version string, availableProviders []string, goscribeEnabled bool, goscribeClient *goscribe.Client, selfURL string) *Handler {
	h := &Handler{
		nd: nd, proc: proc, tmpls: tmpls, partials: partials,
		version: version, runs: newRunStore(), stats: &statsCache{},
		availableProviders: availableProviders,
		goscribeEnabled:    goscribeEnabled,
		goscribeClient:     goscribeClient,
		selfURL:            selfURL,
	}
	go func() {
		if err := h.stats.refresh(context.Background(), nd); err != nil {
			log.Printf("stats: boot refresh failed: %v", err)
		}
	}()
	return h
}

// render executes a full page template (enters via base.html).
func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	t, ok := h.tmpls[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// renderPartial executes a standalone partial template (no base.html wrapper).
func (h *Handler) renderPartial(w http.ResponseWriter, name string, data any) {
	t, ok := h.partials[name]
	if !ok {
		http.Error(w, "partial not found: "+name, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		http.Error(w, "partial error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// ParseTemplates builds a per-page template map (each page clones base.html).
// songs.html also gets the songs_rows_body partial injected so it can use
// {{template "songs_rows_body" .}} in its initial render.
func ParseTemplates(fsys fs.FS) (map[string]*template.Template, error) {
	base, err := template.New("base.html").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	}).ParseFS(fsys, "templates/base.html")
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
		// songs.html uses {{template "songs_rows_body"}} defined in the partial.
		if name == "songs.html" {
			t, err = t.ParseFS(fsys, "templates/partials/songs_rows.html")
			if err != nil {
				return nil, err
			}
		}
		tmpls[name] = t
	}
	return tmpls, nil
}

// ParsePartials builds a map of standalone partial templates (no base.html).
func ParsePartials(fsys fs.FS) (map[string]*template.Template, error) {
	files, err := fs.Glob(fsys, "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	partials := make(map[string]*template.Template, len(files))
	for _, f := range files {
		name := path.Base(f)
		t, err := template.New(name).Funcs(template.FuncMap{
			"lower": strings.ToLower,
		}).ParseFS(fsys, f)
		if err != nil {
			return nil, err
		}
		partials[name] = t
	}
	return partials, nil
}
