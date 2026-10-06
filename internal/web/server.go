package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/tyler180/jobtracker/internal/jobs"
)

type Fetcher interface {
	Fetch(context.Context, string) (jobs.Job, error)
}
type Server struct {
	store    *jobs.Store
	importer Fetcher
	slots    chan struct{}
}

func New(store *jobs.Store, importer Fetcher) http.Handler {
	s := &Server{store: store, importer: importer, slots: make(chan struct{}, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /{$}", s.entries)
	mux.HandleFunc("GET /new", s.home)
	mux.HandleFunc("GET /edit", s.home)
	mux.HandleFunc("GET /entries", s.entries)
	mux.HandleFunc("GET /api/jobs", s.list)
	mux.HandleFunc("POST /api/jobs", s.save)
	mux.HandleFunc("POST /api/jobs/preview", s.preview)
	mux.HandleFunc("POST /api/jobs/pdf", s.pdfPreview)
	mux.HandleFunc("PUT /api/jobs/{id}/application", s.update)
	mux.HandleFunc("PATCH /api/jobs/{id}/dates", s.updateDates)
	mux.HandleFunc("GET /jobs/{id}", s.description)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.delete)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]string{"error": message})
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.List()
	if err != nil {
		slog.Error("read archive", "error", err)
		fail(w, 500, "Could not read saved jobs")
		return
	}
	reply(w, 200, v)
}
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	s.importPosting(w, r, false)
}
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	s.importPosting(w, r, true)
}
func (s *Server) importPosting(w http.ResponseWriter, r *http.Request, preview bool) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, 403, "Cross-site requests are not allowed")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.Header.Get("Content-Encoding") != "" {
		fail(w, 415, "Use application/json without content encoding")
		return
	}
	allowed := []string{"url", "description_text", "company", "title", "status", "interview_stage", "interview_notes", "next_steps", "pay_min", "pay_max", "pay_type", "milestones", "applied_date", "response_date", "screening_date", "round_1_date", "round_2_date", "round_3_date"}
	if preview {
		allowed = []string{"url", "description_text"}
	}
	fields, err := decodeFields(w, r, allowed)
	if err != nil || (strings.TrimSpace(fields["url"]) == "" && strings.TrimSpace(fields["description_text"]) == "") {
		fail(w, 400, "Expected a single JSON object containing a posting URL or job description and valid application fields")
		return
	}
	raw := fields["url"]
	var application jobs.Application
	tracking := false
	for key := range fields {
		if key != "url" && key != "description_text" {
			tracking = true
		}
	}
	if tracking && !preview {
		application = applicationFrom(fields)
		candidate := application
		if strings.TrimSpace(candidate.Company) == "" {
			candidate.Company = "Pending imported company"
		}
		if strings.TrimSpace(candidate.Title) == "" {
			candidate.Title = "Pending imported title"
		}
		if err := candidate.Validate(); err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		fail(w, 429, "Too many imports; try again shortly")
		return
	}
	var j jobs.Job
	if strings.TrimSpace(fields["description_text"]) != "" {
		if preview {
			j, err = jobs.ManualPreview(raw, fields["description_text"])
		} else {
			j, err = jobs.Manual(raw, fields["company"], fields["title"], fields["description_text"])
		}
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
	} else {
		j, err = s.importer.Fetch(r.Context(), raw)
	}
	if err != nil {
		// A blocked Upstart request can still offer editable URL-derived fields.
		// This is preview metadata only, never a saved posting or description.
		if preview {
			fallback, fallbackErr := jobs.ManualPreview(raw, "")
			if fallbackErr == nil && fallback.Company == "Upstart" {
				reply(w, 200, struct {
					jobs.Job
					DescriptionRequired bool `json:"description_required"`
				}{fallback, true})
				return
			}
		}
		if errors.Is(err, jobs.ErrURL) {
			fail(w, 400, err.Error())
		} else {
			slog.Warn("import failed", "error", err)
			fail(w, 502, "Could not retrieve the posting. Check that it is still public and available, or paste its description and enter the company and job title.")
		}
		return
	}
	if preview {
		j.Application = jobs.InferPay(j.DescriptionText)
		reply(w, 200, j)
		return
	}
	if tracking {
		if _, minSet := fields["pay_min"]; !minSet {
			if _, maxSet := fields["pay_max"]; !maxSet {
				suggestion := jobs.InferPay(j.DescriptionText)
				application.PayMin, application.PayMax = suggestion.PayMin, suggestion.PayMax
				if application.PayType == "" {
					application.PayType = suggestion.PayType
				}
			}
		}
		if strings.TrimSpace(application.Company) == "" {
			application.Company = strings.TrimSpace(j.Company)
		}
		if strings.TrimSpace(application.Title) == "" {
			application.Title = strings.TrimSpace(j.Title)
		}
		if err := application.Validate(); err != nil {
			fail(w, 400, "Could not extract valid company and job title fields. Enter them manually.")
			return
		}
		application.DefaultDates(jobs.Today())
		j.Application = application
	}
	j, created, err := s.store.Save(j)
	if err != nil {
		slog.Error("save failed", "error", err)
		fail(w, 500, "Could not save the posting")
		return
	}
	status := 200
	if created {
		status = 201
	}
	reply(w, status, j)
}

//go:embed page.html
var pageHTML string
var page = template.Must(template.New("home").Parse(pageHTML))

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.Execute(w, nil); err != nil {
		slog.Error("render page", "error", err)
	}
}
