package jobs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Application contains editable tracking information, separate from the saved posting.
type Application struct {
	Company        string `json:"company"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	InterviewStage string `json:"interview_stage"`
	InterviewNotes string `json:"interview_notes"`
	NextSteps      string `json:"next_steps"`
}

func (a Application) Validate() error {
	if strings.TrimSpace(a.Company) == "" || strings.TrimSpace(a.Title) == "" || len(a.Company) > 300 || len(a.Title) > 300 {
		return errors.New("Company and job title are required (maximum 300 bytes each)")
	}
	switch a.Status {
	case "applied", "waiting for response", "not moving forward", "interview":
	default:
		return errors.New("Choose a valid application status")
	}
	switch a.InterviewStage {
	case "", "initial screening", "round 1", "round 2", "round 3":
	default:
		return errors.New("Choose a valid interview stage")
	}
	if a.Status == "interview" && a.InterviewStage == "" {
		return errors.New("Choose an interview stage")
	}
	if len(a.InterviewNotes) > 10000 || len(a.NextSteps) > 10000 {
		return errors.New("Interview notes and next steps must be at most 10000 bytes each")
	}
	return nil
}

var validID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Store) Get(id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}
func (s *Store) get(id string) (Job, error) {
	if !validID.MatchString(id) {
		return Job{}, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Job{}, err
	}
	var j Job
	err = json.Unmarshal(b, &j)
	return j, err
}
func (s *Store) UpdateApplication(id string, a Application) (Job, error) {
	if err := a.Validate(); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.get(id)
	if err != nil {
		return Job{}, err
	}
	a.Company = strings.TrimSpace(a.Company)
	a.Title = strings.TrimSpace(a.Title)
	j.Application = a
	return j, s.write(j)
}

// Delete removes both the archived posting and its application details.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID.MatchString(id) {
		return os.ErrNotExist
	}
	if err := os.Remove(filepath.Join(s.dir, id+".json")); err != nil {
		return err
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
