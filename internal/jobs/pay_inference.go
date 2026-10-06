package jobs

import (
	"regexp"
	"strconv"
	"strings"
)

var descriptionPay = regexp.MustCompile(`(?i)\$\s*([0-9][0-9,]*(?:\.[0-9]{1,2})?\s*[kK]?)(?:\s*(?:-|–|—|to)\s*\$?\s*([0-9][0-9,]*(?:\.[0-9]{1,2})?\s*[kK]?))?`)
var hourlyPay = regexp.MustCompile(`(?i)(?:\b(?:hourly|per hour|an hour)\b|/\s*(?:hr|hour)\b)`)
var annualPay = regexp.MustCompile(`(?i)\b(?:salary|annual|annually|per year|a year|base pay|base salary)\b|/\s*(?:yr|year)\b`)
var otherCurrency = regexp.MustCompile(`(?i)\b(?:CAD|AUD|NZD|SGD|HKD|Canadian|Australian)\b`)

// InferPay returns one unambiguous USD rate, leaving conflicting offers blank.
// It does not treat bonuses, benefits, or arbitrary dollar amounts as base pay.
func InferPay(description string) Application {
	var result Application
	found := false
	for _, line := range strings.Split(description, "\n") {
		if otherCurrency.MatchString(line) {
			continue
		}
		kind := ""
		if hourlyPay.MatchString(line) {
			kind = "hourly"
		}
		if annualPay.MatchString(line) {
			if kind != "" {
				return Application{}
			}
			kind = "salary"
		}
		if kind == "" {
			continue
		}
		for _, match := range descriptionPay.FindAllStringSubmatch(line, -1) {
			min := normalizePay(match[1])
			if min == "" {
				continue
			}
			max := normalizePay(match[2])
			if match[2] != "" && max == "" {
				continue
			}
			if max == "" {
				max = min
			}
			candidate := Application{Company: "Preview", Title: "Preview", PayMin: min, PayMax: max, PayType: kind}
			if candidate.Validate() != nil {
				continue
			}
			if found && (result.PayMin != min || result.PayMax != max || result.PayType != kind) {
				return Application{}
			}
			result = Application{PayMin: min, PayMax: max, PayType: kind}
			found = true
		}
	}
	return result
}

func normalizePay(raw string) string {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	if raw == "" {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(raw), "k") {
		number, err := strconv.ParseFloat(strings.TrimSpace(raw[:len(raw)-1]), 64)
		if err != nil {
			return ""
		}
		raw = strconv.FormatFloat(number*1000, 'f', 2, 64)
	}
	if !payAmount.MatchString(raw) {
		return ""
	}
	if strings.Contains(raw, ".") {
		raw = strings.TrimRight(strings.TrimRight(raw, "0"), ".")
	}
	return raw
}
