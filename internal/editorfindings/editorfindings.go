package editorfindings

import (
	"encoding/json"
	"fmt"
	"io"
)

const Kind = "glade.findings.v1"

type Payload struct {
	Kind      string     `json:"kind"`
	Summary   string     `json:"summary"`
	Findings  []Finding  `json:"findings"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

type Finding struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	RuleID   string `json:"ruleId"`
	Source   string `json:"source"`
}

type Artifact struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

func New(findings []Finding, artifacts []Artifact) Payload {
	if findings == nil {
		findings = []Finding{}
	}
	return Payload{
		Kind:      Kind,
		Summary:   Summary(len(findings)),
		Findings:  findings,
		Artifacts: artifacts,
	}
}

func Summary(count int) string {
	if count == 1 {
		return "1 finding"
	}
	return fmt.Sprintf("%d findings", count)
}

func Write(w io.Writer, payload Payload) error {
	if payload.Kind == "" {
		payload.Kind = Kind
	}
	if payload.Summary == "" {
		payload.Summary = Summary(len(payload.Findings))
	}
	if payload.Findings == nil {
		payload.Findings = []Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
