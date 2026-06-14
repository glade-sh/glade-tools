package perfscan

import (
	"github.com/glade-sh/glade/internal/profile"
)

func scanTraceBytes(report *Report, data []byte) error {
	profileReport, err := TraceProfileFromBytes(data)
	if err != nil {
		return err
	}
	AddMeasuredTraceFindings(report, profileReport)
	return nil
}

func measuredSeverity(durationMS int64) Severity {
	if durationMS >= 1000 {
		return SeverityHigh
	}
	if durationMS >= 100 {
		return SeverityMedium
	}
	return SeverityLow
}

func measuredScore(durationMS int64) int {
	score := int(durationMS / 10)
	if score < 40 {
		return 40
	}
	if score > 100 {
		return 100
	}
	return score
}

func firstFile(entry profile.Entry) string {
	return entry.File
}

func firstLine(entry profile.Entry) int {
	if len(entry.SourceRanges) > 0 {
		return entry.SourceRanges[0].Line
	}
	return 0
}

func measuredCount(entry profile.Entry) int {
	if entry.DurationCount > 0 {
		return entry.DurationCount
	}
	return entry.Count
}

func soqlRowsFromHotEvent(entry profile.Entry) int {
	return entry.Rows
}
