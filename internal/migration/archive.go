// Package migration imports a verified, read-only JSON archive into PostgreSQL.
// It does not change the running application's storage or authentication.
package migration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var jobID = regexp.MustCompile(`^[0-9a-f]{64}$`)

const maxArchiveBytes = 128 << 20

type record struct {
	id  string
	raw []byte
}

// Archive is an in-memory snapshot. Loading it never extracts or changes files.
type Archive struct {
	sha256  string
	records []record
	files   int
}

// Summary contains no descriptions, notes, credentials, or identity details.
type Summary struct {
	ArchiveSHA256 string `json:"archive_sha256"`
	Files         int    `json:"files"`
	Records       int    `json:"records"`
}

func (a *Archive) Summary() Summary {
	return Summary{a.sha256, a.files, len(a.records)}
}

// Load validates a tar.gz backup against an independently recorded checksum.
// Unknown JSON fields are retained; duplicate JSON keys are rejected because
// jsonb would otherwise silently discard the earlier value.
func Load(path, expectedSHA256 string) (*Archive, error) {
	if !jobID.MatchString(expectedSHA256) {
		return nil, errors.New("a 64-character lowercase backup SHA-256 is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxArchiveBytes {
		return nil, errors.New("compressed archive exceeds 128 MiB")
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != expectedSHA256 {
		return nil, errors.New("backup checksum mismatch")
	}
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxArchiveBytes + 1}
	tr := tar.NewReader(limited)
	a := &Archive{sha256: expectedSHA256}
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag == tar.TypeDir && (name == "" || name == ".") {
			continue
		}
		if h.Typeflag != tar.TypeReg || strings.ContainsAny(name, `/\\`) || seen[name] {
			return nil, errors.New("archive contains duplicate, nested, or non-regular entries")
		}
		seen[name] = true
		a.files++
		if a.files > 10000 || h.Size < 0 || h.Size > 2<<20 {
			return nil, errors.New("archive entry limit exceeded")
		}
		raw, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		if name == ".pay-backfill-v1" {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !strings.HasSuffix(name, ".json") || !jobID.MatchString(id) {
			return nil, errors.New("unexpected file in archive")
		}
		if !utf8.Valid(raw) {
			return nil, errors.New("job JSON is not valid UTF-8")
		}
		if err := uniqueJSON(raw); err != nil {
			return nil, fmt.Errorf("invalid job JSON: %w", err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
			return nil, errors.New("job JSON must be an object")
		}
		var actualID string
		if json.Unmarshal(doc["id"], &actualID) != nil || actualID != id {
			return nil, errors.New("job ID does not match filename")
		}
		a.records = append(a.records, record{id, raw})
	}
	// Drain gzip to verify its trailer/CRC, including bytes after tar end markers.
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return nil, fmt.Errorf("gzip integrity: %w", err)
	}
	if limited.N <= 0 {
		return nil, errors.New("expanded archive exceeds 128 MiB")
	}
	if len(a.records) == 0 {
		return nil, errors.New("archive has no job records")
	}
	sort.Slice(a.records, func(i, j int) bool { return a.records[i].id < a.records[j].id })
	return a, nil
}

func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 100 {
			return errors.New("JSON nesting exceeds 100 levels")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok || keys[s] {
					return errors.New("duplicate JSON key")
				}
				keys[s] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON content")
	}
	return nil
}
