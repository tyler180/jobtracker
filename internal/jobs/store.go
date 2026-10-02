package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store keeps immutable snapshots. The first successful import wins for a URL.
// Use one server replica per archive directory.
type Store struct {
	dir string
	mu  sync.Mutex
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}
func (s *Store) Save(j Job) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := sha256.Sum256([]byte(j.URL))
	j.ID = hex.EncodeToString(sum[:])
	path := filepath.Join(s.dir, j.ID+".json")
	if b, e := os.ReadFile(path); e == nil {
		var old Job
		if e = json.Unmarshal(b, &old); e != nil {
			return Job{}, false, e
		}
		return old, false, nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return Job{}, false, e
	}
	j.SavedAt = time.Now().UTC()
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return Job{}, false, err
	}
	f, err := os.CreateTemp(s.dir, ".posting-*")
	if err != nil {
		return Job{}, false, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return Job{}, false, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return Job{}, false, err
	}
	if err = f.Close(); err != nil {
		return Job{}, false, err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return Job{}, false, err
	}
	d, err := os.Open(s.dir)
	if err != nil {
		return Job{}, false, err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}
func (s *Store) List() ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := []Job{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var j Job
		if err = json.Unmarshal(b, &j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SavedAt.After(out[j].SavedAt) })
	return out, nil
}
