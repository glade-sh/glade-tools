package perfscan

import (
	"sort"
	"strconv"
	"strings"
)

type RankOptions struct {
	TopN          int
	MinConfidence Confidence
}

func RankFindings(findings []Finding, options RankOptions) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if confidenceValue(finding.Confidence) < confidenceValue(options.MinConfidence) {
			continue
		}
		finding.Score = scoreFinding(finding)
		out = append(out, finding)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Severity != out[j].Severity {
			return severityRank(out[i].Severity) > severityRank(out[j].Severity)
		}
		return findingSortKey(out[i]) < findingSortKey(out[j])
	})
	if options.TopN > 0 && len(out) > options.TopN {
		out = out[:options.TopN]
	}
	return out
}

func findingSortKey(finding Finding) string {
	return strings.Join([]string{
		finding.ID,
		finding.Location.File,
		strconv.Itoa(finding.Location.Line),
		strconv.Itoa(finding.Location.Column),
		finding.Message,
		evidenceSortKey(finding.Evidence),
	}, "|")
}

func evidenceSortKey(evidence []Evidence) string {
	parts := make([]string, 0, len(evidence))
	for _, item := range evidence {
		parts = append(parts, evidenceKey(item))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x00")
}

func scoreFinding(finding Finding) int {
	score := severityScore(finding.Severity)
	switch finding.Confidence {
	case ConfidenceCombined:
		score += 30
	case ConfidenceMeasured:
		score += 20
	}
	if finding.ResourceRisk.CPU || finding.ResourceRisk.Heap || finding.ResourceRisk.DBTime || finding.ResourceRisk.SharedLimit {
		score += 15
	}
	if finding.ResourceRisk.DBRows {
		score += 8
	}
	if finding.ResourceRisk.Locks {
		score += 12
	}
	if finding.Category == CategoryAutomation {
		score += 8
	}
	switch strings.ToLower(strings.TrimSpace(finding.Multiplicity)) {
	case "per-record":
		score += 15
	case "per-child":
		score += 12
	case "per-field":
		score += 10
	case "once-per-transaction":
		score += 4
	}
	score += traceEvidenceScore(finding.Evidence)
	if finding.Confidence == ConfidenceStatic && finding.Severity == SeverityLow && score > 55 {
		score = 55
	}
	if score > 100 {
		return 100
	}
	if score < 0 {
		return 0
	}
	return score
}

func traceEvidenceScore(evidence []Evidence) int {
	score := 0
	for _, item := range evidence {
		if item.Kind != "trace" {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(item.Value))
		if err != nil || value <= 0 {
			continue
		}
		message := strings.ToLower(item.Message)
		switch {
		case strings.Contains(message, "duration"):
			score += minInt(value/50, 25)
		case strings.Contains(message, "row"):
			score += minInt(value/100, 15)
		}
	}
	return score
}

func severityScore(severity Severity) int {
	switch severity {
	case SeverityHigh:
		return 60
	case SeverityMedium:
		return 40
	default:
		return 20
	}
}

func confidenceValue(confidence Confidence) int {
	switch confidence {
	case ConfidenceCombined:
		return 3
	case ConfidenceMeasured:
		return 2
	default:
		return 1
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
