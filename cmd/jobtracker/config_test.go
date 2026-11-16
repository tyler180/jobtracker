package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func clearConfig(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "PUBLIC_URL", "STORAGE_BACKEND"} {
		t.Setenv(k, "")
	}
}

func TestRejectPartialConfiguration(t *testing.T) {
	clearConfig(t)
	t.Setenv("DATABASE_URL", "postgres://must-not-fall-back")
	if _, _, err := buildHandler(context.Background(), false); err == nil {
		t.Fatal("silently used unauthenticated file store")
	}
	t.Setenv("STORAGE_BACKEND", "postgres")
	t.Setenv("DATABASE_URL", "")
	if _, _, err := buildHandler(context.Background(), false); err == nil {
		t.Fatal("postgres mode accepted missing database")
	}
}

func TestDemoNeverOpensPrivateArchiveOrDatabase(t *testing.T) {
	clearConfig(t)
	private := filepath.Join(t.TempDir(), "must-not-open")
	t.Setenv("DATA_DIR", private)
	t.Setenv("DATABASE_URL", "invalid")
	t.Setenv("OIDC_ISSUER_URL", "invalid")
	t.Setenv("STORAGE_BACKEND", "postgres")
	h, cleanup, err := buildHandler(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/jobs", nil))
	if w.Code != 403 {
		t.Fatal("demo permits writes")
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatal("demo touched private archive")
	}
}
