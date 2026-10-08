package jobs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

// InterviewDate records a screening or interview milestone.
type InterviewDate struct {
	Stage    string `json:"stage"`
	Date     string `json:"date"`
	Time     string `json:"time,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

func (d InterviewDate) TimeLabel() string {
	if d.Time == "" {
		return ""
	}
	parsed, err := time.Parse("15:04", d.Time)
	if err != nil {
		return ""
	}
	return parsed.Format("3:04 PM") + " (" + d.Timezone + ")"
}

func (d InterviewDate) Label() string {
	if d.Stage == "initial screening" {
		return "Initial screening"
	}
	return "Round " + strings.TrimPrefix(d.Stage, "round ")
}

var roundStage = regexp.MustCompile(`^round [1-9][0-9]{0,5}$`)

func validStage(stage string) bool {
	return stage == "initial screening" || roundStage.MatchString(stage)
}

// InterviewDates includes older saved milestones without changing archived files.
func (a Application) InterviewDates() []InterviewDate {
	if a.Milestones != nil {
		return a.Milestones
	}
	dates := []InterviewDate{}
	for i, date := range []string{a.ScreeningDate, a.Round1Date, a.Round2Date, a.Round3Date} {
		if date != "" {
			dates = append(dates, InterviewDate{Stage: []string{"initial screening", "round 1", "round 2", "round 3"}[i], Date: date})
		}
	}
	return dates
}

// Application contains editable tracking information, separate from the saved posting.
type Application struct {
	PayMin        string          `json:"pay_min"`
	PayMax        string          `json:"pay_max"`
	PayType       string          `json:"pay_type"`
	Milestones    []InterviewDate `json:"milestones"`
	AppliedDate   string          `json:"applied_date"`
	ResponseDate  string          `json:"response_date"`
	ScreeningDate string          `json:"screening_date"`
	Round1Date    string          `json:"round_1_date"`
	Round2Date    string          `json:"round_2_date"`
	Round3Date    string          `json:"round_3_date"`

	Company        string `json:"company"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	InterviewStage string `json:"interview_stage"`
	GeneralNotes   string `json:"general_notes"`
	InterviewNotes string `json:"interview_notes"`
	NextSteps      string `json:"next_steps"`
}

var payAmount = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,8})(?:\.[0-9]{1,2})?$`)

func (a Application) PayLabel() string {
	if a.PayMin == "" && a.PayMax == "" {
		return ""
	}
	amount := a.PayMin
	if amount == "" {
		amount = "Up to $" + a.PayMax
	} else {
		amount = "$" + amount
		if a.PayMax != "" && a.PayMax != a.PayMin {
			amount += " – $" + a.PayMax
		} else if a.PayMax == "" {
			amount += "+"
		}
	}
	unit := " / year"
	if a.PayType == "hourly" {
		unit = " / hour"
	}
	return amount + unit
}

func (a Application) Validate() error {
	if a.PayType != "" && a.PayType != "salary" && a.PayType != "hourly" {
		return errors.New("Choose salary or hourly pay")
	}
	values := make([]int64, 2)
	for i, amount := range []string{a.PayMin, a.PayMax} {
		if amount == "" {
			continue
		}
		if !payAmount.MatchString(amount) {
			return errors.New("Pay amounts must be positive numbers with at most two decimal places")
		}
		parts := strings.SplitN(amount, ".", 2)
		whole, _ := strconv.ParseInt(parts[0], 10, 64)
		values[i] = whole * 100
		if len(parts) == 2 {
			cents, _ := strconv.ParseInt(parts[1], 10, 64)
			if len(parts[1]) == 1 {
				cents, _ = strconv.ParseInt(parts[1]+"0", 10, 64)
			}
			values[i] += cents
		}
		if values[i] <= 0 {
			return errors.New("Pay amounts must be greater than zero")
		}
	}
	if (a.PayMin != "" || a.PayMax != "") && a.PayType == "" {
		return errors.New("Choose salary or hourly pay")
	}
	if a.PayMin != "" && a.PayMax != "" && values[0] > values[1] {
		return errors.New("Minimum pay must be on or below maximum pay")
	}

	if len(a.Milestones) > 100 {
		return errors.New("Use at most 100 interview dates")
	}
	for _, milestone := range a.Milestones {
		if !validStage(milestone.Stage) {
			return errors.New("Choose initial screening or an interview round")
		}
		if _, err := time.Parse("2006-01-02", milestone.Date); err != nil {
			return errors.New("Interview dates must be valid dates in YYYY-MM-DD format")
		}
		if milestone.Time != "" {
			if _, err := time.Parse("15:04", milestone.Time); err != nil {
				return errors.New("Interview times must use HH:MM format")
			}
			if milestone.Timezone == "" || len(milestone.Timezone) > 100 {
				return errors.New("Choose a timezone for the interview time")
			}
			location, err := time.LoadLocation(milestone.Timezone)
			if err != nil {
				return errors.New("Use a valid timezone such as America/Denver")
			}
			value := milestone.Date + " " + milestone.Time
			parsed, err := time.ParseInLocation("2006-01-02 15:04", value, location)
			if err != nil || parsed.Format("2006-01-02 15:04") != value {
				return errors.New("This interview time does not exist in the selected timezone due to daylight saving time")
			}
		} else if milestone.Timezone != "" {
			return errors.New("Add an interview time before choosing a timezone")
		}

	}
	for _, date := range []string{a.AppliedDate, a.ResponseDate, a.ScreeningDate, a.Round1Date, a.Round2Date, a.Round3Date} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return errors.New("Dates must be valid dates in YYYY-MM-DD format")
			}
		}
	}
	if strings.TrimSpace(a.Company) == "" || strings.TrimSpace(a.Title) == "" || len(a.Company) > 300 || len(a.Title) > 300 {
		return errors.New("Company and job title are required (maximum 300 bytes each)")
	}
	switch a.Status {
	case "", "applied", "waiting for response", "not moving forward", "interview":
	default:
		return errors.New("Choose a valid application status")
	}
	if a.InterviewStage != "" && !validStage(a.InterviewStage) {
		return errors.New("Choose a valid interview stage")
	}
	if a.Status == "interview" && a.InterviewStage == "" {
		return errors.New("Choose an interview stage")
	}
	if len(a.GeneralNotes) > 10000 {
		return errors.New("General notes must be at most 10000 bytes")
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
	if a.Status != j.Application.Status || a.InterviewStage != j.Application.InterviewStage {
		a.DefaultDates(Today())
	}
	j.Application = a
	return j, s.write(j)
}

// UpdateDates changes only the supplied dates, including explicitly cleared dates.
// It leaves tracking details and the archived posting untouched.
func (s *Store) UpdateDates(id string, dates map[string]string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.get(id)
	if err != nil {
		return Job{}, err
	}
	for name, value := range dates {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return Job{}, errors.New("Dates must be valid dates in YYYY-MM-DD format")
			}
		}
		switch name {
		case "applied_date":
			j.Application.AppliedDate = value
		case "response_date":
			j.Application.ResponseDate = value
		case "screening_date":
			j.Application.ScreeningDate = value
		case "round_1_date":
			j.Application.Round1Date = value
		case "round_2_date":
			j.Application.Round2Date = value
		case "round_3_date":
			j.Application.Round3Date = value
		default:
			return Job{}, errors.New("Unknown date field")
		}
	}
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

// Today returns the calendar date in the user's configured local timezone.
func Today() string {
	location, err := time.LoadLocation("America/Denver")
	if err != nil {
		location = time.UTC
	}
	return time.Now().In(location).Format("2006-01-02")
}

// DefaultDates fills dates only for events represented by the application.
func (a *Application) DefaultDates(today string) {
	if a.AppliedDate == "" && a.Status != "" {
		a.AppliedDate = today
	}
	if a.ResponseDate == "" && (a.Status == "interview" || a.Status == "not moving forward") {
		a.ResponseDate = today
	}
	if a.Status == "interview" && a.Milestones == nil && roundStage.MatchString(a.InterviewStage) && a.InterviewStage != "round 1" && a.InterviewStage != "round 2" && a.InterviewStage != "round 3" {
		a.Milestones = a.InterviewDates()
	}
	if a.Status == "interview" && a.Milestones != nil {
		for _, date := range a.Milestones {
			if date.Stage == a.InterviewStage {
				return
			}
		}
		if validStage(a.InterviewStage) && len(a.Milestones) < 100 {
			a.Milestones = append(a.Milestones, InterviewDate{Stage: a.InterviewStage, Date: today})
		}
		return
	}
	if a.Status == "interview" {
		var date *string
		switch a.InterviewStage {
		case "initial screening":
			date = &a.ScreeningDate
		case "round 1":
			date = &a.Round1Date
		case "round 2":
			date = &a.Round2Date
		case "round 3":
			date = &a.Round3Date
		}
		if date != nil && *date == "" {
			*date = today
		}
	}
}
