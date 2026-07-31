package oracleprobe

import (
	"fmt"
)

// ComparisonStatus labels the outcome of a single case comparison.
type ComparisonStatus string

const (
	StatusPass         ComparisonStatus = "pass"
	StatusFail         ComparisonStatus = "fail"
	StatusInconclusive ComparisonStatus = "inconclusive"
)

// CaseComparison holds the result of comparing a single oracle case
// across Salesforce and Glade.
type CaseComparison struct {
	CaseID              string           `json:"caseId"`
	Status              ComparisonStatus `json:"status"`
	ExpectedObservation string           `json:"expectedObservation,omitempty"`
	SFObservation       string           `json:"sfObservation,omitempty"`
	GladeObservation    string           `json:"gladeObservation,omitempty"`
}

// Compare evaluates a Salesforce result against a Glade result for the
// given case definition.  When the case declares an unstable value the
// actual value text is replaced with a sentinel so that the comparison
// only validates execution shape (value vs exception, type consistency).
func Compare(sf, glade *Result, c Case) CaseComparison {
	cc := CaseComparison{
		CaseID:              c.ID,
		ExpectedObservation: expectedObservation(c),
	}

	if sf == nil || glade == nil {
		cc.Status = StatusInconclusive
		if sf != nil {
			cc.SFObservation = observationText(sf, c.UnstableValue != "")
		}
		if glade != nil {
			cc.GladeObservation = observationText(glade, c.UnstableValue != "")
		}
		return cc
	}

	normalize := c.UnstableValue != ""
	sfObs := observationText(sf, normalize)
	gladeObs := observationText(glade, normalize)

	cc.SFObservation = sfObs
	cc.GladeObservation = gladeObs

	if sfObs == gladeObs {
		cc.Status = StatusPass
	} else {
		cc.Status = StatusFail
	}

	return cc
}

// observationText renders a human-readable observation from a result.
// When normalize is true the concrete value is replaced with <UNSTABLE>
// so that case-declared unstable values do not cause false mismatches.
func observationText(r *Result, normalize bool) string {
	if r.ExceptionType != "" {
		return fmt.Sprintf("throws %s: %s", r.ExceptionType, r.ExceptionMessage)
	}
	if r.HasValue {
		if normalize {
			return fmt.Sprintf("value <UNSTABLE> %s", r.ValueType)
		}
		val := "null"
		if r.Value != nil {
			val = *r.Value
		}
		return fmt.Sprintf("value %s %s", val, r.ValueType)
	}
	return r.ValueType
}

// expectedObservation returns the observation the case definition
// expects, independent of any runtime result.
func expectedObservation(c Case) string {
	if c.ExpectThrow {
		return "throws"
	}
	return fmt.Sprintf("value %s", c.ValueType)
}

// RedactReport returns a copy of the report with credentials and
// identity fields cleared.  Stable fields like API version and
// results are preserved.
func RedactReport(r Report) Report {
	r.TargetOrg = ""
	r.Username = ""
	r.OrgID = ""
	return r
}

// CompareReports compares every case in the supplied list against the
// corresponding results from both runners.  Cases with no matching
// result on either side are recorded as inconclusive.
func CompareReports(sfReport, gladeReport Report, cases []Case) []CaseComparison {
	sfByID := make(map[string]*Result, len(sfReport.Results))
	for i := range sfReport.Results {
		sfByID[sfReport.Results[i].ID] = &sfReport.Results[i]
	}
	gladeByID := make(map[string]*Result, len(gladeReport.Results))
	for i := range gladeReport.Results {
		gladeByID[gladeReport.Results[i].ID] = &gladeReport.Results[i]
	}

	var comparisons []CaseComparison
	for _, c := range cases {
		comparisons = append(comparisons, Compare(sfByID[c.ID], gladeByID[c.ID], c))
	}
	return comparisons
}
