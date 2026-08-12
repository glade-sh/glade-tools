package surfaceledger

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergeCombinesSourcesBySurfaceID(t *testing.T) {
	id := ApexMemberID("System", "Label", "get", []string{"String", "String"})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod})},
		[]SurfaceLedgerRow{RowFromOrg(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod})},
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	if len(ledger.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(ledger.Rows))
	}
	row := ledger.Rows[0]
	if row.Docs != SourcePresent || row.Org != SourcePresent || row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
		t.Fatalf("merged row states = docs:%s org:%s shape:%s behavior:%s evidence:%s", row.Docs, row.Org, row.GladeShape, row.GladeBehavior, row.Evidence)
	}
	if row.Bucket != BucketImplemented {
		t.Fatalf("bucket = %q, want %q", row.Bucket, BucketImplemented)
	}
}

func TestMergePreservesSourceSpecificTypes(t *testing.T) {
	id := ApexMemberID("ConnectApi", "ManagedContentVersionCollection", "items", nil)
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindProperty, ReturnType: "List<ConnectApi.ManagedContentVersion>"})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindProperty, ReturnType: "String", GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindProperty, Evidence: EvidenceFixture})},
	)

	row := ledger.Rows[0]
	if row.DocsReturnType != "List<ConnectApi.ManagedContentVersion>" || row.GladeReturnType != "String" {
		t.Fatalf("source return types = docs:%q glade:%q", row.DocsReturnType, row.GladeReturnType)
	}
	if row.Bucket != BucketFailure || row.GapClass != GapReturnTypeMismatch {
		t.Fatalf("bucket/gap = %q/%q, want failure/%s", row.Bucket, row.GapClass, GapReturnTypeMismatch)
	}
}

func TestMergeClassifiesParameterMismatch(t *testing.T) {
	id := ApexMemberID("System", "List", "List", []string{"Set<T>"})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Parameters: []string{"Set<T>"}})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Parameters: []string{"List<T>"}, GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketFailure || row.GapClass != GapParameterMismatch {
		t.Fatalf("bucket/gap = %q/%q, want failure/%s", row.Bucket, row.GapClass, GapParameterMismatch)
	}
}

func TestMergeDoesNotClassifyEquivalentApexTypeSpellingsAsMismatch(t *testing.T) {
	id := ApexMemberID("System", "Probe", "touch", []string{"List<Id>", "Map<String,Object>", "List<Object>", "List<PageReference>", "Cache.Visibility"})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{
			SurfaceID:  id,
			Product:    ProductApex,
			Area:       AreaRuntime,
			Kind:       KindMethod,
			ReturnType: "List<ID>",
			Parameters: []string{"List<ID>", "Map<String,ANY>", "sObject[]", "System.PageReference[]", "cache.Visibility"},
		})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          KindMethod,
			ReturnType:    "List<Id>",
			Parameters:    []string{"List<Id>", "Map<String,Object>", "List<Object>", "List<PageReference>", "Cache.Visibility"},
			GladeBehavior: BehaviorSupported,
		})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented: %#v", row.Bucket, row.GapClass, row)
	}
}

func TestMergePrefersOrgShapeOverWeakDocsReturnType(t *testing.T) {
	id := ApexMemberID("Schema", "DescribeSObjectResult", "getChildRelationships", []string{})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "Schema.ChildRelationship"})},
		[]SurfaceLedgerRow{RowFromOrg(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "List<Schema.ChildRelationship>"})},
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "List<Schema.ChildRelationship>", GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented: %#v", row.Bucket, row.GapClass, row)
	}
}

func TestMergeTreatsSystemCollectionGenericInstantiationsAsComparable(t *testing.T) {
	id := ApexMemberID("System", "List", "clone", []string{})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "System", TypeName: "List", Kind: KindMethod, ReturnType: "List<Object>"})},
		[]SurfaceLedgerRow{RowFromOrg(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "System", TypeName: "List", Kind: KindMethod, ReturnType: "List<String>"})},
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "System", TypeName: "List", Kind: KindMethod, ReturnType: "List<Boolean>", GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "System", TypeName: "List", Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented: %#v", row.Bucket, row.GapClass, row)
	}
}

func TestMergeTreatsSystemListGetGenericReturnSpecializationsAsComparable(t *testing.T) {
	id := ApexMemberID("System", "List", "get", []string{"Integer"})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{
			SurfaceID:  id,
			Product:    ProductApex,
			Area:       AreaRuntime,
			Namespace:  "System",
			TypeName:   "List",
			MemberName: "get",
			Kind:       KindMethod,
			ReturnType: "Object",
			Parameters: []string{"Integer"},
		})},
		[]SurfaceLedgerRow{RowFromOrg(SurfaceLedgerRow{
			SurfaceID:  id,
			Product:    ProductApex,
			Area:       AreaRuntime,
			Namespace:  "System",
			TypeName:   "List",
			MemberName: "get",
			Kind:       KindMethod,
			ReturnType: "String",
			Parameters: []string{"Integer"},
		})},
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Namespace:     "System",
			TypeName:      "List",
			MemberName:    "get",
			Kind:          KindMethod,
			ReturnType:    "Id",
			Parameters:    []string{"Integer"},
			GladeBehavior: BehaviorSupported,
		})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{
			SurfaceID:  id,
			Product:    ProductApex,
			Area:       AreaRuntime,
			Namespace:  "System",
			TypeName:   "List",
			MemberName: "get",
			Kind:       KindMethod,
			Evidence:   EvidenceFixture,
		})},
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented/no gap: %#v", row.Bucket, row.GapClass, row)
	}
}

func TestMergeTreatsIdAndStringAsComparableApexIdentityTypes(t *testing.T) {
	id := ApexMemberID("ApexPages", "StandardController", "getId", []string{})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "String"})},
		[]SurfaceLedgerRow{RowFromOrg(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "String"})},
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "Id", GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented: %#v", row.Bucket, row.GapClass, row)
	}
}

func TestMergeLetsUnsupportedFixtureEvidenceOverrideGeneratedSupport(t *testing.T) {
	id := ApexMemberID("System", "WebStoreContext", "getCommerceContext", []string{})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture})},
	)

	if len(ledger.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(ledger.Rows))
	}
	row := ledger.Rows[0]
	if row.GladeBehavior != BehaviorUnsupported || row.Evidence != EvidenceFixture || row.Bucket != BucketExplicitUnsupported {
		t.Fatalf("merged row behavior/evidence/bucket = %s/%s/%s, want unsupported/fixture/explicitUnsupported", row.GladeBehavior, row.Evidence, row.Bucket)
	}
}

func TestMergeLetsUnsupportedFixtureEvidenceOverrideSignatureMismatch(t *testing.T) {
	id := ApexMemberID("", "Answers", "findSimilar", []string{"Object"})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "List<ID>", Parameters: []string{"Question"}})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "List<Id>", Parameters: []string{"Object"}, GladeBehavior: BehaviorUnsupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, GladeBehavior: BehaviorUnsupported, Evidence: EvidenceFixture})},
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketExplicitUnsupported || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want explicitUnsupported/no gap", row.Bucket, row.GapClass)
	}
}

func TestMergeDoesNotLetUnsupportedWithoutEvidenceHideSignatureMismatch(t *testing.T) {
	id := ApexMemberID("System", "UnsupportedProbe", "check", []string{"String"})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "String", Parameters: []string{"String"}})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "Boolean", Parameters: []string{"String"}, GladeBehavior: BehaviorUnsupported})},
		nil,
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketFailure || row.GapClass != GapReturnTypeMismatch {
		t.Fatalf("bucket/gap = %q/%q, want failure/%s", row.Bucket, row.GapClass, GapReturnTypeMismatch)
	}
}

func TestMergeClassifiesDocsAbsentPassiveToolingMismatchAsPassive(t *testing.T) {
	id := ApexMemberID("Slack", "Builder", "appId", []string{"String"})
	ledger := Merge(
		nil,
		[]SurfaceLedgerRow{RowFromOrg(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "Slack.RequestContext.Builder", Parameters: []string{"String"}})},
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, ReturnType: "Slack.TeamIntegrationLogsRequest.Builder", Parameters: []string{"String"}, GladeBehavior: BehaviorPassive})},
		nil,
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketPassive || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want passive/no gap", row.Bucket, row.GapClass)
	}
}

func TestMergeClassifiesDocsPresentPassiveMismatchAsPassive(t *testing.T) {
	id := ApexMemberID("ConnectApi", "CreatedFile", "success", nil)
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "ConnectApi", TypeName: "CreatedFile", Kind: KindProperty, ReturnType: "Boolean"})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "ConnectApi", TypeName: "CreatedFile", Kind: KindProperty, ReturnType: "Object", GladeBehavior: BehaviorPassive})},
		nil,
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketPassive || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want passive/no gap", row.Bucket, row.GapClass)
	}
}

func TestMergeTreatsObjectAsWeakComparableType(t *testing.T) {
	id := ApexMemberID("Cache", "Org", "remove", []string{"String"})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "Cache", TypeName: "Org", Kind: KindMethod, ReturnType: "Boolean", Parameters: []string{"String"}})},
		[]SurfaceLedgerRow{RowFromOrg(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "Cache", TypeName: "Org", Kind: KindMethod, ReturnType: "Boolean", Parameters: []string{"String"}})},
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "Cache", TypeName: "Org", Kind: KindMethod, ReturnType: "Object", Parameters: []string{"String"}, GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Namespace: "Cache", TypeName: "Org", Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	row := ledger.Rows[0]
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented: %#v", row.Bucket, row.GapClass, row)
	}
}

func TestMergeTreatsTailTypeSpellingsAsComparable(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		namespace   string
		typeName    string
		memberName  string
		docsReturn  string
		orgReturn   string
		gladeReturn string
		docsParams  []string
		orgParams   []string
		gladeParams []string
	}{
		{
			name:        "database delete filter values",
			id:          ApexMemberID("Database.Cursor", "DeleteFilter", "values", []string{}),
			namespace:   "Database.Cursor",
			typeName:    "DeleteFilter",
			memberName:  "values",
			docsReturn:  "List<Cursor.DeleteFilter>",
			gladeReturn: "List<PaginationCursor.DeleteFilter>",
		},
		{
			name:        "database delete filter valueOf",
			id:          ApexMemberID("Database.Cursor", "DeleteFilter", "valueOf", []string{"String"}),
			namespace:   "Database.Cursor",
			typeName:    "DeleteFilter",
			memberName:  "valueOf",
			orgReturn:   "Database.Cursor.DeleteFilter",
			gladeReturn: "Database.PaginationCursor.DeleteFilter",
			docsParams:  []string{"String"},
			orgParams:   []string{"String"},
			gladeParams: []string{"String"},
		},
		{
			name:        "metadata retrieve custom metadata subtype",
			id:          ApexMemberID("Metadata", "Operations", "retrieve", []string{"Object"}),
			namespace:   "Metadata",
			typeName:    "Operations",
			memberName:  "retrieve",
			orgReturn:   "List<Metadata.Metadata>",
			gladeReturn: "List<Metadata.CustomMetadata>",
			docsParams:  []string{"Object"},
			orgParams:   []string{"Object"},
			gladeParams: []string{"Object"},
		},
		{
			name:        "count query with binds tooling return",
			id:          ApexMemberID("System", "Database", "countQueryWithBinds", []string{"String", "Map<String,Object>", "AccessLevel"}),
			namespace:   "System",
			typeName:    "Database",
			memberName:  "countQueryWithBinds",
			orgReturn:   "List<SObject>",
			gladeReturn: "Integer",
			docsParams:  []string{"String", "Map<String,Object>", "AccessLevel"},
			orgParams:   []string{"String", "Map<String,Object>", "AccessLevel"},
			gladeParams: []string{"String", "Map<String,Object>", "AccessLevel"},
		},
		{
			name:        "uuid random uuid string backing",
			id:          ApexMemberID("System", "UUID", "randomUUID", []string{}),
			namespace:   "System",
			typeName:    "UUID",
			memberName:  "randomUUID",
			docsReturn:  "System.UUID",
			orgReturn:   "System.UUID",
			gladeReturn: "String",
		},
		{
			name:        "system runAs package version",
			id:          ApexMemberID("System", "System", "runAs", []string{"Package.Version"}),
			namespace:   "System",
			typeName:    "System",
			memberName:  "runAs",
			docsParams:  []string{"System.Version"},
			orgParams:   []string{"Package.Version"},
			gladeParams: []string{"Version"},
		},
		{
			name:        "raw collection parameter",
			id:          ApexMemberID("System", "Database", "getQueryLocator", []string{"List<Object>", "AccessLevel"}),
			namespace:   "System",
			typeName:    "Database",
			memberName:  "getQueryLocator",
			docsParams:  []string{"sObject []", "System.AccessLevel"},
			orgParams:   []string{"List", "AccessLevel"},
			gladeParams: []string{"List<Object>", "AccessLevel"},
		},
		{
			name:        "list remove returns element",
			id:          ApexMemberID("System", "List", "remove", []string{"Integer"}),
			namespace:   "System",
			typeName:    "List",
			memberName:  "remove",
			orgReturn:   "Object",
			gladeReturn: "Boolean",
			docsParams:  []string{"Integer"},
			orgParams:   []string{"Integer"},
			gladeParams: []string{"Integer"},
		},
		{
			name:        "messaging generic builder bridge",
			id:          ApexMemberID("Messaging", "Builder", "build", []string{}),
			namespace:   "Messaging",
			typeName:    "Builder",
			memberName:  "build",
			orgReturn:   "Messaging.ActionableNotification",
			gladeReturn: "Messaging.ActionResult",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ledger := Merge(
				[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: tc.id, Product: ProductApex, Area: AreaRuntime, Namespace: tc.namespace, TypeName: tc.typeName, MemberName: tc.memberName, Kind: KindMethod, ReturnType: tc.docsReturn, Parameters: tc.docsParams})},
				[]SurfaceLedgerRow{RowFromOrg(SurfaceLedgerRow{SurfaceID: tc.id, Product: ProductApex, Area: AreaRuntime, Namespace: tc.namespace, TypeName: tc.typeName, MemberName: tc.memberName, Kind: KindMethod, ReturnType: tc.orgReturn, Parameters: tc.orgParams})},
				[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: tc.id, Product: ProductApex, Area: AreaRuntime, Namespace: tc.namespace, TypeName: tc.typeName, MemberName: tc.memberName, Kind: KindMethod, ReturnType: tc.gladeReturn, Parameters: tc.gladeParams, GladeBehavior: BehaviorSupported})},
				[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: tc.id, Product: ProductApex, Area: AreaRuntime, Namespace: tc.namespace, TypeName: tc.typeName, MemberName: tc.memberName, Kind: KindMethod, Evidence: EvidenceFixture})},
			)
			row := ledger.Rows[0]
			if row.Bucket != BucketImplemented || row.GapClass != "" {
				t.Fatalf("bucket/gap = %q/%q, want implemented: %#v", row.Bucket, row.GapClass, row)
			}
		})
	}
}

func TestMergeCombinesApexSurfaceIDsCaseInsensitively(t *testing.T) {
	docsID := ApexMemberID("Schema", "DescribeSObjectResult", "getSobjectType", []string{})
	gladeID := ApexMemberID("Schema", "DescribeSObjectResult", "getSObjectType", []string{})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: docsID, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: gladeID, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: gladeID, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	if len(ledger.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(ledger.Rows))
	}
	row := ledger.Rows[0]
	if row.SurfaceID != docsID {
		t.Fatalf("surface id = %q, want docs spelling %q", row.SurfaceID, docsID)
	}
	if row.Docs != SourcePresent || row.GladeShape != ShapeSignatureKnown || row.GladeBehavior != BehaviorSupported || row.Evidence != EvidenceFixture {
		t.Fatalf("merged row states = docs:%s shape:%s behavior:%s evidence:%s", row.Docs, row.GladeShape, row.GladeBehavior, row.Evidence)
	}
}

func TestMergeCombinesSchemaRootClassAcrossSystemNamespace(t *testing.T) {
	docsID := ApexMemberID("Schema", "Schema", "describeTabs", []string{})
	gladeID := ApexMemberID("System", "Schema", "describeTabs", []string{})
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: docsID, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod})},
		nil,
		[]SurfaceLedgerRow{RowFromGladeShape(SurfaceLedgerRow{SurfaceID: gladeID, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, GladeBehavior: BehaviorSupported})},
		[]SurfaceLedgerRow{RowFromEvidence(SurfaceLedgerRow{SurfaceID: docsID, Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Evidence: EvidenceFixture})},
	)

	if len(ledger.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(ledger.Rows))
	}
	row := ledger.Rows[0]
	if row.SurfaceID != docsID {
		t.Fatalf("surface id = %q, want docs spelling %q", row.SurfaceID, docsID)
	}
	if row.Bucket != BucketImplemented {
		t.Fatalf("bucket = %q, want %q", row.Bucket, BucketImplemented)
	}
}

func TestMergeClassifiesGeneratedDataReferenceShapeWithDocsRow(t *testing.T) {
	id := DataObjectID("AIInsightAction")
	ledger := Merge(
		[]SurfaceLedgerRow{RowFromDocs(SurfaceLedgerRow{SurfaceID: id, Product: ProductDataRef, Area: AreaData, Kind: KindType})},
		nil,
		[]SurfaceLedgerRow{RowFromGeneratedDataReferenceShape(SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductDataRef,
			Area:          AreaData,
			Kind:          KindType,
			GladeBehavior: BehaviorSupported,
			Sources:       []string{SourceStandardSObjectGeneratedShape},
		})},
		nil,
	)

	if len(ledger.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(ledger.Rows))
	}
	row := ledger.Rows[0]
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented", row.Bucket, row.GapClass)
	}
	if !hasSource(row.Sources, SourceStandardSObjectGeneratedShape) {
		t.Fatalf("sources = %#v, want generated shape source", row.Sources)
	}
}

func TestMergePreservesNamespaceForRoundTrippedApexRowWithEmptyParameters(t *testing.T) {
	// JSON snapshots omit empty parameter slices. A row such as System.Answers.Answers()
	// must retain its namespace identity rather than being re-parsed from the surface ID,
	// which drops the System namespace for unqualified types.
	id := ApexMemberID("System", "Answers", "Answers", []string{})
	row := SurfaceLedgerRow{
		SurfaceID:     id,
		Product:       ProductApex,
		Area:          AreaRuntime,
		Namespace:     "System",
		TypeName:      "Answers",
		MemberName:    "Answers",
		Kind:          KindMethod,
		Parameters:    []string{},
		Docs:          SourcePresent,
		Org:           SourceAbsent,
		GladeShape:    ShapeSignatureKnown,
		GladeBehavior: BehaviorSupported,
		Evidence:      EvidenceFixture,
	}

	// Round-trip through JSON so the empty parameter slice becomes nil, as in a snapshot.
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	var decoded SurfaceLedgerRow
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal row: %v", err)
	}

	ledger := Merge(nil, nil, []SurfaceLedgerRow{decoded}, nil)
	if len(ledger.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(ledger.Rows))
	}
	got := ledger.Rows[0]
	if got.Namespace != "System" {
		t.Fatalf("namespace = %q, want %q", got.Namespace, "System")
	}
	if got.Bucket != BucketImplemented || got.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented: %#v", got.Bucket, got.GapClass, got)
	}
}

func TestClassifyFixtureBackedLWCBridgeRowDoesNotRemainBlankGap(t *testing.T) {
	row := SurfaceLedgerRow{
		SurfaceID:     "lwc:apex-wire.sobject-json",
		Product:       ProductLWC,
		Area:          AreaUI,
		Kind:          KindMethod,
		Docs:          SourceAbsent,
		Org:           SourceAbsent,
		GladeShape:    ShapeAbsent,
		GladeBehavior: BehaviorSupported,
		Evidence:      EvidenceFixture,
	}

	Classify(&row)
	if row.Bucket == BucketGap && row.GapClass == "" {
		t.Fatalf("blank gap remained after fixture evidence merge: %#v", row)
	}
}

func TestClassifyEvidenceOnlyRuntimeGuideImplemented(t *testing.T) {
	row := SurfaceLedgerRow{
		SurfaceID:     "unknown:sforce_api_calls_soql_select_orderby",
		Product:       ProductUnknown,
		Area:          AreaRuntime,
		Kind:          KindType,
		Docs:          SourceAbsent,
		Org:           SourceAbsent,
		GladeShape:    ShapeAbsent,
		GladeBehavior: BehaviorSupported,
		Evidence:      EvidenceFixture,
	}

	Classify(&row)
	if row.Bucket != BucketImplemented || row.GapClass != "" {
		t.Fatalf("bucket/gap = %q/%q, want implemented: %#v", row.Bucket, row.GapClass, row)
	}
}

func TestClassifyGapFromStates(t *testing.T) {
	tests := []struct {
		name string
		row  SurfaceLedgerRow
		gap  string
	}{
		{
			name: "missing shape",
			row:  RowFromDocs(SurfaceLedgerRow{SurfaceID: ApexTypeID("System", "Label"), Product: ProductApex, Area: AreaRuntime, Kind: KindType}),
			gap:  GapMissingShape,
		},
		{
			name: "missing signature",
			row:  RowFromDocs(SurfaceLedgerRow{SurfaceID: ApexMemberID("System", "Label", "get", []string{"String"}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, GladeShape: ShapeTypeKnown}),
			gap:  GapMissingSignature,
		},
		{
			name: "missing behavior",
			row:  RowFromGladeShape(RowFromDocs(SurfaceLedgerRow{SurfaceID: ApexMemberID("System", "Label", "get", nil), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod})),
			gap:  GapMissingBehavior,
		},
		{
			name: "missing evidence",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("System", "String", "length", nil), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Docs: SourcePresent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceNone},
			gap:  GapMissingEvidence,
		},
		{
			name: "stale glade shape",
			row:  SurfaceLedgerRow{SurfaceID: ApexTypeID("System", "OldThing"), Product: ProductApex, Area: AreaRuntime, Kind: KindType, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeTypeKnown},
			gap:  GapStaleGladeShape,
		},
		{
			name: "fixture backed data-reference field",
			row:  SurfaceLedgerRow{SurfaceID: DataFieldID("AsyncApexJob", "CompletedDate"), Product: ProductDataRef, Area: AreaData, Kind: KindField, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeTypeKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
			gap:  "",
		},
		{
			name: "generated data-reference field",
			row:  SurfaceLedgerRow{SurfaceID: DataFieldID("AIInsightAction", "AiRecordInsightId"), Product: ProductDataRef, Area: AreaData, Kind: KindField, Docs: SourcePresent, Org: SourceAbsent, GladeShape: ShapeGenerated, GladeBehavior: BehaviorSupported, ShapeSource: SourceStandardSObjectGeneratedShape},
			gap:  "",
		},
		{
			name: "fixture backed apex marker type",
			row:  SurfaceLedgerRow{SurfaceID: ApexTypeID("Database", "Stateful"), Product: ProductApex, Area: AreaRuntime, Kind: KindType, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeTypeKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
			gap:  "",
		},
		{
			name: "fixture backed apex method",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("System", "Limits", "getBatchJobs", []string{}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Namespace: "System", TypeName: "Limits", MemberName: "getBatchJobs", Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
			gap:  "",
		},
		{
			name: "fixture backed apex shape-only method",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("System", "String", "template", []string{}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Namespace: "System", TypeName: "String", MemberName: "template", Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeTypeKnown, GladeBehavior: BehaviorNone, Evidence: EvidenceFixture},
			gap:  "",
		},
		{
			name: "fixture backed runtime guide row",
			row:  SurfaceLedgerRow{SurfaceID: "unknown:sforce_api_calls_soql_select_orderby", Product: ProductUnknown, Area: AreaRuntime, Kind: KindType, Docs: SourcePresent, Org: SourceAbsent, GladeShape: ShapeAbsent, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
			gap:  "",
		},
		{
			name: "evidence only runtime guide row",
			row:  SurfaceLedgerRow{SurfaceID: "unknown:sforce_api_calls_soql_select_orderby", Product: ProductUnknown, Area: AreaRuntime, Kind: KindType, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeAbsent, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture},
			gap:  "",
		},
		{
			name: "passive glade-only shape",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("ApexPages", "Component", "Component", []string{}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorPassive},
			gap:  "",
		},
		{
			name: "passive glade-only dto type",
			row:  SurfaceLedgerRow{SurfaceID: ApexTypeID("ConnectApi", "ApplicationFormInput"), Product: ProductApex, Area: AreaRuntime, Kind: KindType, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeTypeKnown, GladeBehavior: BehaviorPassive},
			gap:  "",
		},
		{
			name: "generic object glade-only method",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("ConnectApi", "ApplicationFormInput", "equals", []string{"Object"}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, MemberName: "equals", Parameters: []string{"Object"}, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported},
			gap:  "",
		},
		{
			name: "generic object org-backed method",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("Schema", "SoapType", "equals", []string{"Object"}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, MemberName: "equals", Parameters: []string{"Object"}, Docs: SourceAbsent, Org: SourcePresent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported},
			gap:  "",
		},
		{
			name: "explicit unsupported glade-only method",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("ConnectApi", "Billing", "createCreditMemos", []string{"ConnectApi.StandaloneCreditMemoInputRequest"}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, MemberName: "createCreditMemos", Parameters: []string{"ConnectApi.StandaloneCreditMemoInputRequest"}, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorUnsupported},
			gap:  "",
		},
		{
			name: "generic enum glade-only method",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("Database.Cursor", "DeleteFilter", "values", []string{}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, MemberName: "values", Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported},
			gap:  "",
		},
		{
			name: "enum constant glade-only property",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("Database.Cursor", "DeleteFilter", "NO_FILTER", nil), Product: ProductApex, Area: AreaRuntime, Kind: KindProperty, MemberName: "NO_FILTER", Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported},
			gap:  "",
		},
		{
			name: "enum constant org-backed property",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("Schema", "SoapType", "ADDRESS", nil), Product: ProductApex, Area: AreaRuntime, Kind: KindProperty, MemberName: "ADDRESS", Docs: SourceAbsent, Org: SourcePresent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported},
			gap:  "",
		},
		{
			name: "supported glade-only method without evidence",
			row:  SurfaceLedgerRow{SurfaceID: ApexMemberID("System", "Database", "delete", []string{"Object"}), Product: ProductApex, Area: AreaRuntime, Kind: KindMethod, MemberName: "delete", Parameters: []string{"Object"}, Docs: SourceAbsent, Org: SourceAbsent, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceNone},
			gap:  GapMissingEvidence,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Classify(&tt.row)
			if tt.row.GapClass != tt.gap {
				t.Fatalf("gap = %q, want %q", tt.row.GapClass, tt.gap)
			}
			if strings.HasPrefix(tt.name, "passive glade-only") && tt.row.Bucket != BucketPassive {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketPassive)
			}
			if (tt.name == "generic object glade-only method" || tt.name == "generic object org-backed method") && tt.row.Bucket != BucketImplemented {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketImplemented)
			}
			if tt.name == "explicit unsupported glade-only method" && tt.row.Bucket != BucketExplicitUnsupported {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketExplicitUnsupported)
			}
			if (tt.name == "generic enum glade-only method" || tt.name == "enum constant glade-only property" || tt.name == "enum constant org-backed property") && tt.row.Bucket != BucketImplemented {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketImplemented)
			}
			if tt.name == "fixture backed data-reference field" && tt.row.Bucket != BucketImplemented {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketImplemented)
			}
			if tt.name == "fixture backed apex marker type" && tt.row.Bucket != BucketImplemented {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketImplemented)
			}
			if tt.name == "fixture backed apex method" && tt.row.Bucket != BucketImplemented {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketImplemented)
			}
			if tt.name == "fixture backed apex shape-only method" && tt.row.Bucket != BucketImplemented {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketImplemented)
			}
			if tt.name == "fixture backed runtime guide row" && tt.row.Bucket != BucketImplemented {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketImplemented)
			}
			if tt.name == "evidence only runtime guide row" && tt.row.Bucket != BucketImplemented {
				t.Fatalf("bucket = %q, want %q", tt.row.Bucket, BucketImplemented)
			}
		})
	}
}
