package web

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
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
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /api/jobs", s.list)
	mux.HandleFunc("POST /api/jobs", s.save)
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
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, 403, "Cross-site requests are not allowed")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.Header.Get("Content-Encoding") != "" {
		fail(w, 415, "Use application/json without content encoding")
		return
	}
	// Decode an object token by token to reject duplicate or unknown URL fields.
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		fail(w, 400, "Expected a JSON object containing url")
		return
	}
	raw := ""
	seen := false
	for d.More() {
		key, err := d.Token()
		if err != nil || key != "url" || seen {
			fail(w, 400, "Expected exactly one url field")
			return
		}
		seen = true
		if err = d.Decode(&raw); err != nil {
			fail(w, 400, "url must be a string")
			return
		}
	}
	if _, err = d.Token(); err != nil {
		fail(w, 400, "Invalid JSON")
		return
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF || !seen || strings.TrimSpace(raw) == "" {
		fail(w, 400, "Expected a single JSON object containing url")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		fail(w, 429, "Too many imports; try again shortly")
		return
	}
	j, err := s.importer.Fetch(r.Context(), raw)
	if err != nil {
		if errors.Is(err, jobs.ErrURL) {
			fail(w, 400, err.Error())
		} else {
			slog.Warn("import failed", "error", err)
			fail(w, 502, "Could not retrieve the posting. Check that it is still public and available.")
		}
		return
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

var page = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Job tracker</title>
<style>body{font:16px system-ui;max-width:960px;margin:48px auto;padding:0 20px;background:#f5f5f2;color:#172b2a}h1{font-size:36px}input{flex:1;min-width:0;padding:14px;border:1px solid #bac4c0;border-radius:8px;font:inherit}form{display:flex;gap:12px}button{padding:12px 20px;background:#176b58;color:white;border:0;border-radius:8px;font:inherit;cursor:pointer}button:disabled{opacity:.6}article{background:white;border:1px solid #ddd;border-radius:12px;padding:24px;margin:18px 0}pre{white-space:pre-wrap;font:inherit;line-height:1.6}small{color:#52645e}#message{min-height:24px}a{color:#176b58}</style>
<h1>Job tracker</h1><p>Keep a copy of the jobs you apply to, even after the posting disappears.</p>
<form id="save"><input id="url" type="url" required aria-label="Job posting URL" placeholder="Paste an Ashby, Greenhouse, or Workday posting URL"><button>Save posting</button></form><p id="message" role="status"></p><main id="jobs"></main>
<script>
const root=document.querySelector('#jobs'),message=document.querySelector('#message'),form=document.querySelector('form');
function el(tag,text){const node=document.createElement(tag);node.textContent=text;return node}
async function load(){const r=await fetch('/api/jobs');if(!r.ok)throw Error('Could not load saved jobs');const data=await r.json();root.replaceChildren();if(!data.length)root.append(el('p','Your saved postings will appear here.'));for(const j of data){const a=el('article','');a.append(el('h2',j.title),el('p',j.company+' · '+j.location),el('small','Saved '+new Date(j.saved_at).toLocaleString()+' · '+j.provider));const d=el('details','');d.append(el('summary','Read saved description'),el('pre',j.description_text));a.append(d);const link=el('a','Original posting');link.href=j.url;link.target='_blank';link.rel='noopener noreferrer';a.append(link);root.append(a)}}
form.addEventListener('submit',async e=>{e.preventDefault();const button=form.querySelector('button');button.disabled=true;message.textContent='Retrieving posting…';try{const r=await fetch('/api/jobs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:document.querySelector('#url').value})});const data=await r.json();if(!r.ok)throw Error(data.error);await load();message.textContent=r.status===201?'Posting saved.':'Already saved — your original copy was preserved.';form.reset()}catch(e){message.textContent=e.message}finally{button.disabled=false}});load().catch(e=>message.textContent=e.message);
</script></html>`))

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.Execute(w, nil); err != nil {
		slog.Error("render page", "error", err)
	}
}
