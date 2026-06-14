package perfscan

import "testing"

func TestRankerPromotesMeasuredSharedLimitFindings(t *testing.T) {
	findings := []Finding{
		{ID: "perf.soql.loop", Severity: SeverityHigh, Confidence: ConfidenceStatic, Score: 80, ResourceRisk: ResourceRisk{DBRows: true}},
		{ID: "perf.static.first-touch", Severity: SeverityMedium, Confidence: ConfidenceCombined, Score: 70, ResourceRisk: ResourceRisk{CPU: true, Heap: true, SharedLimit: true}, Evidence: []Evidence{{Kind: "trace", Message: "duration ms", Value: "900"}}},
	}
	ranked := RankFindings(findings, RankOptions{TopN: 0})
	if ranked[0].ID != "perf.static.first-touch" {
		t.Fatalf("ranked = %#v", ranked)
	}
	if ranked[0].Score != 100 {
		t.Fatalf("score = %d", ranked[0].Score)
	}
}

func TestRankerFiltersByConfidenceAndTopN(t *testing.T) {
	findings := []Finding{
		{ID: "static", Severity: SeverityHigh, Confidence: ConfidenceStatic},
		{ID: "measured", Severity: SeverityLow, Confidence: ConfidenceMeasured},
		{ID: "combined", Severity: SeverityMedium, Confidence: ConfidenceCombined},
	}
	ranked := RankFindings(findings, RankOptions{TopN: 1, MinConfidence: ConfidenceMeasured})
	if len(ranked) != 1 || ranked[0].ID != "combined" {
		t.Fatalf("ranked = %#v", ranked)
	}
}

func TestRankerCapsLowStaticNoise(t *testing.T) {
	ranked := RankFindings([]Finding{{
		ID:         "noise",
		Severity:   SeverityLow,
		Confidence: ConfidenceStatic,
		Score:      99,
	}}, RankOptions{})
	if ranked[0].Score > 55 {
		t.Fatalf("score = %d, want capped static low confidence", ranked[0].Score)
	}
}
