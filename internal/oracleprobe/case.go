package oracleprobe

import "encoding/json"

type Mode string

const (
	ModeAnonymous Mode = "anonymous"
	ModeDeploy    Mode = "deploy"
)

type Case struct {
	ID                          string   `json:"id"`
	Area                        string   `json:"area"`
	API                         string   `json:"api"`
	Mode                        Mode     `json:"mode"`
	SurfaceIDs                  []string `json:"surfaceIds,omitempty"`
	SetupClass                  string   `json:"setupClass,omitempty"`
	Statements                  []string `json:"statements,omitempty"`
	Expression                  string   `json:"expression"`
	ValueType                   string   `json:"valueType,omitempty"`
	ExpectThrow                 bool     `json:"expectThrow,omitempty"`
	UnstableValue               string   `json:"unstableValue,omitempty"`
	ExceptionMessageContractual bool     `json:"exceptionMessageContractual,omitempty"`
}

type Result struct {
	ID               string  `json:"id"`
	Area             string  `json:"area"`
	API              string  `json:"api"`
	Mode             Mode    `json:"mode"`
	Value            *string `json:"value"`
	HasValue         bool    `json:"-"`
	ValueType        string  `json:"valueType,omitempty"`
	ExceptionType    string  `json:"exceptionType,omitempty"`
	ExceptionMessage string  `json:"exceptionMessage,omitempty"`
	RawLogLine       string  `json:"rawLogLine,omitempty"`
}

func (r *Result) UnmarshalJSON(data []byte) error {
	type resultAlias Result
	var raw struct {
		*resultAlias
		Value json.RawMessage `json:"value"`
	}
	raw.resultAlias = (*resultAlias)(r)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Value != nil {
		r.HasValue = true
		if string(raw.Value) != "null" {
			var value string
			if err := json.Unmarshal(raw.Value, &value); err != nil {
				return err
			}
			r.Value = &value
		}
	}
	return nil
}

type Report struct {
	TargetOrg  string   `json:"targetOrg"`
	Username   string   `json:"username,omitempty"`
	OrgID      string   `json:"orgId,omitempty"`
	APIVersion string   `json:"apiVersion,omitempty"`
	Results    []Result `json:"results"`
}
