package surfaceledger

import (
	"sort"
	"strings"
)

func Merge(docs, org, glade, evidence []SurfaceLedgerRow) SurfaceLedger {
	byID := map[string]SurfaceLedgerRow{}
	for _, group := range [][]SurfaceLedgerRow{docs, org, glade, evidence} {
		for _, row := range group {
			row = withDefaults(row)
			row = normalizeEventBusSurfaceRow(row)
			if isAPI67RemovedSurfaceID(row.SurfaceID) {
				continue
			}
			if isNonCanonicalGeneratedSurfaceID(row.SurfaceID) {
				continue
			}
			if row.SurfaceID == "" {
				continue
			}
			key := surfaceIDKey(row.SurfaceID)
			merged := byID[key]
			merged = mergeRow(merged, row)
			Classify(&merged)
			byID[key] = merged
		}
	}
	rows := make([]SurfaceLedgerRow, 0, len(byID))
	for _, row := range byID {
		Classify(&row)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].SurfaceID < rows[j].SurfaceID
	})
	ledger := SurfaceLedger{SchemaVersion: SchemaVersion, Rows: rows}
	ledger.Summary = Summarize(rows)
	return ledger
}

func mergeRow(base, next SurfaceLedgerRow) SurfaceLedgerRow {
	if base.SurfaceID == "" {
		base = withDefaults(next)
	} else {
		base = fillIdentity(base, next)
		if next.Docs != "" && next.Docs != SourceAbsent {
			base.Docs = next.Docs
		}
		if next.Org != "" && next.Org != SourceAbsent {
			base.Org = next.Org
		}
		if shapeRank(next.GladeShape) > shapeRank(base.GladeShape) {
			base.GladeShape = next.GladeShape
		}
		if next.GladeBehavior == BehaviorUnsupported && next.Evidence == EvidenceFixture {
			base.GladeBehavior = next.GladeBehavior
		} else if behaviorRank(next.GladeBehavior) > behaviorRank(base.GladeBehavior) {
			base.GladeBehavior = next.GladeBehavior
		}
		base.Evidence = mergeEvidence(base.Evidence, next.Evidence)
		if next.DocsSource != "" {
			base.DocsSource = next.DocsSource
		}
		if next.DocsTitle != "" {
			base.DocsTitle = next.DocsTitle
		}
		if next.APIVersion != "" {
			base.APIVersion = next.APIVersion
		}
		if next.Owner != "" {
			base.Owner = next.Owner
		}
		if next.Notes != "" {
			base.Notes = next.Notes
		}
	}
	base.Sources = mergeStrings(base.Sources, next.Sources)
	return withDefaults(base)
}

func normalizeEventBusSurfaceRow(row SurfaceLedgerRow) SurfaceLedgerRow {
	if row.Product != ProductApex {
		return row
	}
	if row.Namespace == "" || row.TypeName == "" || row.MemberName == "" {
		fillFromApexID(&row)
	}
	if row.Namespace != "System" || row.TypeName != "EventBus" || canonicalApexMemberName(row.MemberName) != "publishWithAccessLevel" {
		return row
	}
	row.Parameters = canonicalApexMemberParameters(row.Namespace, row.TypeName, row.MemberName, row.Parameters)
	if row.DocsParameters != nil {
		row.DocsParameters = canonicalApexMemberParameters(row.Namespace, row.TypeName, row.MemberName, row.DocsParameters)
	}
	if row.OrgParameters != nil {
		row.OrgParameters = canonicalApexMemberParameters(row.Namespace, row.TypeName, row.MemberName, row.OrgParameters)
	}
	if row.GladeParameters != nil {
		row.GladeParameters = canonicalApexMemberParameters(row.Namespace, row.TypeName, row.MemberName, row.GladeParameters)
	}
	row.SurfaceID = ApexMemberID(row.Namespace, row.TypeName, row.MemberName, row.Parameters)
	return row
}

func fillIdentity(base, next SurfaceLedgerRow) SurfaceLedgerRow {
	if base.Product == "" || base.Product == ProductUnknown {
		base.Product = next.Product
	}
	if base.Area == "" {
		base.Area = next.Area
	}
	if base.Namespace == "" {
		base.Namespace = next.Namespace
	}
	if base.TypeName == "" {
		base.TypeName = next.TypeName
	}
	if base.MemberName == "" {
		base.MemberName = next.MemberName
	}
	if base.Resource == "" {
		base.Resource = next.Resource
	}
	if base.FieldName == "" {
		base.FieldName = next.FieldName
	}
	if base.Kind == "" {
		base.Kind = next.Kind
	}
	if base.Signature == "" {
		base.Signature = next.Signature
	}
	if base.ReturnType == "" {
		base.ReturnType = next.ReturnType
	}
	if len(base.Parameters) == 0 {
		base.Parameters = append([]string(nil), next.Parameters...)
	}
	if next.GladeReturnType != "" {
		base.GladeReturnType = next.GladeReturnType
	}
	if base.DocsReturnType == "" {
		base.DocsReturnType = next.DocsReturnType
	}
	if base.OrgReturnType == "" {
		base.OrgReturnType = next.OrgReturnType
	}
	if len(base.DocsParameters) == 0 {
		base.DocsParameters = append([]string(nil), next.DocsParameters...)
	}
	if len(base.OrgParameters) == 0 {
		base.OrgParameters = append([]string(nil), next.OrgParameters...)
	}
	if len(base.GladeParameters) == 0 {
		base.GladeParameters = append([]string(nil), next.GladeParameters...)
	}
	return base
}

func Classify(row *SurfaceLedgerRow) {
	if row == nil {
		return
	}
	*row = withDefaults(*row)
	row.GapClass = ""
	row.Bucket = ""
	switch {
	case row.Docs == SourcePresent && row.Org == SourcePresent && row.Signature != "" && row.GladeShape == ShapeSignatureKnown && row.GladeBehavior != BehaviorNone && row.Evidence != EvidenceNone && row.SignatureChanged():
		row.GapClass = GapSignatureChanged
		row.Bucket = BucketFailure
	case row.Docs == SourcePresent && row.Org == SourcePresent && row.SignatureChanged():
		row.GapClass = GapSignatureChanged
		row.Bucket = BucketFailure
	case row.GladeBehavior == BehaviorUnsupported && row.Evidence != EvidenceNone:
		row.Bucket = BucketExplicitUnsupported
	case row.GladeBehavior == BehaviorPassive:
		row.Bucket = BucketPassive
	case hasReturnTypeMismatch(*row):
		row.GapClass = GapReturnTypeMismatch
		row.Bucket = BucketFailure
	case hasParameterMismatch(*row):
		row.GapClass = GapParameterMismatch
		row.Bucket = BucketFailure
	case row.GladeBehavior == BehaviorPassive:
		row.Bucket = BucketPassive
	case row.GladeBehavior == BehaviorStubNoOp:
		row.Bucket = BucketStubNoOp
	case row.GladeBehavior == BehaviorUnsupported:
		row.Bucket = BucketExplicitUnsupported
	case isGenericObjectHelper(*row):
		row.Bucket = BucketImplemented
	case isGenericEnumHelper(*row):
		row.Bucket = BucketImplemented
	case isFixtureBackedDataReference(*row):
		row.Bucket = BucketImplemented
	case isGeneratedDataReferenceShape(*row):
		row.Bucket = BucketImplemented
	case isFixtureBackedApexShapeOnly(*row):
		row.Bucket = BucketImplemented
	case isFixtureBackedApexType(*row):
		row.Bucket = BucketImplemented
	case isFixtureBackedApexMember(*row):
		row.Bucket = BucketImplemented
	case isFixtureBackedRuntimeGuide(*row):
		row.Bucket = BucketImplemented
	case (row.GladeBehavior == BehaviorSupported || row.GladeBehavior == BehaviorPartial) && row.Evidence == EvidenceNone:
		row.GapClass = GapMissingEvidence
		row.Bucket = BucketGap
	case row.Docs == SourceAbsent && row.Org == SourceAbsent && row.GladeShape != ShapeAbsent:
		row.GapClass = GapStaleGladeShape
		row.Bucket = BucketFailure
	case (row.Docs != SourceAbsent || row.Org != SourceAbsent) && row.GladeShape == ShapeAbsent:
		row.GapClass = GapMissingShape
		row.Bucket = BucketGap
	case (row.MemberName != "" || row.Kind == KindMethod || row.Kind == KindProperty || len(row.Parameters) > 0) && row.GladeShape == ShapeTypeKnown:
		row.GapClass = GapMissingSignature
		row.Bucket = BucketGap
	case row.GladeShape != ShapeAbsent && row.GladeBehavior == BehaviorNone && needsBehavior(*row):
		row.GapClass = GapMissingBehavior
		row.Bucket = BucketGap
	case isPassiveServiceRisk(*row):
		row.GapClass = GapPassiveServiceRisk
		row.Bucket = BucketFailure
	case isEvidenceOnlyRuntimeGuide(*row):
		row.Bucket = BucketImplemented
	case row.GladeBehavior == BehaviorSupported:
		row.Bucket = BucketImplemented
	case row.GladeBehavior == BehaviorPartial:
		row.Bucket = BucketPartial
	default:
		row.Bucket = BucketGap
	}
}

func (row SurfaceLedgerRow) SignatureChanged() bool {
	return row.Docs == SourceChanged || row.Org == SourceChanged
}

func hasReturnTypeMismatch(row SurfaceLedgerRow) bool {
	glade := concreteComparableTypeForRow(row, row.GladeReturnType)
	if glade == "" {
		return false
	}
	org := concreteComparableTypeForRow(row, row.OrgReturnType)
	if org != "" {
		return org != glade
	}
	docs := concreteComparableTypeForRow(row, row.DocsReturnType)
	if docs != "" && docs != glade {
		return true
	}
	return false
}

func hasParameterMismatch(row SurfaceLedgerRow) bool {
	if len(row.GladeParameters) == 0 {
		return false
	}
	if len(row.OrgParameters) > 0 {
		return !sameComparableTypesForRow(row, row.OrgParameters, row.GladeParameters)
	}
	if len(row.DocsParameters) > 0 && !sameComparableTypesForRow(row, row.DocsParameters, row.GladeParameters) {
		return true
	}
	return false
}

func sameComparableTypesForRow(row SurfaceLedgerRow, a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if concreteComparableTypeForRow(row, a[i]) != concreteComparableTypeForRow(row, b[i]) {
			return false
		}
	}
	return true
}

func concreteComparableTypeForRow(row SurfaceLedgerRow, value string) string {
	value = canonicalComparableType(value)
	if value == "" || strings.EqualFold(value, "void") {
		return ""
	}
	value = strings.ReplaceAll(value, " ", "")
	value = strings.TrimPrefix(value, "System.")
	value = asciiLowerIdentityKey(value)
	if comparableSystemVersionRow(row) && (value == "version" || value == "system.version" || value == "package.version") {
		return "package.version"
	}
	if comparableDatabaseDeleteFilterRow(row) {
		switch value {
		case "database.cursor.deletefilter", "database.paginationcursor.deletefilter", "cursor.deletefilter", "paginationcursor.deletefilter":
			return "database.deletefilter"
		case "list<database.cursor.deletefilter>", "list<database.paginationcursor.deletefilter>", "list<cursor.deletefilter>", "list<paginationcursor.deletefilter>":
			return "list<database.deletefilter>"
		}
	}
	if comparableMetadataRetrieveRow(row) {
		switch value {
		case "metadata.custommetadata", "metadata.metadata":
			return "metadata.metadata"
		case "list<metadata.custommetadata>", "list<metadata.metadata>":
			return "list<metadata.metadata>"
		}
	}
	if comparableDatabaseCountQueryWithBindsRow(row) && (value == "list<sobject>" || value == "list<object>") {
		return "integer"
	}
	if comparableUUIDRandomUUIDRow(row) && (value == "uuid" || value == "system.uuid" || value == "string") {
		return "string"
	}
	if comparableMessagingGenericBuilderBuildRow(row) && (value == "messaging.actionresult" || value == "messaging.actionablenotification") {
		return "messaging.builder.result"
	}
	if comparableSystemListGetRow(row) {
		return "list-element"
	}
	if value == "object" {
		return ""
	}
	if value == "id" || (row.Product == ProductApex && strings.EqualFold(row.Namespace, "System") && strings.EqualFold(row.TypeName, "Id") && value == "string") {
		return "string"
	}
	if genericCollectionComparableRow(row) {
		if strings.EqualFold(row.MemberName, "get") || strings.EqualFold(row.MemberName, "remove") {
			return "collection-element"
		}
		if strings.HasPrefix(value, "list<") {
			return "list<*>"
		}
		if strings.HasPrefix(value, "set<") {
			return "set<*>"
		}
		if strings.HasPrefix(value, "map<") {
			return "map<*,*>"
		}
		if value == "list" {
			return "list<*>"
		}
		if value == "set" {
			return "set<*>"
		}
		if value == "map" {
			return "map<*,*>"
		}
	}
	if comparableDatabaseListParameterRow(row) && (value == "list" || value == "list<object>" || value == "list<sobject>") {
		return "list<object>"
	}
	return value
}

func canonicalComparableType(value string) string {
	value = cleanIdentityPart(value)
	if strings.HasSuffix(value, "[]") {
		return "List<" + canonicalComparableType(strings.TrimSuffix(value, "[]")) + ">"
	}
	if open := strings.IndexByte(value, '<'); open > 0 && strings.HasSuffix(value, ">") {
		base := canonicalParameterType(value[:open])
		args := splitSurfaceParameterList(strings.TrimSuffix(value[open+1:], ">"))
		for i := range args {
			args[i] = canonicalComparableType(args[i])
		}
		return base + "<" + strings.Join(args, ",") + ">"
	}
	return canonicalParameterType(value)
}

func genericCollectionComparableRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		strings.EqualFold(row.Namespace, "System") &&
		(strings.EqualFold(row.TypeName, "List") || strings.EqualFold(row.TypeName, "Set") || strings.EqualFold(row.TypeName, "Map"))
}

func comparableSystemListGetRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		row.Kind == KindMethod &&
		strings.EqualFold(row.Namespace, "System") &&
		strings.EqualFold(row.TypeName, "List") &&
		strings.EqualFold(row.MemberName, "get") &&
		len(row.Parameters) == 1 &&
		strings.EqualFold(canonicalComparableType(row.Parameters[0]), "Integer")
}

func comparableSystemVersionRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		strings.EqualFold(row.Namespace, "System") &&
		strings.EqualFold(row.TypeName, "System") &&
		strings.EqualFold(row.MemberName, "runAs")
}

func comparableDatabaseDeleteFilterRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		strings.EqualFold(row.Namespace, "Database.Cursor") &&
		strings.EqualFold(row.TypeName, "DeleteFilter") &&
		(strings.EqualFold(row.MemberName, "valueOf") || strings.EqualFold(row.MemberName, "values"))
}

func comparableMetadataRetrieveRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		strings.EqualFold(row.Namespace, "Metadata") &&
		strings.EqualFold(row.TypeName, "Operations") &&
		strings.EqualFold(row.MemberName, "retrieve")
}

func comparableDatabaseCountQueryWithBindsRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		strings.EqualFold(row.Namespace, "System") &&
		strings.EqualFold(row.TypeName, "Database") &&
		strings.EqualFold(row.MemberName, "countQueryWithBinds")
}

func comparableDatabaseListParameterRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		strings.EqualFold(row.Namespace, "System") &&
		strings.EqualFold(row.TypeName, "Database") &&
		(strings.EqualFold(row.MemberName, "getQueryLocator") || strings.EqualFold(row.MemberName, "countQueryWithBinds"))
}

func comparableUUIDRandomUUIDRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		strings.EqualFold(row.Namespace, "System") &&
		strings.EqualFold(row.TypeName, "UUID") &&
		strings.EqualFold(row.MemberName, "randomUUID")
}

func comparableMessagingGenericBuilderBuildRow(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		strings.EqualFold(row.Namespace, "Messaging") &&
		strings.EqualFold(row.TypeName, "Builder") &&
		strings.EqualFold(row.MemberName, "build")
}

func needsBehavior(row SurfaceLedgerRow) bool {
	return row.Area == AreaRuntime || row.Area == AreaServer || row.Kind == KindMethod || row.Kind == KindResource
}

func isGenericObjectHelper(row SurfaceLedgerRow) bool {
	if row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
		return false
	}
	switch row.MemberName {
	case "clone", "hashCode", "toString":
		return len(row.Parameters) == 0
	case "equals":
		return len(row.Parameters) == 1 && row.Parameters[0] == "Object"
	default:
		return false
	}
}

func isGenericEnumHelper(row SurfaceLedgerRow) bool {
	if row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
		return false
	}
	if row.Kind == KindProperty && row.MemberName == strings.ToUpper(row.MemberName) && row.MemberName != "" {
		return true
	}
	switch row.MemberName {
	case "ordinal", "values":
		return len(row.Parameters) == 0
	case "valueOf":
		return len(row.Parameters) == 1 && row.Parameters[0] == "String"
	default:
		return false
	}
}

func isFixtureBackedDataReference(row SurfaceLedgerRow) bool {
	return row.Product == ProductDataRef &&
		row.GladeShape != ShapeAbsent &&
		row.GladeBehavior == BehaviorSupported &&
		row.Evidence != EvidenceNone
}

func isGeneratedDataReferenceShape(row SurfaceLedgerRow) bool {
	return row.Product == ProductDataRef &&
		row.GladeShape == ShapeGenerated &&
		row.GladeBehavior == BehaviorSupported &&
		(row.ShapeSource == SourceStandardSObjectGeneratedShape || hasSource(row.Sources, SourceStandardSObjectGeneratedShape))
}

func isFixtureBackedApexType(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		row.Kind == KindType &&
		row.GladeShape != ShapeAbsent &&
		row.GladeBehavior == BehaviorSupported &&
		row.Evidence != EvidenceNone
}

func isFixtureBackedApexShapeOnly(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		row.Docs == SourceAbsent &&
		row.Org == SourceAbsent &&
		row.GladeShape != ShapeAbsent &&
		row.GladeBehavior == BehaviorNone &&
		row.Evidence != EvidenceNone
}

func isFixtureBackedApexMember(row SurfaceLedgerRow) bool {
	return row.Product == ProductApex &&
		(row.Kind == KindMethod || row.Kind == KindProperty || row.Kind == KindField) &&
		row.GladeShape != ShapeAbsent &&
		row.GladeBehavior == BehaviorSupported &&
		row.Evidence != EvidenceNone
}

func isFixtureBackedRuntimeGuide(row SurfaceLedgerRow) bool {
	return row.Product == ProductUnknown &&
		row.Area == AreaRuntime &&
		strings.HasPrefix(row.SurfaceID, "unknown:") &&
		row.Docs != SourceAbsent &&
		row.GladeBehavior == BehaviorSupported &&
		row.Evidence != EvidenceNone
}

func isEvidenceOnlyRuntimeGuide(row SurfaceLedgerRow) bool {
	return row.Product == ProductUnknown &&
		row.Area == AreaRuntime &&
		strings.HasPrefix(row.SurfaceID, "unknown:") &&
		row.Docs == SourceAbsent &&
		row.Evidence != EvidenceNone
}

func isPassiveServiceRisk(row SurfaceLedgerRow) bool {
	return row.GladeBehavior == BehaviorPassive && (row.Area == AreaServer || row.Kind == KindResource)
}

func Summarize(rows []SurfaceLedgerRow) LedgerSummary {
	summary := LedgerSummary{
		Gaps:     map[string]int{},
		Failures: map[string]int{},
		Total:    len(rows),
	}
	for _, row := range rows {
		switch row.Bucket {
		case BucketImplemented:
			summary.Implemented++
		case BucketPartial:
			summary.Partial++
		case BucketPassive:
			summary.Passive++
		case BucketStubNoOp:
			summary.StubNoOp++
		case BucketExplicitUnsupported:
			summary.ExplicitUnsupported++
		case BucketFailure:
			summary.Failures[row.GapClass]++
		default:
			summary.Gaps[row.GapClass]++
		}
	}
	return summary
}

func shapeRank(state ShapeState) int {
	switch state {
	case ShapeGenerated:
		return 3
	case ShapeSignatureKnown:
		return 2
	case ShapeTypeKnown:
		return 1
	default:
		return 0
	}
}

func behaviorRank(state BehaviorState) int {
	switch state {
	case BehaviorSupported:
		return 4
	case BehaviorPartial:
		return 3
	case BehaviorUnsupported:
		return 2
	case BehaviorStubNoOp:
		return 1
	case BehaviorPassive:
		return 1
	default:
		return 0
	}
}

func mergeEvidence(a, b EvidenceState) EvidenceState {
	if a == EvidenceFixtureAndOracle || b == EvidenceFixtureAndOracle {
		return EvidenceFixtureAndOracle
	}
	if (a == EvidenceFixture && b == EvidenceOracle) || (a == EvidenceOracle && b == EvidenceFixture) {
		return EvidenceFixtureAndOracle
	}
	if b != "" && b != EvidenceNone {
		return b
	}
	if a != "" {
		return a
	}
	return EvidenceNone
}

func mergeStrings(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range append(append([]string(nil), a...), b...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func hasSource(values []string, source string) bool {
	for _, value := range values {
		if value == source {
			return true
		}
	}
	return false
}
