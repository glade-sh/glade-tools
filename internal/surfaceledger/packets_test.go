package surfaceledger

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAreaRegistryNamesInitialParallelAreas(t *testing.T) {
	want := []string{
		"Ledger.Identity",
		"Core.Runtime.System.FeatureManagement",
		"Core.Runtime.Database.Batchable",
		"Apex.Language",
		"Core.Runtime.SystemAndStdlib",
		"Query.Runtime.SOQLSOSL",
		"Data.Reference.ObjectsFields",
		"Data.Runtime.SchemaDescribe",
		"Data.Runtime.SOQL",
		"Data.Runtime.DML",
		"Tests.AsyncAndIsolation",
		"UI.ApexPagesControllers",
		"UI.VisualforceComponents",
		"UI.LWCModules",
		"UI.AuraComponents",
		"UI.UIAPI",
		"Server.RESTResources",
		"Server.ToolingObjects",
		"Integration.GraphQL",
		"Integration.PubSub",
		"Integration.BulkAPI",
		"Integration.MetadataAPI",
		"Integration.SOAPAPI",
		"Integration.StreamingAPI",
		"Integration.SalesforceConnect.AmazonRDS",
		"Platform.Events",
		"AI.Agentforce",
		"External.MarketingCloud.AMPscript",
		"External.MarketingCloud.Handlebars",
		"ConnectApi.PassiveDTOs",
	}
	got := map[string]bool{}
	for _, area := range AreaRegistry() {
		got[area.Name] = true
		if area.Owner == "" || area.RowFilter == "" || area.AreaRatchetCommand == "" {
			t.Fatalf("area %q is missing owner, row filter, or ratchet command: %#v", area.Name, area)
		}
		if len(area.DependsOn) == 0 || len(area.MayRunInParallelWith) == 0 || len(area.SharedFiles) == 0 || len(area.ExclusiveFiles) == 0 {
			t.Fatalf("area %q is missing parallel-work boundaries: %#v", area.Name, area)
		}
	}
	for _, name := range want {
		if !got[name] {
			t.Fatalf("area registry missing %q", name)
		}
	}
}

func TestApexLanguagePacketOwnsLanguageRows(t *testing.T) {
	packet, ok := AreaPacketByName("Apex.Language")
	if !ok {
		t.Fatal("missing Apex.Language packet")
	}
	rows := PacketRows(SurfaceLedger{Rows: []SurfaceLedgerRow{{SurfaceID: "apex-language:NamespaceClassVariablePrecedence", Bucket: BucketGap, GapClass: GapMissingShape}}}, packet)
	if len(rows) != 1 || rows[0].SurfaceID != "apex-language:NamespaceClassVariablePrecedence" {
		t.Fatalf("language rows = %#v", rows)
	}
}

func TestPacketManifestRetainsSelectableClosedRows(t *testing.T) {
	ledger := SurfaceLedger{Rows: []SurfaceLedgerRow{{
		SurfaceID: "apex-language:NamespaceClassVariablePrecedence",
		Product:   "apex-language",
		Bucket:    BucketImplemented,
	}}}
	manifest := BuildPacketManifest(ledger)
	for _, packet := range manifest.Packets {
		if packet.ID == "Apex.Language" {
			if len(packet.RowIDs) != 1 || packet.RowIDs[0] != "apex-language:NamespaceClassVariablePrecedence" {
				t.Fatalf("Apex.Language row IDs = %#v", packet.RowIDs)
			}
			return
		}
	}
	t.Fatal("Apex.Language packet missing from manifest")
}

func TestPacketMarkdownIncludesAgentCloseoutRules(t *testing.T) {
	packet, ok := AreaPacketByName("Core.Runtime.System.FeatureManagement")
	if !ok {
		t.Fatal("missing FeatureManagement packet")
	}
	markdown := PacketMarkdown(SurfaceLedger{}, packet)
	for _, want := range []string{
		"## Standard Validation Block",
		"focused tests run",
		"fixture command run",
		"surface refresh run",
		"area ratchet command run",
		"before counts",
		"after counts",
		"next top row",
		"## Docs Defect Path",
		"re-scrape docs",
		"patch the docs parser to read existing docs correctly",
		"## Reviewer Checklist",
		"no corpus-specific runtime hacks",
		"packet area did not expand during work",
		"## Breadth Work Order",
		"Schema.Describe",
		"glade-tools surface refresh",
		"glade-tools surface check",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("packet markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "glade surface") {
		t.Fatalf("packet markdown still points at old surface commands:\n%s", markdown)
	}
}

func TestOpenRowsReturnsSortedGapAndFailureRows(t *testing.T) {
	ledger := SurfaceLedger{Rows: []SurfaceLedgerRow{
		{SurfaceID: "apex:System.Done", Bucket: BucketImplemented, Priority: 1},
		{SurfaceID: "apex:System.LaterGap", Bucket: BucketGap, GapClass: GapMissingBehavior, Priority: 50},
		{SurfaceID: "apex:System.FirstFailure", Bucket: BucketFailure, GapClass: GapReturnTypeMismatch, Priority: 5},
		{SurfaceID: "apex:System.FirstGap", Bucket: BucketGap, GapClass: GapMissingShape, Priority: 5},
	}}

	rows := OpenRows(ledger)
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, row.SurfaceID)
	}
	want := []string{"apex:System.FirstFailure", "apex:System.FirstGap", "apex:System.LaterGap"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("open rows = %#v, want %#v", got, want)
	}
}

func TestPacketManifestAssignsEveryOpenRowOnce(t *testing.T) {
	ledger := SurfaceLedger{Rows: []SurfaceLedgerRow{
		{
			SurfaceID: "apex:System.FeatureManagement.checkPermission(String)",
			Product:   ProductApex,
			Namespace: "System",
			TypeName:  "FeatureManagement",
			Kind:      KindMethod,
			Bucket:    BucketGap,
			GapClass:  GapMissingShape,
			Priority:  10,
		},
		{
			SurfaceID:               "rest:/services/data/vXX.X/sobjects",
			Product:                 ProductREST,
			SalesforceSurfaceFamily: "rest-api",
			Kind:                    KindResource,
			Bucket:                  BucketFailure,
			GapClass:                GapDocsOrgMismatch,
			Priority:                20,
		},
		{
			SurfaceID:               "unknown:commerce-marketplaces",
			Product:                 ProductUnknown,
			SalesforceSurfaceFamily: "commerce-api",
			Kind:                    KindGuide,
			Bucket:                  BucketGap,
			GapClass:                GapMissingShape,
			Priority:                30,
		},
		{
			SurfaceID: "apex:System.Done",
			Product:   ProductApex,
			Bucket:    BucketImplemented,
		},
		{
			SurfaceID:               "unknown:connect-address-input",
			Product:                 ProductUnknown,
			SalesforceSurfaceFamily: ProductUnknown,
			DocsSource:              "connect-rest-api/connect_requests_address_input.md",
			Kind:                    KindGuide,
			Bucket:                  BucketGap,
			GapClass:                GapMissingShape,
			Priority:                40,
		},
	}}

	manifest := BuildPacketManifest(ledger)
	if manifest.TotalOpenRows != 4 {
		t.Fatalf("total open rows = %d, want 4", manifest.TotalOpenRows)
	}
	if len(manifest.UnassignedRows) != 0 {
		t.Fatalf("unassigned rows = %#v", manifest.UnassignedRows)
	}

	seen := map[string]string{}
	for _, packet := range manifest.Packets {
		if packet.ID == "" || packet.Owner == "" {
			t.Fatalf("packet missing id or owner: %#v", packet)
		}
		if packet.SourceFamily == "" && packet.SourceDir == "" {
			t.Fatalf("packet %s is missing source family or source dir", packet.ID)
		}
		for _, rowID := range packet.RowIDs {
			if previous := seen[rowID]; previous != "" {
				t.Fatalf("row %s assigned to both %s and %s", rowID, previous, packet.ID)
			}
			seen[rowID] = packet.ID
		}
	}
	for _, want := range []string{
		"apex:System.FeatureManagement.checkPermission(String)",
		"rest:/services/data/vXX.X/sobjects",
		"unknown:commerce-marketplaces",
		"unknown:connect-address-input",
	} {
		if seen[want] == "" {
			t.Fatalf("open row %s was not assigned; manifest=%#v", want, manifest)
		}
	}
	if packetID := seen["unknown:connect-address-input"]; !strings.Contains(packetID, "connect-rest-api") {
		t.Fatalf("connect docs row assigned to %q, want connect-rest-api packet", packetID)
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"totalOpenRows":4`, `"packets"`, `"rowIds"`, `"unassignedRows"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("manifest JSON missing %s: %s", want, data)
		}
	}
	if !strings.Contains(string(data), `"unassignedRows":[]`) {
		t.Fatalf("empty unassignedRows must encode as []: %s", data)
	}
}

func TestPacketManifestLeavesTrulyUnknownRowsUnassigned(t *testing.T) {
	ledger := SurfaceLedger{Rows: []SurfaceLedgerRow{
		{
			SurfaceID:               "unknown:connect-address-input",
			Product:                 ProductUnknown,
			SalesforceSurfaceFamily: ProductUnknown,
			DocsSource:              "connect-rest-api/connect_requests_address_input.md",
			Kind:                    KindGuide,
			Bucket:                  BucketGap,
			GapClass:                GapMissingShape,
			Priority:                10,
		},
		{
			SurfaceID: "unknown:no-source",
			Kind:      KindGuide,
			Bucket:    BucketGap,
			GapClass:  GapMissingShape,
			Priority:  20,
		},
	}}

	manifest := BuildPacketManifest(ledger)
	if manifest.TotalOpenRows != 2 {
		t.Fatalf("total open rows = %d, want 2", manifest.TotalOpenRows)
	}
	if strings.Join(manifest.UnassignedRows, ",") != "unknown:no-source" {
		t.Fatalf("unassigned rows = %#v, want unknown:no-source", manifest.UnassignedRows)
	}

	seen := map[string]string{}
	for _, packet := range manifest.Packets {
		if strings.Contains(packet.ID, "unknown") {
			t.Fatalf("truly unknown rows should not create unknown packet: %#v", packet)
		}
		for _, rowID := range packet.RowIDs {
			seen[rowID] = packet.ID
		}
	}
	if packetID := seen["unknown:connect-address-input"]; !strings.Contains(packetID, "connect-rest-api") {
		t.Fatalf("connect docs row assigned to %q, want connect-rest-api packet", packetID)
	}
	if seen["unknown:no-source"] != "" {
		t.Fatalf("truly unknown row was assigned to %q", seen["unknown:no-source"])
	}
}

func TestSchemaDescribePacketOwnsOnlySchemaNamespaceDescribeRows(t *testing.T) {
	packet, ok := AreaPacketByName("Data.Runtime.SchemaDescribe")
	if !ok {
		t.Fatal("missing SchemaDescribe packet")
	}
	schemaRow := SurfaceLedgerRow{
		SurfaceID: ApexMemberID("Schema", "DescribeFieldResult", "getName", []string{}),
		Product:   ProductApex,
		Namespace: "Schema",
		TypeName:  "DescribeFieldResult",
		Kind:      KindMethod,
	}
	if !packetOwnsRow(packet, schemaRow) {
		t.Fatalf("SchemaDescribe packet should own %s", schemaRow.SurfaceID)
	}
	foreignDescribe := SurfaceLedgerRow{
		SurfaceID: ApexTypeID("Invocable.Action", "DescribeResult"),
		Product:   ProductApex,
		Namespace: "Invocable.Action",
		TypeName:  "DescribeResult",
		Kind:      KindType,
	}
	if packetOwnsRow(packet, foreignDescribe) {
		t.Fatalf("SchemaDescribe packet should not own %s", foreignDescribe.SurfaceID)
	}
}

func TestDatabaseBatchablePacketUsesCanonicalDatabaseNamespace(t *testing.T) {
	packet, ok := AreaPacketByName("Core.Runtime.Database.Batchable")
	if !ok {
		t.Fatal("missing Database.Batchable packet")
	}
	batchableRow := SurfaceLedgerRow{
		SurfaceID:  ApexMemberID("Database", "Batchable", "start", []string{"Database.BatchableContext"}),
		Product:    ProductApex,
		Namespace:  "Database",
		TypeName:   "Batchable",
		MemberName: "start",
		Kind:       KindMethod,
	}
	if !packetOwnsRow(packet, batchableRow) {
		t.Fatalf("Database.Batchable packet should own %s", batchableRow.SurfaceID)
	}
	stdlibDatabaseRow := SurfaceLedgerRow{
		SurfaceID: ApexMemberID("System", "Database", "getQueryLocatorWithBinds", nil),
		Product:   ProductApex,
		Namespace: "System.Database",
		TypeName:  "getQueryLocatorWithBinds",
		Kind:      KindMethod,
	}
	if !packetOwnsRow(packet, stdlibDatabaseRow) {
		t.Fatalf("Database.Batchable packet should own stdlib matrix row %s", stdlibDatabaseRow.SurfaceID)
	}
	systemPacket, ok := AreaPacketByName("Core.Runtime.SystemAndStdlib")
	if !ok {
		t.Fatal("missing SystemAndStdlib packet")
	}
	stdlibDatabaseMethodRow := SurfaceLedgerRow{
		SurfaceID:  ApexMemberID("System", "Database", "getQueryLocator", []string{"String"}),
		Product:    ProductApex,
		Namespace:  "System",
		TypeName:   "Database",
		MemberName: "getQueryLocator",
		Kind:       KindMethod,
	}
	if packetOwnsRow(systemPacket, stdlibDatabaseMethodRow) {
		t.Fatalf("SystemAndStdlib packet should not own Database batch row %s", stdlibDatabaseMethodRow.SurfaceID)
	}
}

func TestAsyncAndIsolationPacketExcludesServiceAsyncDocs(t *testing.T) {
	packet, ok := AreaPacketByName("Tests.AsyncAndIsolation")
	if !ok {
		t.Fatal("missing Tests.AsyncAndIsolation packet")
	}
	localTestRow := SurfaceLedgerRow{
		SurfaceID: ApexMemberID("System", "Test", "startTest", []string{}),
		Product:   ProductApex,
		Namespace: "System",
		TypeName:  "Test",
		Kind:      KindMethod,
	}
	if !packetOwnsRow(packet, localTestRow) {
		t.Fatalf("Tests.AsyncAndIsolation packet should own %s", localTestRow.SurfaceID)
	}
	for _, row := range []SurfaceLedgerRow{
		{
			SurfaceID: "unknown:asynch_api_batches_create",
			Product:   ProductUnknown,
			Kind:      KindGuide,
		},
		{
			SurfaceID: ApexMemberID("ConnectApi", "CommerceBuyerExperience", "calculateAdjustmentAggregates", []string{"String", "ConnectApi.OrderSummaryAdjustmentAggregatesAsyncInput"}),
			Product:   ProductApex,
			Namespace: "ConnectApi",
			TypeName:  "CommerceBuyerExperience",
			Kind:      KindMethod,
		},
		{
			SurfaceID: "tooling:ContainerAsyncRequest",
			Product:   ProductTooling,
			Kind:      KindType,
		},
	} {
		if packetOwnsRow(packet, row) {
			t.Fatalf("Tests.AsyncAndIsolation packet should not own %s", row.SurfaceID)
		}
	}
}

func TestQuerySOQLSOSLPacketExcludesToolingRows(t *testing.T) {
	packet, ok := AreaPacketByName("Query.Runtime.SOQLSOSL")
	if !ok {
		t.Fatal("missing Query.Runtime.SOQLSOSL packet")
	}
	queryGuide := SurfaceLedgerRow{
		SurfaceID:  "unknown:sforce_api_calls_soql_select_limit",
		Product:    ProductUnknown,
		Area:       AreaRuntime,
		Kind:       KindGuide,
		DocsSource: "soql-sosl",
	}
	if !packetOwnsRow(packet, queryGuide) {
		t.Fatalf("Query.Runtime.SOQLSOSL packet should own %s", queryGuide.SurfaceID)
	}
	toolingRow := SurfaceLedgerRow{
		SurfaceID:  ToolingObjectID("SOQL"),
		Product:    ProductTooling,
		Area:       AreaServer,
		Kind:       KindType,
		DocsSource: "soql-sosl",
	}
	if packetOwnsRow(packet, toolingRow) {
		t.Fatalf("Query.Runtime.SOQLSOSL packet should not own tooling row %s", toolingRow.SurfaceID)
	}
}

func TestAuraComponentsPacketExcludesNonLocalLightningRows(t *testing.T) {
	packet, ok := AreaPacketByName("UI.AuraComponents")
	if !ok {
		t.Fatal("missing AuraComponents packet")
	}
	auraDoc := SurfaceLedgerRow{
		SurfaceID: "unknown:ref_aura_application",
		Product:   ProductUnknown,
		Area:      AreaUI,
		Kind:      KindGuide,
	}
	if !packetOwnsRow(packet, auraDoc) {
		t.Fatalf("AuraComponents packet should own %s", auraDoc.SurfaceID)
	}
	for _, row := range []SurfaceLedgerRow{
		{
			SurfaceID: "rest:resources_lightning_usagebypagemetrics.get",
			Product:   ProductREST,
			Area:      AreaServer,
			Kind:      KindResource,
		},
		{
			SurfaceID: ToolingObjectID("LightningComponentBundle"),
			Product:   ProductTooling,
			Area:      AreaServer,
			Kind:      KindType,
		},
		{
			SurfaceID: ApexTypeID("ConnectApi", "LightningExtensionInformation"),
			Product:   ProductApex,
			Namespace: "ConnectApi",
			TypeName:  "LightningExtensionInformation",
			Kind:      KindType,
		},
		{
			SurfaceID: LWCModuleID("`lightning/graphql`"),
			Product:   ProductLWC,
			Area:      AreaUI,
			Kind:      KindModule,
		},
	} {
		if packetOwnsRow(packet, row) {
			t.Fatalf("AuraComponents packet should not own %s", row.SurfaceID)
		}
	}
}

func TestVisualforceComponentsPacketExcludesAPIAndMetadataRows(t *testing.T) {
	packet, ok := AreaPacketByName("UI.VisualforceComponents")
	if !ok {
		t.Fatal("missing VisualforceComponents packet")
	}
	componentDoc := SurfaceLedgerRow{
		SurfaceID: "visualforce:pages_compref_page",
		Product:   ProductVisualforce,
		Area:      AreaUI,
		Kind:      KindGuide,
	}
	if !packetOwnsRow(packet, componentDoc) {
		t.Fatalf("VisualforceComponents packet should own %s", componentDoc.SurfaceID)
	}
	for _, row := range []SurfaceLedgerRow{
		{
			SurfaceID: "unknown:ui_api_responses_visualforce_layout_component",
			Product:   ProductUnknown,
			Area:      AreaServer,
			Kind:      KindGuide,
		},
		{
			SurfaceID: ApexMemberID("ConnectApi", "UserProfileTabType", "CustomVisualForce", nil),
			Product:   ProductApex,
			Namespace: "ConnectApi",
			TypeName:  "UserProfileTabType",
			Kind:      KindProperty,
		},
		{
			SurfaceID: ApexMemberID("Metadata", "FeedLayoutComponentType", "Visualforce", nil),
			Product:   ProductApex,
			Namespace: "Metadata",
			TypeName:  "FeedLayoutComponentType",
			Kind:      KindProperty,
		},
	} {
		if packetOwnsRow(packet, row) {
			t.Fatalf("VisualforceComponents packet should not own %s", row.SurfaceID)
		}
	}
}

func TestIntegrationPacketsDoNotClaimPassiveDTONameMatches(t *testing.T) {
	tests := []struct {
		packet string
		row    SurfaceLedgerRow
	}{
		{
			packet: "Integration.BulkAPI",
			row: SurfaceLedgerRow{
				SurfaceID:     "apex:ConnectApi.TextClassificationsBulkResultsOutputRepresentation.resultsList",
				Product:       ProductApex,
				Namespace:     "ConnectApi",
				TypeName:      "ConnectApi.TextClassificationsBulkResultsOutputRepresentation",
				MemberName:    "resultsList",
				Kind:          KindProperty,
				GladeBehavior: BehaviorPassive,
			},
		},
		{
			packet: "Integration.MetadataAPI",
			row: SurfaceLedgerRow{
				SurfaceID:     "apex:ConnectApi.CdpQueryMetadataOutput.metadata",
				Product:       ProductApex,
				Namespace:     "ConnectApi",
				TypeName:      "ConnectApi.CdpQueryMetadataOutput",
				MemberName:    "metadata",
				Kind:          KindProperty,
				GladeBehavior: BehaviorPassive,
			},
		},
		{
			packet: "Integration.StreamingAPI",
			row: SurfaceLedgerRow{
				SurfaceID:               "connect-rest-api:connect_responses_personalization_streaming_app_data_connector",
				Product:                 "connect-rest-api",
				SalesforceSurfaceFamily: "connect-rest-api",
				TypeName:                "Streaming",
				Kind:                    KindType,
			},
		},
	}

	for _, tc := range tests {
		packet, ok := AreaPacketByName(tc.packet)
		if !ok {
			t.Fatalf("missing packet %s", tc.packet)
		}
		if packetOwnsRow(packet, tc.row) {
			t.Fatalf("%s should not claim %s", tc.packet, tc.row.SurfaceID)
		}
	}
}
