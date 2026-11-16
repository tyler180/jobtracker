package migration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testFile struct {
	name, body string
	kind       byte
}

func backupFile(t *testing.T, files ...testFile) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		kind := f.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Size: int64(len(f.body)), Mode: 0600, Typeflag: kind}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return p, hex.EncodeToString(sum[:])
}

func jobFile(digit, extra string) testFile {
	id := strings.Repeat(digit, 64)
	return testFile{name: "./" + id + ".json", body: fmt.Sprintf(`{"id":%q,"title":"Preserve me","application":{"general_notes":"private","milestones":null},"future_field":{"number":1234567890123456789}%s}`, id, extra)}
}

func loadFixture(t *testing.T, files ...testFile) *Archive {
	t.Helper()
	p, sha := backupFile(t, files...)
	a, err := Load(p, sha)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestArchiveValidation(t *testing.T) {
	good := jobFile("2", "")
	for _, tt := range []struct {
		name  string
		files []testFile
	}{
		{"duplicate filename", []testFile{good, good}},
		{"duplicate JSON key", []testFile{jobFile("2", `,"title":"replacement"`)}},
		{"nested duplicate key", []testFile{jobFile("2", `,"nested":{"x":1,"x":2}`)}},
		{"wrong ID", []testFile{{name: good.name, body: `{"id":"wrong"}`}}},
		{"traversal", []testFile{{name: "../record.json", body: good.body}}},
		{"symlink", []testFile{{name: "link", kind: tar.TypeSymlink}}},
		{"unknown file", []testFile{{name: "secret.txt", body: "bad"}}},
		{"empty", nil},
		{"invalid UTF8", []testFile{{name: good.name, body: strings.Replace(good.body, "Preserve me", string([]byte{255}), 1)}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, sha := backupFile(t, tt.files...)
			if _, err := Load(p, sha); err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
	p, sha := backupFile(t, good, testFile{name: "./.pay-backfill-v1", body: "done"})
	if _, err := Load(p, strings.Repeat("0", 64)); err == nil {
		t.Fatal("accepted bad checksum")
	}
	a, err := Load(p, sha)
	if err != nil {
		t.Fatal(err)
	}
	if a.Summary().Records != 1 || a.Summary().Files != 2 || string(a.records[0].raw) != good.body {
		t.Fatal("archive not preserved")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-5] ^= 1
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if _, err := Load(p, hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("accepted corrupt gzip trailer")
	}
}
