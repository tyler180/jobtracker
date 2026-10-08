package jobs

import "fmt"

// SeedDemo populates a fresh demo archive with entirely fictional records.
func SeedDemo(store *Store) error {
	companies := []string{"Juniper Harbor Labs", "Copper Finch Systems", "Lantern Grove Software", "Cobalt Meadow Cloud", "Paper Kite Analytics", "Silver Fern Works"}
	titles := []string{"Senior Platform Engineer", "Site Reliability Engineer", "Backend Engineer", "Cloud Infrastructure Engineer", "Developer Experience Engineer", "Infrastructure Consultant"}
	statuses := []string{"interview", "waiting for response", "applied", "not moving forward", "", "interview"}
	for i, company := range companies {
		a := Application{Company: company, Title: titles[i], Status: statuses[i], PayMin: "140000", PayMax: "180000", PayType: "salary", Milestones: []InterviewDate{}}
		if a.Status != "" {
			a.AppliedDate = fmt.Sprintf("2026-09-%02d", 12+i)
		}
		if a.Status == "interview" {
			a.InterviewStage = "round 1"
			a.ResponseDate = "2026-09-22"
			a.Milestones = []InterviewDate{{Stage: "initial screening", Date: "2026-09-24", Time: "10:00", Timezone: "America/Denver"}, {Stage: "round 1", Date: "2026-10-02", Time: "13:30", Timezone: "America/Denver"}}
			a.InterviewNotes = "Fictional interview: discussed deployment automation, incident response, and how the team supports developers."
			a.NextSteps = "Example next step: prepare a platform design walkthrough."
		}
		if a.Status == "not moving forward" {
			a.ResponseDate = "2026-09-28"
			a.NextSteps = "Example outcome: position closed; keep notes for future opportunities."
		}
		if i == 5 {
			a.PayMin = "85"
			a.PayMax = "110"
			a.PayType = "hourly"
		}
		if err := a.Validate(); err != nil {
			return err
		}
		_, _, err := store.Save(Job{Company: company, Title: titles[i], Location: "Remote · United States", Provider: "demo", Application: a, DescriptionText: fmt.Sprintf("DEMO — FICTIONAL JOB POSTING\n\n%s\n%s\n\nAbout the role\nBuild reliable services and improve the developer experience for a fictional software team.\n\nResponsibilities\n• Automate infrastructure and delivery workflows.\n• Improve service monitoring and incident response.\n• Collaborate with developers on clear operational practices.\n\nQualifications\nExperience with Linux, cloud infrastructure, containers, and a programming language.\n\nCompensation\n%s USD.\n\nThis posting and all associated application details are mock data created for the public Jobtracker demo.", company, titles[i], a.PayLabel())})
		if err != nil {
			return err
		}
	}
	return nil
}
