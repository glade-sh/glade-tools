package perfscan

import "sort"

const SchemaVersion = 2

type Category string

const (
	CategoryApex       Category = "apex"
	CategorySOQL       Category = "soql"
	CategoryDML        Category = "dml"
	CategoryDescribe   Category = "describe"
	CategoryAutomation Category = "automation"
	CategoryUI         Category = "ui"
	CategoryAsync      Category = "async"
	CategoryMeasured   Category = "measured"
)

type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

type Confidence string

const (
	ConfidenceStatic   Confidence = "static"
	ConfidenceMeasured Confidence = "measured"
	ConfidenceCombined Confidence = "combined"
)

type EntryKind string

const (
	EntryTrigger     EntryKind = "trigger"
	EntryBatch       EntryKind = "batch"
	EntryQueueable   EntryKind = "queueable"
	EntrySchedulable EntryKind = "schedulable"
	EntryFuture      EntryKind = "future"
	EntryInvocable   EntryKind = "invocable"
	EntryVisualforce EntryKind = "visualforce"
	EntryAura        EntryKind = "aura"
	EntryLWC         EntryKind = "lwc"
	EntryFlow        EntryKind = "flow"
	EntryWorkflow    EntryKind = "workflow"
	EntryUnknown     EntryKind = "unknown"
)

type Report struct {
	SchemaVersion int           `json:"schemaVersion"`
	Project       string        `json:"project"`
	Summary       Summary       `json:"summary"`
	Findings      []Finding     `json:"findings,omitempty"`
	EntryPoints   []EntryPoint  `json:"entryPoints,omitempty"`
	Measurements  []Measurement `json:"measurements,omitempty"`
}

type Summary struct {
	Findings   int            `json:"findings"`
	High       int            `json:"high"`
	Medium     int            `json:"medium"`
	Low        int            `json:"low"`
	Categories map[string]int `json:"categories,omitempty"`
}

type Finding struct {
	ID            string       `json:"id"`
	Category      Category     `json:"category"`
	Severity      Severity     `json:"severity"`
	Confidence    Confidence   `json:"confidence"`
	Score         int          `json:"score"`
	EntryPoint    EntryPoint   `json:"entryPoint,omitempty"`
	Message       string       `json:"message"`
	Location      Location     `json:"location,omitempty"`
	Path          []PathStep   `json:"path,omitempty"`
	NamespacePath []string     `json:"namespacePath,omitempty"`
	Multiplicity  string       `json:"multiplicity,omitempty"`
	Evidence      []Evidence   `json:"evidence,omitempty"`
	ResourceRisk  ResourceRisk `json:"resourceRisk,omitempty,omitzero"`
	Fix           string       `json:"fix,omitempty"`
	Acceptance    string       `json:"acceptance,omitempty"`
}

type EntryPoint struct {
	Kind   EntryKind `json:"kind,omitempty"`
	Name   string    `json:"name,omitempty"`
	File   string    `json:"file,omitempty"`
	Line   int       `json:"line,omitempty"`
	Method string    `json:"method,omitempty"`
}

type Location struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type PathStep struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

type Evidence struct {
	Kind         string       `json:"kind"`
	Message      string       `json:"message"`
	Value        string       `json:"value,omitempty"`
	NodeID       string       `json:"nodeId,omitempty"`
	Operation    string       `json:"operation,omitempty"`
	Path         []PathStep   `json:"path,omitempty"`
	ResourceRisk ResourceRisk `json:"resourceRisk,omitempty,omitzero"`
}

type Measurement struct {
	Name         string       `json:"name"`
	Category     string       `json:"category,omitempty"`
	DurationMS   int64        `json:"durationMs,omitempty"`
	Count        int          `json:"count,omitempty"`
	File         string       `json:"file,omitempty"`
	Line         int          `json:"line,omitempty"`
	EntryPoint   EntryPoint   `json:"entryPoint,omitempty,omitzero"`
	Path         []PathStep   `json:"path,omitempty"`
	Namespace    string       `json:"namespace,omitempty"`
	Operation    string       `json:"operation,omitempty"`
	OperationID  string       `json:"operationId,omitempty"`
	ResourceRisk ResourceRisk `json:"resourceRisk,omitempty,omitzero"`
	Evidence     []Evidence   `json:"evidence,omitempty"`
}

type ResourceRisk struct {
	CPU         bool `json:"cpu,omitempty"`
	Heap        bool `json:"heap,omitempty"`
	DBTime      bool `json:"dbTime,omitempty"`
	DBRows      bool `json:"dbRows,omitempty"`
	Locks       bool `json:"locks,omitempty"`
	SharedLimit bool `json:"sharedLimit,omitempty"`
}

func mergeResourceRisk(left, right ResourceRisk) ResourceRisk {
	return ResourceRisk{
		CPU:         left.CPU || right.CPU,
		Heap:        left.Heap || right.Heap,
		DBTime:      left.DBTime || right.DBTime,
		DBRows:      left.DBRows || right.DBRows,
		Locks:       left.Locks || right.Locks,
		SharedLimit: left.SharedLimit || right.SharedLimit,
	}
}

func (r *Report) AddFinding(f Finding) {
	r.Findings = append(r.Findings, f)
}

func (r *Report) AddEntryPoint(e EntryPoint) {
	if e.Kind == "" {
		e.Kind = EntryUnknown
	}
	r.EntryPoints = append(r.EntryPoints, e)
}

func (r *Report) AddMeasurement(m Measurement) {
	r.Measurements = append(r.Measurements, m)
}

func (r *Report) Finalize() {
	if r.SchemaVersion == 0 {
		r.SchemaVersion = SchemaVersion
	}
	r.Findings = RankFindings(r.Findings, RankOptions{})
	sort.Slice(r.EntryPoints, func(i, j int) bool {
		if r.EntryPoints[i].Kind != r.EntryPoints[j].Kind {
			return r.EntryPoints[i].Kind < r.EntryPoints[j].Kind
		}
		if r.EntryPoints[i].Name != r.EntryPoints[j].Name {
			return r.EntryPoints[i].Name < r.EntryPoints[j].Name
		}
		return r.EntryPoints[i].Line < r.EntryPoints[j].Line
	})
	sort.Slice(r.Measurements, func(i, j int) bool {
		if r.Measurements[i].DurationMS != r.Measurements[j].DurationMS {
			return r.Measurements[i].DurationMS > r.Measurements[j].DurationMS
		}
		return r.Measurements[i].Count > r.Measurements[j].Count
	})
	r.Summary = Summary{Findings: len(r.Findings), Categories: map[string]int{}}
	for _, finding := range r.Findings {
		switch finding.Severity {
		case SeverityHigh:
			r.Summary.High++
		case SeverityMedium:
			r.Summary.Medium++
		default:
			r.Summary.Low++
		}
		r.Summary.Categories[string(finding.Category)]++
	}
	if len(r.Summary.Categories) == 0 {
		r.Summary.Categories = nil
	}
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	default:
		return 1
	}
}
