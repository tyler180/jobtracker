package web

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed entries.html
var entriesHTML string

var entriesPage = template.Must(template.New("entries").Parse(entriesHTML))

func (s *Server) entries(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.List()
	if err != nil {
		slog.Error("read entries", "error", err)
		fail(w, http.StatusInternalServerError, "Could not read saved entries")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := entriesPage.Execute(w, entries); err != nil {
		slog.Error("render entries", "error", err)
	}
}
