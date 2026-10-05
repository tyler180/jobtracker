package jobs

import (
	"crypto/rand"
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

// Store keeps immutable posting snapshots and editable application details.
// The first successful import wins for a URL.
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
	identity := []byte(j.URL)
	if j.URL == "" {
		identity = make([]byte, 32)
		if _, err := rand.Read(identity); err != nil {
			return Job{}, false, err
		}
	}
	sum := sha256.Sum256(identity)
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
	if err := s.write(j); err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}

func (s *Store) write(j Job) error {
	path := filepath.Join(s.dir, j.ID+".json")
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".posting-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return err
	}
	return nil
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
