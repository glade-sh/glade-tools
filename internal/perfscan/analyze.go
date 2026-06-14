package perfscan

import (
	"os"
	"path/filepath"

	"github.com/glade-sh/glade/internal/apexast"
	"github.com/glade-sh/glade/internal/project"
	gladeschema "github.com/glade-sh/glade/internal/schema"
	"github.com/glade-sh/glade/internal/typesys"
)

type Options struct {
	ProjectRoot  string
	TracePath    string
	OrgFactsPath string
	TopN         int
}

func AnalyzeProject(options Options) (Report, error) {
	root := options.ProjectRoot
	if root == "" {
		root = "."
	}
	absRoot, _ := filepath.Abs(root)
	report := Report{SchemaVersion: SchemaVersion, Project: absRoot}

	p, err := project.Load(absRoot)
	if err != nil {
		return report, err
	}

	schema, err := gladeschema.LoadProject(p)
	if err != nil {
		return report, err
	}

	parser := apexast.NewParser()
	parsed := apexast.Result{Files: make([]apexast.File, 0, len(p.ApexFiles))}
	for _, path := range p.ApexFiles {
		file, err := parser.ParseFile(path)
		if err != nil {
			continue
		}
		parsed.Files = append(parsed.Files, file)
	}

	index := typesys.Build(p, schema)
	scanApex(&report, p, parsed, index)
	sourceGraph := BuildSourceGraph(parsed, index)
	metadataFacts := BuildMetadataFacts(p, parsed)
	ApplyMetadataFacts(sourceGraph, metadataFacts, parsed)
	var orgFacts OrgFacts
	hasOrgFacts := false
	if options.OrgFactsPath != "" {
		loadedOrgFacts, err := LoadOrgFacts(options.OrgFactsPath)
		if err != nil {
			return report, err
		}
		orgFacts = loadedOrgFacts
		hasOrgFacts = true
		ApplyOrgFacts(sourceGraph, orgFacts)
	}
	traceLoaded := false
	if options.TracePath != "" {
		data, err := os.ReadFile(options.TracePath)
		if err != nil {
			return report, err
		}
		profileReport, err := TraceProfileFromBytes(data)
		if err != nil {
			return report, err
		}
		correlation := CorrelateTrace(sourceGraph, profileReport)
		AddTraceMeasurements(&report, profileReport.Spans)
		AddMeasuredTraceFindingsForEntries(&report, correlation.Unmatched)
		traceLoaded = true
	}
	emitSourceGraphFindings(&report, sourceGraph)
	emitMetadataGraphFindings(&report, sourceGraph)
	if hasOrgFacts {
		emitOrgFactFindings(&report, sourceGraph, orgFacts)
	}
	scanMetadata(&report, p, index)
	if traceLoaded {
		promoteReportFindingsFromTrace(&report, sourceGraph)
	}
	report.Finalize()
	if options.TopN > 0 && len(report.Findings) > options.TopN {
		report.Findings = report.Findings[:options.TopN]
		report.Finalize()
	}
	return report, nil
}
