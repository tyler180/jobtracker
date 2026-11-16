package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyler180/jobtracker/internal/auth"
	"github.com/tyler180/jobtracker/internal/jobs"
	"github.com/tyler180/jobtracker/internal/migration"
	"github.com/tyler180/jobtracker/internal/postgres"
	"github.com/tyler180/jobtracker/internal/web"
	"golang.org/x/oauth2"
)

type grant struct{ subject, nonce, challenge, mode string }
type provider struct {
	server                *httptest.Server
	key                   *rsa.PrivateKey
	mu                    sync.Mutex
	grants                map[string]grant
	nextSubject, nextMode string
}

func fakeProvider(t *testing.T) *provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &provider{key: key, grants: map[string]grant{}, nextSubject: "alice"}
	p.server = httptest.NewTLSServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	return p
}

func (p *provider) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize", "token_endpoint": p.server.URL + "/token", "jwks_uri": p.server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	case "/keys":
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
	case "/authorize":
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("client_id") != "test-client" {
			http.Error(w, "invalid authorization", 400)
			return
		}
		p.mu.Lock()
		code := fmt.Sprintf("code-%d-%s", len(p.grants), q.Get("state"))
		p.grants[code] = grant{p.nextSubject, q.Get("nonce"), q.Get("code_challenge"), p.nextMode}
		p.mu.Unlock()
		u, _ := url.Parse(q.Get("redirect_uri"))
		v := u.Query()
		v.Set("code", code)
		v.Set("state", q.Get("state"))
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), 302)
	case "/token":
		_ = r.ParseForm()
		p.mu.Lock()
		g, ok := p.grants[r.Form.Get("code")]
		delete(p.grants, r.Form.Get("code"))
		p.mu.Unlock()
		id, secret, hasBasic := r.BasicAuth()
		if !hasBasic || id != "test-client" || secret != "test-secret" || !ok || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != g.challenge {
			http.Error(w, "invalid exchange", 400)
			return
		}
		claims := map[string]any{"iss": p.server.URL, "sub": g.subject, "aud": "test-client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": g.nonce}
		signingKey := p.key
		switch g.mode {
		case "nonce":
			claims["nonce"] = "wrong"
		case "audience":
			claims["aud"] = "another-client"
		case "issuer":
			claims["iss"] = "https://attacker.example"
		case "expired":
			claims["exp"] = time.Now().Add(-time.Hour).Unix()
		case "signature":
			signingKey, _ = rsa.GenerateKey(rand.Reader, 2048)
		}
		b, _ := json.Marshal(claims)
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signingKey}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
		signed, _ := signer.Sign(b)
		token, _ := signed.CompactSerialize()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access", "token_type": "Bearer", "expires_in": 3600, "id_token": token})
	default:
		http.NotFound(w, r)
	}
}

func localDatabase(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("JOBTRACKER_MIGRATION_TEST_URL")
	if dsn == "" {
		t.Skip("set JOBTRACKER_MIGRATION_TEST_URL for local PostgreSQL/OIDC integration")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "::1") || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("requires loopback test database")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	dbName := "jobtracker_auth_" + suffix + "_test"
	roleName := "jobtracker_runtime_" + suffix
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE ROLE "+pgx.Identifier{roleName}.Sanitize()+" LOGIN"); err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	owner, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		owner.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		_, _ = admin.Exec(ctx, "DROP ROLE "+pgx.Identifier{roleName}.Sanitize())
		_ = admin.Close(ctx)
	})
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.InitializeSchema(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	grantee := pgx.Identifier{roleName}.Sanitize()
	for _, q := range []string{
		"GRANT USAGE ON SCHEMA jobtracker TO " + grantee,
		"GRANT SELECT ON jobtracker.schema_versions TO " + grantee,
		"GRANT SELECT,INSERT ON jobtracker.users TO " + grantee,
		"GRANT SELECT,INSERT,DELETE ON jobtracker.jobs TO " + grantee,
		"GRANT UPDATE(document) ON jobtracker.jobs TO " + grantee,
		"GRANT SELECT,INSERT,DELETE ON jobtracker.sessions,jobtracker.login_attempts TO " + grantee,
	} {
		if _, err := owner.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	cfg = cfg.Copy()
	cfg.ConnConfig.User = roleName
	cfg.MaxConns = 5
	runtime, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	if _, err := runtime.Exec(ctx, "CREATE TABLE jobtracker.forbidden(id int)"); err == nil {
		t.Fatal("runtime role can create schema objects")
	}
	if _, err := runtime.Exec(ctx, "SELECT * FROM jobtracker.archive_imports"); err == nil {
		t.Fatal("runtime role can access migration ledger")
	}
	if _, err := runtime.Exec(ctx, "UPDATE jobtracker.jobs SET source_json='changed'::bytea"); err == nil {
		t.Fatal("runtime role can overwrite original archive bytes")
	}
	return owner, runtime
}

func TestOIDCAndApplicationRoutes(t *testing.T) {
	owner, pool := localDatabase(t)
	p := fakeProvider(t)
	app := httptest.NewUnstartedServer(nil)
	publicURL := "https://" + app.Listener.Addr().String()
	ctx := oidc.ClientContext(context.Background(), p.server.Client())
	a, err := auth.New(ctx, pool, auth.Config{Issuer: p.server.URL, ClientID: "test-client", ClientSecret: "test-secret", PublicURL: publicURL})
	if err != nil {
		t.Fatal(err)
	}
	app.Config.Handler = a.Handler(web.NewWithStore(postgres.New(pool), nil))
	app.StartTLS()
	defer app.Close()
	client := p.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request := func(method, path, body string, cookies []*http.Cookie, origin string, headers map[string]string) (int, []byte, http.Header, []*http.Cookie) {
		t.Helper()
		target := path
		if strings.HasPrefix(path, "/") {
			target = app.URL + path
		}
		r, err := http.NewRequest(method, target, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, b, res.Header, res.Cookies()
	}
	begin := func(subject, mode string) (string, []*http.Cookie) {
		t.Helper()
		p.mu.Lock()
		p.nextSubject = subject
		p.nextMode = mode
		p.mu.Unlock()
		status, _, h, cookies := request("GET", "/auth/login", "", nil, "", nil)
		if status != 302 || len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
			t.Fatal("unsafe login cookie", status)
		}
		status, _, h, _ = request("GET", h.Get("Location"), "", nil, "", nil)
		if status != 302 {
			t.Fatal("fake provider authorization failed", status)
		}
		return h.Get("Location"), cookies
	}
	login := func(subject string) []*http.Cookie {
		t.Helper()
		callback, cookies := begin(subject, "")
		status, _, _, returned := request("GET", callback, "", cookies, "", nil)
		if status != 303 {
			t.Fatal("login failed", status)
		}
		var session []*http.Cookie
		for _, c := range returned {
			if c.Name == "__Host-jobtracker-session" {
				if !c.Secure || !c.HttpOnly || c.MaxAge != 43200 {
					t.Fatal("unsafe session cookie")
				}
				session = append(session, c)
			}
		}
		if len(session) != 1 {
			t.Fatal("missing session cookie")
		}
		status, _, _, _ = request("GET", callback, "", cookies, "", nil)
		if status != 400 {
			t.Fatal("replayed callback accepted", status)
		}
		return session
	}
	for _, path := range []string{"/api/jobs", "/api/jobs/preview", "/api/jobs/pdf", "/api/jobs/anything/application", "/api/jobs/anything/dates", "/api/jobs/anything"} {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			status, _, _, _ := request(method, path, `{}`, nil, "", nil)
			if status != 401 {
				t.Fatal("unauthenticated API accessible", method, path, status)
			}
		}
	}
	status, _, h, _ := request("GET", "/", "", nil, "", nil)
	if status != 303 || h.Get("Location") != "/auth/login" {
		t.Fatal("missing login redirect")
	}
	status, _, _, _ = request("GET", "/healthz", "", nil, "", nil)
	if status != 200 {
		t.Fatal("health probe blocked")
	}
	callback, cookies := begin("alice", "")
	status, _, _, _ = request("GET", callback, "", []*http.Cookie{{Name: cookies[0].Name, Value: strings.Repeat("A", 43)}}, "", nil)
	if status != 400 {
		t.Fatal("browser binding failed")
	}
	status, _, _, _ = request("GET", callback, "", cookies, "", nil)
	if status != 303 {
		t.Fatal("wrong-browser attempt consumed valid state")
	}
	callback, cookies = begin("alice", "")
	if _, err := owner.Exec(context.Background(), "UPDATE jobtracker.login_attempts SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	status, _, _, _ = request("GET", callback, "", cookies, "", nil)
	if status != 400 {
		t.Fatal("expired login state accepted")
	}
	for _, mode := range []string{"nonce", "signature", "issuer", "audience", "expired"} {
		callback, cookies := begin("alice", mode)
		status, _, _, _ := request("GET", callback, "", cookies, "", nil)
		if status != 400 {
			t.Fatal("invalid identity token accepted", mode, status)
		}
	}
	alice, bob := login("alice"), login("bob")
	status, meBody, _, _ := request("GET", "/auth/me", "", alice, "", nil)
	var me map[string]string
	if err := json.Unmarshal(meBody, &me); err != nil || status != 200 || me["subject"] != "alice" || me["issuer"] != p.server.URL || me["user_id"] == "" {
		t.Fatal("verified identity endpoint incorrect", status, err)
	}
	body := `{"company":"Private company","title":"Owner role","url":"https://example.com/shared-job","description_text":"Private archived description","status":"applied","general_notes":"private notes"}`
	status, b, _, _ := request("POST", "/api/jobs", body, alice, app.URL, nil)
	if status != 201 {
		t.Fatalf("save failed: %d %s", status, b)
	}
	var saved jobs.Job
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	status, b, _, _ = request("GET", "/api/jobs", "", bob, "", map[string]string{"X-Forwarded-User": "alice", "X-Auth-Request-User": "alice"})
	if status != 200 || string(strings.TrimSpace(string(b))) != "[]" {
		t.Fatal("user spoofing or cross-user listing", status)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/jobs/" + saved.ID, ""},
		{"PUT", "/api/jobs/" + saved.ID + "/application", `{"company":"Other","title":"Other"}`},
		{"PATCH", "/api/jobs/" + saved.ID + "/dates", `{"applied_date":"2026-11-12"}`},
		{"DELETE", "/api/jobs/" + saved.ID, ""},
	} {
		status, _, _, _ := request(tc.method, tc.path, tc.body, bob, app.URL, nil)
		if status != 404 {
			t.Fatal("cross-user route accessible", tc.method, status)
		}
	}
	for _, origin := range []string{"", "https://attacker.example"} {
		status, _, _, _ := request("DELETE", "/api/jobs/"+saved.ID, "", alice, origin, nil)
		if status != 403 {
			t.Fatal("CSRF allowed", origin, status)
		}
	}
	status, _, _, _ = request("POST", "/api/jobs", body, alice, app.URL, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if status != 403 {
		t.Fatal("cross-site mutation allowed")
	}
	status, b, _, _ = request("POST", "/api/jobs", body, alice, app.URL, nil)
	if status != 200 {
		t.Fatal("duplicate did not retain snapshot", status)
	}
	status, b, _, _ = request("POST", "/api/jobs", strings.Replace(body, "Owner role", "Bob role", 1), bob, app.URL, nil)
	if status != 201 {
		t.Fatal("different user cannot save same posting", status)
	}
	// Preserve unknown fields while editing known application fields.
	if _, err := owner.Exec(context.Background(), `UPDATE jobtracker.jobs SET document=document || '{"future_top":"keep"}'::jsonb WHERE document->>'title'='Owner role'`); err != nil {
		t.Fatal(err)
	}
	status, b, _, _ = request("PUT", "/api/jobs/"+saved.ID+"/application", `{"company":"Private company","title":"Edited role","status":"applied","general_notes":"Updated"}`, alice, app.URL, nil)
	if status != 200 {
		t.Fatalf("owner edit failed %d %s", status, b)
	}
	status, b, _, _ = request("PATCH", "/api/jobs/"+saved.ID+"/dates", `{"applied_date":"2026-11-12"}`, alice, app.URL, nil)
	if status != 200 {
		t.Fatal("owner date update failed", status)
	}
	var future string
	if err := owner.QueryRow(context.Background(), `SELECT document->>'future_top' FROM jobtracker.jobs WHERE document->'application'->>'title'='Edited role'`).Scan(&future); err != nil || future != "keep" {
		t.Fatal("future field lost", err)
	}
	status, b, _, _ = request("GET", "/jobs/"+saved.ID, "", alice, "", nil)
	if status != 200 || !strings.Contains(string(b), "Private archived description") {
		t.Fatal("owner description missing")
	}
	status, b, _, _ = request("GET", "/", "", alice, "", nil)
	if status != 200 || !strings.Contains(string(b), "Edited role") || !strings.Contains(string(b), "/auth/account") {
		t.Fatal("authenticated cards failed")
	}
	status, b, _, _ = request("GET", "/new", "", alice, "", nil)
	if status != 200 || !strings.Contains(string(b), "/auth/account") {
		t.Fatal("authenticated form failed")
	}
	status, b, _, _ = request("GET", "/auth/account", "", alice, "", nil)
	if status != 200 || !strings.Contains(string(b), "Sign out") {
		t.Fatal("account page missing")
	}
	status, _, _, _ = request("GET", "/auth/logout", "", alice, "", nil)
	if status != 405 {
		t.Fatal("GET can sign out")
	}
	status, _, _, _ = request("POST", "/auth/logout", "", alice, app.URL, nil)
	if status != 303 {
		t.Fatal("logout failed", status)
	}
	status, signedOut, _, _ := request("GET", "/auth/signed-out", "", nil, "", nil)
	if status != 200 || !strings.Contains(string(signedOut), "You’re signed out") {
		t.Fatal("logout starts automatic reauthentication")
	}
	status, _, _, _ = request("GET", "/api/jobs", "", alice, "", nil)
	if status != 401 {
		t.Fatal("logged-out cookie still valid")
	}
	if _, err := owner.Exec(context.Background(), "UPDATE jobtracker.sessions SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	status, _, _, _ = request("GET", "/api/jobs", "", bob, "", nil)
	if status != 401 {
		t.Fatal("expired session accepted")
	}

	if backup := os.Getenv("JOBTRACKER_MIGRATION_BACKUP"); backup != "" {
		a, err := migration.Load(backup, os.Getenv("JOBTRACKER_MIGRATION_BACKUP_SHA256"))
		if err != nil {
			t.Fatal(err)
		}
		c, err := owner.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		result, err := migration.Apply(context.Background(), c.Conn(), a, migration.Identity{Issuer: p.server.URL, Subject: "existing-owner"}, false)
		c.Release()
		if err != nil {
			t.Fatal(err)
		}
		existing := login("existing-owner")
		status, b, _, _ = request("GET", "/api/jobs", "", existing, "", nil)
		var records []jobs.Job
		if err := json.Unmarshal(b, &records); err != nil || status != 200 || len(records) != result.Verified {
			t.Fatal("migrated records not linked to verified login", status, err)
		}
		empty := login("new-empty-user")
		status, b, _, _ = request("GET", "/api/jobs", "", empty, "", nil)
		if status != 200 || strings.TrimSpace(string(b)) != "[]" {
			t.Fatal("new login inherited archive")
		}
		t.Logf("verified real OIDC login sees exactly %d migrated records; new account sees zero", len(records))
	}
	t.Log("passed signed OIDC exchange with PKCE, state/browser binding/expiry/replay, invalid-token rejection, restricted database role, authenticated HTTP ownership, CSRF, logout and session expiry")
}
