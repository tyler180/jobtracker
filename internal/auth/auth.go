// Package auth implements OIDC sign-in with server-side, revocable sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyler180/jobtracker/internal/identity"
	"golang.org/x/oauth2"
)

const sessionCookie = "__Host-jobtracker-session"
const loginCookie = "__Host-jobtracker-login"

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	PublicURL    string
}

type Auth struct {
	pool     *pgxpool.Pool
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
	origin   string
	host     string
}

// New discovers a trusted, configured provider. No insecure production option
// exists: HTTPS is required, including when TLS terminates at a reverse proxy.
func New(ctx context.Context, pool *pgxpool.Pool, cfg Config) (*Auth, error) {
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("PUBLIC_URL must be an HTTPS origin without a path")
	}
	issuer, err := url.Parse(cfg.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("OIDC issuer, client ID, and client secret are required")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	// Tests may inject a trusted TLS client through oidc.ClientContext.
	if c, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok {
		client = c
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), cfg.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery failed")
	}
	origin := u.Scheme + "://" + u.Host
	endpoint := provider.Endpoint()
	for _, raw := range []string{endpoint.AuthURL, endpoint.TokenURL} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return nil, errors.New("OIDC endpoints must use HTTPS")
		}
	}
	return &Auth{pool: pool, origin: origin, host: u.Host, client: client, verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}), oauth: oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: endpoint, RedirectURL: origin + "/auth/callback", Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}}, nil
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func tokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func validToken(raw string) bool {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	return err == nil && len(b) == 32
}

func cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func reject(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// Handler requires verified sessions for every application route, including
// imports, previews, PDFs, and descriptions. Only health and auth routes are public.
func (a *Auth) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCtx, requestCancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer requestCancel()
		r = r.WithContext(requestCtx)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/healthz" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}
		// Do not derive the public origin or identity from forwarded headers.
		if r.Host != a.host {
			reject(w, 400, "Unexpected host")
			return
		}
		switch r.URL.Path {
		case "/auth/signed-out":
			if r.Method != http.MethodGet {
				reject(w, 405, "Use GET")
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
			_ = signedOutPage.Execute(w, nil)
			return
		case "/auth/login":
			if r.Method != http.MethodGet {
				reject(w, 405, "Use GET")
				return
			}
			a.login(w, r)
			return
		case "/auth/callback":
			if r.Method != http.MethodGet {
				reject(w, 405, "Use GET")
				return
			}
			a.callback(w, r)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil || !validToken(c.Value) {
			a.unauthorized(w, r)
			return
		}
		var userID string
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		err = a.pool.QueryRow(ctx, `SELECT user_id::text FROM jobtracker.sessions WHERE token_hash=$1 AND expires_at>now()`, tokenHash(c.Value)).Scan(&userID)
		cancel()
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				reject(w, 503, "Sign-in is temporarily unavailable")
				return
			}
			cookie(w, sessionCookie, "", -1)
			a.unauthorized(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Browser requests must originate from this configured public origin.
			// No cookie-authenticated mutation is accepted without Origin.
			if r.Header.Get("Origin") != a.origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				reject(w, 403, "Same-origin request required")
				return
			}
		}
		if r.URL.Path == "/auth/logout" {
			if r.Method != http.MethodPost {
				reject(w, 405, "Use POST to sign out")
				return
			}
			if _, err := a.pool.Exec(r.Context(), `DELETE FROM jobtracker.sessions WHERE token_hash=$1`, tokenHash(c.Value)); err != nil {
				reject(w, 503, "Could not sign out; please retry")
				return
			}
			cookie(w, sessionCookie, "", -1)
			http.Redirect(w, r, "/auth/signed-out", http.StatusSeeOther)
			return
		}
		if r.URL.Path == "/auth/account" {
			if r.Method != http.MethodGet {
				reject(w, 405, "Use GET")
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
			_ = accountPage.Execute(w, nil)
			return
		}
		if r.URL.Path == "/auth/me" {
			if r.Method != http.MethodGet {
				reject(w, 405, "Use GET")
				return
			}
			var issuer, subject string
			if err := a.pool.QueryRow(r.Context(), `SELECT issuer,subject FROM jobtracker.users WHERE id=$1`, userID).Scan(&issuer, &subject); err != nil {
				reject(w, 503, "Could not read account identity")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"user_id": userID, "issuer": issuer, "subject": subject})
			return
		}
		next.ServeHTTP(w, r.WithContext(identity.WithUser(r.Context(), userID)))
	})
}

func (a *Auth) unauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/auth/me" {
		reject(w, 401, "Sign in to continue")
		return
	}
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	state, browser, nonce, verifier := randomToken(), randomToken(), randomToken(), oauth2.GenerateVerifier()
	if _, err := a.pool.Exec(ctx, `DELETE FROM jobtracker.login_attempts WHERE expires_at<=now()`); err != nil {
		reject(w, 503, "Could not start sign-in")
		return
	}
	// Keep pending unauthenticated state bounded even if login URLs are abused.
	tag, err := a.pool.Exec(ctx, `INSERT INTO jobtracker.login_attempts(state_hash,browser_hash,nonce,verifier,expires_at) SELECT $1,$2,$3,$4,now()+interval '10 minutes' WHERE (SELECT count(*) FROM jobtracker.login_attempts)<10000`, tokenHash(state), tokenHash(browser), nonce, verifier)
	if err != nil {
		reject(w, 503, "Could not start sign-in")
		return
	}
	if tag.RowsAffected() != 1 {
		reject(w, 429, "Too many pending sign-ins; try again later")
		return
	}
	cookie(w, loginCookie, browser, 600)
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, err := r.Cookie(loginCookie)
	if err != nil || !validToken(c.Value) || !validToken(q.Get("state")) || len(q["state"]) != 1 || len(q["code"]) != 1 || len(q.Get("code")) > 4096 || q.Get("code") == "" {
		reject(w, 400, "Invalid or expired sign-in; start again")
		return
	}
	cookie(w, loginCookie, "", -1)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var nonce, verifier string
	// State is browser-bound, expires after ten minutes, and can be used once.
	err = a.pool.QueryRow(ctx, `DELETE FROM jobtracker.login_attempts WHERE state_hash=$1 AND browser_hash=$2 AND expires_at>now() RETURNING nonce,verifier`, tokenHash(q.Get("state")), tokenHash(c.Value)).Scan(&nonce, &verifier)
	if err != nil {
		reject(w, 400, "Invalid or expired sign-in; start again")
		return
	}
	tokens, err := a.oauth.Exchange(oidc.ClientContext(ctx, a.client), q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		reject(w, 400, "Sign-in could not be verified; start again")
		return
	}
	raw, ok := tokens.Extra("id_token").(string)
	if !ok {
		reject(w, 400, "Identity token missing")
		return
	}
	id, err := a.verifier.Verify(oidc.ClientContext(ctx, a.client), raw)
	if err != nil || id.Subject == "" || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 {
		reject(w, 400, "Sign-in could not be verified; start again")
		return
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		reject(w, 503, "Could not complete sign-in")
		return
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	uid := fmt.Sprintf("%x", uuid())
	if _, err = tx.Exec(ctx, `INSERT INTO jobtracker.users(id,issuer,subject) VALUES($1,$2,$3) ON CONFLICT(issuer,subject) DO NOTHING`, uid, id.Issuer, id.Subject); err != nil {
		reject(w, 503, "Could not complete sign-in")
		return
	}
	if err = tx.QueryRow(ctx, `SELECT id::text FROM jobtracker.users WHERE issuer=$1 AND subject=$2`, id.Issuer, id.Subject).Scan(&uid); err != nil {
		reject(w, 503, "Could not complete sign-in")
		return
	}
	if _, err = tx.Exec(ctx, `DELETE FROM jobtracker.sessions WHERE expires_at<=now()`); err != nil {
		reject(w, 503, "Could not complete sign-in")
		return
	}
	if old, err := r.Cookie(sessionCookie); err == nil && validToken(old.Value) {
		if _, err = tx.Exec(ctx, `DELETE FROM jobtracker.sessions WHERE token_hash=$1`, tokenHash(old.Value)); err != nil {
			reject(w, 503, "Could not complete sign-in")
			return
		}
	}
	session := randomToken()
	if _, err = tx.Exec(ctx, `INSERT INTO jobtracker.sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '12 hours')`, tokenHash(session), uid); err != nil {
		reject(w, 503, "Could not complete sign-in")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		reject(w, 503, "Could not complete sign-in; try again")
		return
	}
	cookie(w, sessionCookie, session, 12*60*60)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func uuid() []byte {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return b
}

var accountPage = template.Must(template.New("account").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Your account · Job tracker</title><style>body{font:18px system-ui;background:#f4f5ef;color:#17211f;max-width:680px;margin:10vh auto;padding:24px}button,a{font:inherit}button{background:#102621;color:white;border:0;border-radius:8px;padding:12px 20px;cursor:pointer}</style><h1>Your account</h1><p>You’re signed in. Your applications are private to your account.</p><p><a href="/">Back to applications</a></p><form method="post" action="/auth/logout"><button>Sign out of Jobtracker</button></form><p>Signing out here leaves your Authentik session active.</p></html>`))

var signedOutPage = template.Must(template.New("signed-out").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Signed out · Job tracker</title><style>body{font:18px system-ui;background:#f4f5ef;color:#17211f;max-width:680px;margin:10vh auto;padding:24px}a{color:#1d453b}</style><h1>You’re signed out</h1><p>Your Jobtracker session has ended. Your Authentik session may still be active.</p><p><a href="/auth/login">Sign in again</a></p></html>`))
