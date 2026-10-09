package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// importADP uses the public career-site context, never caller-supplied API URLs.
func (i Importer) importADP(ctx context.Context, body []byte, t target, j *Job) error {
	var site struct {
		Domain, Name, ClientName, MyJobsToken string
		Active                                bool
	}
	if err := json.Unmarshal(body, &site); err != nil {
		return err
	}
	if site.Domain != t.company || !site.Active || strings.TrimSpace(site.Name) == "" && strings.TrimSpace(site.ClientName) == "" {
		return errors.New("ADP career site is unavailable or does not match the requested company")
	}
	endpoint := "https://my.adp.com/myadp_prefix/mycareer/public/staffing/v1/job-requisitions/search-meta/" + t.id
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("rolecode", "manager")
	req.Header.Set("User-Agent", "JobTracker/0.1")
	if site.MyJobsToken != "" {
		req.Header.Set("myJobsToken", site.MyJobsToken)
	}
	res, err := i.Client.Do(req)
	if err != nil {
		return errors.New("fetch ADP posting failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ADP returned HTTP %d; posting may be unavailable", res.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 8<<20 {
		return errors.New("ADP response exceeds 8 MiB")
	}
	var data struct {
		JobRequisitions []struct {
			ItemID                                   json.Number
			RequisitionTitle, RequisitionDescription string
			ScreeningRequirements                    []struct{ RequirementDescription string }
			CustomFieldGroup                         struct {
				StringFields []struct {
					StringValue  string
					CategoryCode struct{ CodeValue string }
				}
			}
			RequisitionLocations []struct {
				Address struct {
					CityName                          string
					Country, CountrySubdivisionLevel1 struct{ LongName string }
				}
			}
		}
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return err
	}
	if len(data.JobRequisitions) != 1 || data.JobRequisitions[0].ItemID.String() != t.id {
		return errors.New("ADP returned no matching posting")
	}
	v := data.JobRequisitions[0]
	j.Company = site.Name
	if strings.TrimSpace(j.Company) == "" {
		j.Company = site.ClientName
	}
	j.Title = v.RequisitionTitle
	j.DescriptionHTML = v.RequisitionDescription
	for _, requirement := range v.ScreeningRequirements {
		j.DescriptionHTML += "\n" + requirement.RequirementDescription
	}
	for _, field := range v.CustomFieldGroup.StringFields {
		if field.CategoryCode.CodeValue == "RTiReq_internalDescription" && strings.TrimSpace(field.StringValue) != "" {
			j.DescriptionHTML += "\n" + field.StringValue
		}
	}
	var locations []string
	for _, location := range v.RequisitionLocations {
		var parts []string
		for _, part := range []string{location.Address.CityName, location.Address.CountrySubdivisionLevel1.LongName, location.Address.Country.LongName} {
			if strings.TrimSpace(part) != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			locations = append(locations, strings.Join(parts, ", "))
		}
	}
	j.Location = strings.Join(locations, "; ")
	return nil
}
