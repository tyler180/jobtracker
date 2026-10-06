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
	d.DisallowUnknownFields()
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
		if key == "milestones" {
			var dates []jobs.InterviewDate
			if err := d.Decode(&dates); err != nil || dates == nil {
				return nil, errors.New("Milestones must be an array")
			}
			b, _ := json.Marshal(dates)
			out[key] = string(b)
			continue
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
	a := jobs.Application{PayMin: f["pay_min"], PayMax: f["pay_max"], PayType: f["pay_type"], AppliedDate: f["applied_date"], ResponseDate: f["response_date"], ScreeningDate: f["screening_date"], Round1Date: f["round_1_date"], Round2Date: f["round_2_date"], Round3Date: f["round_3_date"], Company: f["company"], Title: f["title"], Status: f["status"], InterviewStage: f["interview_stage"], InterviewNotes: f["interview_notes"], NextSteps: f["next_steps"]}
	if raw, present := f["milestones"]; present {
		json.Unmarshal([]byte(raw), &a.Milestones)
		a.ScreeningDate = ""
		a.Round1Date = ""
		a.Round2Date = ""
		a.Round3Date = ""
	}
	return a
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
	f, err := decodeFields(w, r, []string{"company", "title", "status", "interview_stage", "interview_notes", "next_steps", "pay_min", "pay_max", "pay_type", "milestones", "applied_date", "response_date", "screening_date", "round_1_date", "round_2_date", "round_3_date"})
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
	if _, present := f["milestones"]; !present && previous.Application.Milestones != nil {
		b, _ := json.Marshal(previous.Application.Milestones)
		f["milestones"] = string(b)
	}
	for key, value := range map[string]string{"pay_min": previous.Application.PayMin, "pay_max": previous.Application.PayMax, "pay_type": previous.Application.PayType} {
		if _, present := f[key]; !present {
			f[key] = value
		}
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

var descriptionPage = template.Must(template.New("description").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="theme-color" content="#102621"><title>{{.Title}} · Job tracker</title><style>:root{--ink:#17211f;--muted:#66736f;--canvas:#f4f5ef;--surface:#fffefa;--line:#dfe4dc;--pine:#102621;--pine-2:#1d453b;--coral:#e66f45;--shadow:0 18px 55px rgba(20,44,38,.08)}*{box-sizing:border-box}body{margin:0;background:var(--canvas);color:var(--ink);font:16px/1.75 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}a{color:var(--pine-2);font-weight:700;text-underline-offset:3px}a:hover{color:#b84928}a:focus-visible{outline:3px solid rgba(230,111,69,.45);outline-offset:3px}.topbar{background:var(--pine);color:#fff}.topbar-inner{max-width:980px;margin:auto;padding:18px 28px;display:flex;align-items:center;justify-content:space-between;gap:24px}.brand{display:flex;align-items:center;gap:10px;color:#fff;text-decoration:none}.brand-mark{display:grid;place-items:center;width:36px;height:36px;border-radius:11px;background:var(--coral)}.brand-mark svg{width:20px}.nav{display:flex;gap:16px}.nav a{color:#dce8e3;font-size:.9rem;text-decoration:none}.shell{max-width:980px;margin:auto;padding:34px 28px 80px}.posting-header{padding:38px 42px;border-radius:25px 25px 0 0;background:var(--pine);color:#fff}.eyebrow{margin:0 0 9px;color:#a9d2c2;font-size:.76rem;font-weight:800;letter-spacing:.13em;text-transform:uppercase}.posting-header h1{max-width:760px;margin:0;font-size:clamp(2rem,5vw,3.8rem);line-height:1.04;letter-spacing:-.045em}.posting-meta{margin:17px 0 0;color:#c8d9d3}.posting-actions{display:flex;flex-wrap:wrap;gap:16px;margin:18px 0 0}.posting-actions a{color:#fff}.posting-body{padding:38px 42px;border:1px solid var(--line);border-top:0;border-radius:0 0 25px 25px;background:var(--surface);box-shadow:var(--shadow)}pre{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;line-height:1.75}@media(max-width:650px){.topbar-inner{padding:14px 16px;align-items:flex-start;flex-direction:column}.shell{padding:18px 14px 50px}.posting-header,.posting-body{padding:26px 21px}.posting-header h1{font-size:2.35rem}}@media print{.topbar{display:none}.shell{max-width:none;padding:0}.posting-header{padding:0 0 25px;background:#fff;color:#000}.posting-header .eyebrow{display:none}.posting-meta{color:#333}.posting-actions{display:none}.posting-body{padding:0;border:0;box-shadow:none}}</style><header class="topbar"><div class="topbar-inner"><a class="brand" href="/"><span class="brand-mark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M7 7.5V6a2 2 0 0 1 2-2h6a2 2 0 0 1 2 2v1.5M5 8h14a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2Z"/><path d="M3 13h18M10 12v2h4v-2"/></svg></span>Job tracker</a><nav class="nav" aria-label="Main navigation"><a href="/">Applications</a><a href="/new">Add entry</a></nav></div></header><main class="shell"><article><header class="posting-header"><p class="eyebrow">Archived job description</p><h1>{{.Title}}</h1><p class="posting-meta">{{.Company}}{{if .Location}} · {{.Location}}{{end}} · Saved {{.SavedAt}}</p><p class="posting-actions"><a href="/">← Back to applications</a>{{if .URL}}<a href="{{.URL}}" target="_blank" rel="noopener noreferrer">Open original posting ↗</a>{{end}}</p></header><div class="posting-body"><pre>{{.DescriptionText}}</pre></div></article></main></html>`))

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
