package jobs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BackfillPay fills empty pay amounts from archived descriptions once per archive.
// Run before serving requests. Completed writes are safe to retry after failure.
func (s *Store) BackfillPay() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	const marker = ".pay-backfill-v1"
	if _, err := os.Stat(filepath.Join(s.dir, marker)); err == nil {
		return 0, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	entries, err := s.list()
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, j := range entries {
		a := &j.Application
		if a.PayMin != "" || a.PayMax != "" {
			continue
		}
		pay := InferPay(j.DescriptionText)
		if pay.PayMin == "" || (a.PayType != "" && a.PayType != pay.PayType) {
			continue
		}
		a.PayMin, a.PayMax, a.PayType = pay.PayMin, pay.PayMax, pay.PayType
		if err := s.write(j); err != nil {
			return updated, fmt.Errorf("backfill pay for %s: %w", j.ID, err)
		}
		updated++
	}
	return updated, s.writeFile(marker, []byte("complete\n"))
}
