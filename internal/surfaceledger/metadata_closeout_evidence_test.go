package surfaceledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

func metadataCloseoutOwners() map[string]string {
	owners := make(map[string]string, 40)
	for _, typeName := range []string{"CustomMetadata", "CustomMetadataValue", "DeployCallbackContext", "DeployContainer", "DeployDetails", "DeployMessage", "DeployResult"} {
		owners["apex:Metadata."+typeName+".clone()"] = "current-base-deterministic-mock-required-Metadata-001"
	}
	for _, typeName := range []string{"DeployProblemType", "DeployStatus", "FeedItemTypeEnum"} {
		for _, method := range []string{"equals(Object)", "hashCode()", "ordinal()", "valueOf(String)", "values()"} {
			owners["apex:Metadata."+typeName+"."+method] = "current-base-deterministic-mock-required-Metadata-002"
		}
	}
	for _, typeName := range []string{"LayoutColumn", "MiniLayout"} {
		owners["apex:Metadata."+typeName+".clone()"] = "current-base-deterministic-mock-required-Metadata-002"
	}
	for _, typeName := range []string{"OmniInteractionAccessConfig", "Operations", "PlatformActionList", "PlatformActionListItem", "PrimaryTabComponents", "QuickActionList"} {
		owners["apex:Metadata."+typeName+".clone()"] = "current-base-deterministic-mock-required-Metadata-003"
	}
	for _, typeName := range []string{"QuickActionListItem", "RelatedContent", "RelatedContentItem", "RelatedList", "RelatedListItem", "ReportChartComponentLayoutItem", "SidebarComponent"} {
		owners["apex:Metadata."+typeName+".clone()"] = "current-base-metadata-dto-deterministic-004-api67"
	}
	for _, typeName := range []string{"SubtabComponents", "SummaryLayout", "SummaryLayoutItem"} {
		owners["apex:Metadata."+typeName+".clone()"] = "current-base-metadata-dto-deterministic-005-api67"
	}
	return owners
}

func TestMetadataCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	want := metadataCloseoutOwners()
	if len(want) != 40 {
		t.Fatalf("Metadata closeout IDs = %d, want 40", len(want))
	}
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := make([]string, 0, len(want))
	var selected []SurfaceLedgerRow
	for id := range want {
		wantIDs = append(wantIDs, id)
	}
	for _, row := range evidence {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	for _, row := range selected {
		owner := "fixture:" + want[row.SurfaceID]
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixtureNames := []string{
		"current-base-deterministic-mock-required-Metadata-001",
		"current-base-deterministic-mock-required-Metadata-002",
		"current-base-deterministic-mock-required-Metadata-003",
		"current-base-metadata-dto-deterministic-004-api67",
		"current-base-metadata-dto-deterministic-005-api67",
	}
	sources := make(map[string]string, len(fixtureNames))
	for _, name := range fixtureNames {
		fixturePath := filepath.Join(root, "docs", "fixtures", name+".json")
		fixture, err := compat.LoadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "test" || len(fixture.Source) == 0 {
			t.Fatalf("fixture %s envelope = %#v", name, fixture)
		}
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		var policy struct {
			SalesforceEligible        *bool  `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			t.Fatal(err)
		}
		if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || !strings.Contains(policy.SalesforceExclusionReason, "without claiming Salesforce") && !strings.Contains(policy.SalesforceExclusionReason, "makes no Salesforce") {
			t.Fatalf("fixture %s policy = %#v", name, policy)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s execution = %#v, error = %v", name, result, err)
		}
		var source strings.Builder
		for _, file := range fixture.Source {
			source.WriteString(file.Content)
		}
		sources[name] = source.String()
	}

	cloneWitnesses := map[string][]string{
		fixtureNames[0]: {"Metadata.CustomMetadata customClone = (Metadata.CustomMetadata)custom.clone();", "Metadata.CustomMetadataValue valueClone = (Metadata.CustomMetadataValue)value.clone();", "Metadata.DeployCallbackContext contextClone = (Metadata.DeployCallbackContext)context.clone();", "Metadata.DeployContainer containerClone = (Metadata.DeployContainer)container.clone();", "Metadata.DeployDetails detailsClone = (Metadata.DeployDetails)details.clone();", "Metadata.DeployMessage messageClone = (Metadata.DeployMessage)message.clone();", "Metadata.DeployResult resultClone = (Metadata.DeployResult)result.clone();"},
		fixtureNames[1]: {"Metadata.LayoutColumn columnClone = (Metadata.LayoutColumn)column.clone();", "Metadata.MiniLayout miniLayoutClone = (Metadata.MiniLayout)miniLayout.clone();"},
		fixtureNames[2]: {"Metadata.OmniInteractionAccessConfig configClone = (Metadata.OmniInteractionAccessConfig)config.clone();", "Metadata.Operations operationsClone = (Metadata.Operations)operations.clone();", "Metadata.PlatformActionList actionListClone = (Metadata.PlatformActionList)actionList.clone();", "Metadata.PlatformActionListItem actionClone = (Metadata.PlatformActionListItem)action.clone();", "Metadata.PrimaryTabComponents primaryClone = (Metadata.PrimaryTabComponents)primary.clone();", "Metadata.QuickActionList quickActionsClone = (Metadata.QuickActionList)quickActions.clone();"},
		fixtureNames[3]: {"Metadata.QuickActionListItem quickActionListItemClone = (Metadata.QuickActionListItem)quickActionListItem.clone();", "Metadata.RelatedContent relatedContentClone = (Metadata.RelatedContent)relatedContent.clone();", "Metadata.RelatedContentItem relatedContentItemClone = (Metadata.RelatedContentItem)relatedContentItem.clone();", "Metadata.RelatedList relatedListClone = (Metadata.RelatedList)relatedList.clone();", "Metadata.RelatedListItem relatedListItemClone = (Metadata.RelatedListItem)relatedListItem.clone();", "Metadata.ReportChartComponentLayoutItem chartClone = (Metadata.ReportChartComponentLayoutItem)chart.clone();", "Metadata.SidebarComponent sidebarClone = (Metadata.SidebarComponent)sidebar.clone();"},
		fixtureNames[4]: {"Metadata.SubtabComponents subtabClone = (Metadata.SubtabComponents)subtab.clone();", "Metadata.SummaryLayout summaryClone = (Metadata.SummaryLayout)summary.clone();", "Metadata.SummaryLayoutItem itemClone = (Metadata.SummaryLayoutItem)item.clone();"},
	}
	for name, witnesses := range cloneWitnesses {
		for _, witness := range witnesses {
			if !strings.Contains(sources[name], witness) {
				t.Fatalf("fixture %s missing %q", name, witness)
			}
		}
	}
	for _, witness := range []string{"Metadata.DeployProblemType.values()", "Metadata.DeployStatus.values()", "Metadata.FeedItemTypeEnum.values()"} {
		if !strings.Contains(sources[fixtureNames[1]], witness) {
			t.Fatalf("enum source missing %q", witness)
		}
	}
}
