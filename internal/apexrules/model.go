package apexrules

type Outcome string

const (
	OutcomeAccept Outcome = "accept"
	OutcomeReject Outcome = "reject"
)

const (
	StatusSupported             = "supported"
	StatusConfirmedGap          = "confirmed-gap"
	StatusRuntimeOnly           = "runtime-only"
	StatusPackageHistoryPending = "package-history-pending"
	StatusPreviewDisabled       = "preview-disabled"
	StatusOraclePending         = "oracle-pending"
)

type SourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Rule struct {
	ID                 string       `json:"id"`
	Area               string       `json:"area"`
	DocsPath           string       `json:"docsPath"`
	DocsLines          string       `json:"docsLines"`
	APIVersion         float64      `json:"apiVersion"`
	SourceKind         string       `json:"sourceKind"`
	Source             string       `json:"source"`
	Dependencies       []SourceFile `json:"dependencies,omitempty"`
	ProjectFiles       []SourceFile `json:"projectFiles,omitempty"`
	Oracle             Outcome      `json:"oracle"`
	Owner              string       `json:"owner"`
	Status             string       `json:"status"`
	ProductTest        string       `json:"productTest,omitempty"`
	ProductTestAliasOf string       `json:"productTestAliasOf,omitempty"`
}

type Catalog struct {
	GladeCommit string `json:"gladeCommit"`
	Rules       []Rule `json:"rules"`
}

type Result struct {
	ID            string   `json:"id"`
	Oracle        Outcome  `json:"oracle"`
	CatalogOracle Outcome  `json:"catalogOracle,omitempty"`
	Salesforce    Outcome  `json:"salesforce,omitempty"`
	OracleMatched bool     `json:"oracleMatched"`
	Glade         Outcome  `json:"glade"`
	Matched       bool     `json:"matched"`
	Status        string   `json:"status"`
	Problems      []string `json:"salesforceProblems,omitempty"`
	ExecStatus    string   `json:"execStatus,omitempty"`
}
