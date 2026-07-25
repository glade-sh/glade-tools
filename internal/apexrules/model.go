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
	ID           string       `json:"id"`
	Area         string       `json:"area"`
	DocsPath     string       `json:"docsPath"`
	DocsLines    string       `json:"docsLines"`
	APIVersion   float64      `json:"apiVersion"`
	SourceKind   string       `json:"sourceKind"`
	Source       string       `json:"source"`
	Dependencies []SourceFile `json:"dependencies,omitempty"`
	Oracle       Outcome      `json:"oracle"`
	Owner        string       `json:"owner"`
	Status       string       `json:"status"`
	ProductTest  string       `json:"productTest,omitempty"`
}

type Catalog struct {
	Rules []Rule `json:"rules"`
}

type Result struct {
	ID      string  `json:"id"`
	Oracle  Outcome `json:"oracle"`
	Glade   Outcome `json:"glade"`
	Matched bool    `json:"matched"`
	Status  string  `json:"status"`
}
