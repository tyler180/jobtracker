package web

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/tyler180/jobtracker/internal/jobs"
)

func decodeFields(w http.ResponseWriter, r *http.Request, allowed []string) (map[string]string, error) {
	limit := int64(32768)
	for _, name := range allowed {
		if name == "description_text" {
			limit = 128 << 10
		}
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return nil, errors.New("Invalid JSON object")
	}
	out := map[string]string{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := t.(string)
		if !ok {
			return nil, errors.New("Invalid field")
		}
		accepted := false
		for _, name := range allowed {
			if key == name {
				accepted = true
			}
		}
		if _, exists := out[key]; exists || !accepted {
			return nil, errors.New("Unknown or repeated field")
		}
		var value *string
		if err := d.Decode(&value); err != nil || value == nil {
			return nil, errors.New("Fields must be strings")
		}
		out[key] = *value
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("Expected one object")
	}
	return out, nil
}
func applicationFrom(f map[string]string) jobs.Application {
	return jobs.Application{AppliedDate: f["applied_date"], ResponseDate: f["response_date"], ScreeningDate: f["screening_date"], Round1Date: f["round_1_date"], Round2Date: f["round_2_date"], Round3Date: f["round_3_date"], Company: f["company"], Title: f["title"], Status: f["status"], InterviewStage: f["interview_stage"], InterviewNotes: f["interview_notes"], NextSteps: f["next_steps"]}
}

func (s *Server) updateDates(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, http.StatusForbidden, "Cross-site requests are not allowed")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.Header.Get("Content-Encoding") != "" {
		fail(w, http.StatusUnsupportedMediaType, "Use application/json without content encoding")
		return
	}
	f, err := decodeFields(w, r, []string{"applied_date", "response_date", "screening_date", "round_1_date", "round_2_date", "round_3_date"})
	if err != nil || len(f) == 0 {
		fail(w, http.StatusBadRequest, "Expected date fields in a single JSON object")
		return
	}
	for _, value := range f {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				fail(w, http.StatusBadRequest, "Dates must be valid dates in YYYY-MM-DD format")
				return
			}
		}
	}
	j, err := s.store.UpdateDates(r.PathValue("id"), f)
	if errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "Saved job not found")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "Could not update dates")
		return
	}
	reply(w, http.StatusOK, j)
}
func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, 403, "Cross-site requests are not allowed")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.Header.Get("Content-Encoding") != "" {
		fail(w, 415, "Use application/json without content encoding")
		return
	}
	f, err := decodeFields(w, r, []string{"company", "title", "status", "interview_stage", "interview_notes", "next_steps", "applied_date", "response_date", "screening_date", "round_1_date", "round_2_date", "round_3_date"})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	previous, readErr := s.store.Get(r.PathValue("id"))
	if errors.Is(readErr, os.ErrNotExist) {
		fail(w, 404, "Saved job not found")
		return
	}
	if readErr != nil {
		fail(w, 500, "Could not read application")
		return
	}
	if _, present := f["applied_date"]; !present {
		f["applied_date"] = previous.Application.AppliedDate
	}
	if _, present := f["response_date"]; !present {
		f["response_date"] = previous.Application.ResponseDate
	}
	if _, present := f["screening_date"]; !present {
		f["screening_date"] = previous.Application.ScreeningDate
	}
	if _, present := f["round_1_date"]; !present {
		f["round_1_date"] = previous.Application.Round1Date
	}
	if _, present := f["round_2_date"]; !present {
		f["round_2_date"] = previous.Application.Round2Date
	}
	if _, present := f["round_3_date"]; !present {
		f["round_3_date"] = previous.Application.Round3Date
	}
	a := applicationFrom(f)
	if err := a.Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	j, err := s.store.UpdateApplication(r.PathValue("id"), a)
	if errors.Is(err, os.ErrNotExist) {
		fail(w, 404, "Saved job not found")
		return
	}
	if err != nil {
		fail(w, 500, "Could not update application")
		return
	}
	reply(w, 200, j)
}

var descriptionPage = template.Must(template.New("description").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title><style>body{font:16px system-ui;max-width:900px;margin:40px auto;padding:20px}pre{white-space:pre-wrap;font:inherit;line-height:1.6}</style><nav><a href="/entries">Back to entries</a> · <a href="/">Manage applications</a></nav><h1>{{.Title}}</h1><p>{{.Company}} · {{.Location}}</p><p>Saved {{.SavedAt}}</p><p><a href="{{.URL}}" target="_blank" rel="noopener noreferrer">Original posting</a></p><pre>{{.DescriptionText}}</pre></html>`))

func (s *Server) description(w http.ResponseWriter, r *http.Request) {
	j, err := s.store.Get(r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(w, 500, "Could not read saved description")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	descriptionPage.Execute(w, j)
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, http.StatusForbidden, "Cross-site requests are not allowed")
		return
	}
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(w, http.StatusNotFound, "Saved job not found")
		} else {
			fail(w, http.StatusInternalServerError, "Could not delete job")
		}
		return
	}
	reply(w, http.StatusOK, map[string]bool{"deleted": true})
}
