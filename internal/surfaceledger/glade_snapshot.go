package surfaceledger

import (
	"strings"

	"github.com/glade-sh/glade/internal/apexast"
	"github.com/glade-sh/glade/internal/storage"
	"github.com/glade-sh/glade/internal/typesys"
	"github.com/glade-sh/glade/internal/visualforce"
	"github.com/glade-sh/glade/tools/internal/capability"
)

func BuildGladeSnapshot() []SurfaceLedgerRow {
	byID := map[string]SurfaceLedgerRow{}
	for _, symbol := range typesys.StandardPlatformSymbolView() {
		namespace, typeName := splitTypeName(symbol.Namespace, symbol.Name)
		id := ApexTypeID(namespace, typeName)
		byID[surfaceIDKey(id)] = RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Namespace:     namespace,
			TypeName:      typeName,
			Kind:          KindType,
			GladeBehavior: behaviorForStandardType(namespace, typeName),
			Sources:       []string{"standard-symbols"},
		})
		for _, member := range symbol.Members {
			params := memberParameterTypes(member.Parameters)
			memberName := member.Name
			if memberName == "" {
				memberName = typeName
			}
			if namespace == "System" && typeName == "EventBus" && memberName == "publishWithAccessLevel" {
				params = eventBusAccessLevelParameters(member.Type, params)
			}
			kind := gladeMemberKind(string(member.Kind))
			if string(member.Kind) == "constructor" {
				memberName = gladeConstructorMemberName(namespace, typeName, memberName)
			}
			if kind == KindProperty {
				params = nil
			}
			memberID := ApexMemberID(namespace, typeName, memberName, params)
			row := RowFromGladeShape(SurfaceLedgerRow{
				SurfaceID:  memberID,
				Product:    ProductApex,
				Area:       AreaRuntime,
				Namespace:  namespace,
				TypeName:   typeName,
				MemberName: memberName,
				Kind:       kind,
				ReturnType: member.Type,
				Parameters: params,
				Sources:    []string{"standard-symbols"},
			})
			if string(member.Kind) == "constructor" && messagingInboundEmailDTOType(namespace, typeName) {
				row.GladeBehavior = BehaviorPassive
			}
			byID[surfaceIDKey(memberID)] = row
		}
	}
	for _, entry := range capability.StdlibMatrix() {
		id := idFromStdlibAPI(entry.API)
		if id == "" {
			continue
		}
		kind := KindMethod
		if stdlibTypeAPI(entry.API) {
			kind = KindType
		}
		key := surfaceIDKey(id)
		row := byID[key]
		if row.SurfaceID == "" {
			row = SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: kind, Sources: []string{"stdlib-matrix"}}
			fillFromApexID(&row)
			if kind == KindType {
				row = RowFromGladeShape(row)
			}
		}
		row.GladeBehavior = behaviorFromCapabilityStatus(entry.Status)
		row.Notes = entry.Notes
		row.Sources = mergeStrings(row.Sources, []string{"stdlib-matrix"})
		byID[key] = withDefaults(row)
	}
	for _, entry := range capability.BuildStubBehaviorReport().Entries {
		id := idFromStubBehavior(entry)
		key := surfaceIDKey(id)
		row := byID[key]
		if row.SurfaceID == "" {
			row = SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, Kind: gladeMemberKind(entry.Kind), Sources: []string{"stub-behavior"}}
			fillFromApexID(&row)
		}
		row.GladeBehavior = mergeGladeBehavior(row.GladeBehavior, behaviorFromStubStatus(entry.Status))
		row.ReturnType = firstNonEmpty(row.ReturnType, entry.ReturnType)
		row.GladeReturnType = firstNonEmpty(row.GladeReturnType, entry.ReturnType)
		if len(row.Parameters) == 0 {
			row.Parameters = append([]string(nil), entry.Parameters...)
		}
		if len(row.GladeParameters) == 0 {
			row.GladeParameters = append([]string(nil), row.Parameters...)
		}
		row.Notes = firstNonEmpty(row.Notes, entry.Notes)
		row.Sources = mergeStrings(row.Sources, []string{"stub-behavior"})
		byID[key] = withDefaults(row)
	}
	addDataReferenceGladeRows(byID)
	addLocalTestLWCGladeRows(byID)
	addLocalTestVisualforceComponentRows(byID)
	addLocalTestAuraMetadataRows(byID)
	addUnsupportedQueryRuntimeGladeRows(byID)
	addFixtureBackedStdlibAliasRows(byID)
	addFixtureBackedSystemAliasRows(byID)
	addFixtureBackedApexAliasRows(byID)
	addFixtureBackedApexMirrorAliasRows(byID)
	addFixtureBackedInvocableActionDTORows(byID)
	addApexLanguageRuleRows(byID)
	addSurfaceClosureTailGladeRows(byID)
	addMethodFamilyShapeReconciliation(byID)
	removeNonCanonicalGeneratedRows(byID)
	rows := make([]SurfaceLedgerRow, 0, len(byID))
	for _, row := range byID {
		rows = append(rows, withDefaults(row))
	}
	sortRows(rows)
	return rows
}

func eventBusAccessLevelParameters(returnType string, params []string) []string {
	first := "SObject"
	if strings.HasPrefix(returnType, "List<") {
		first = "List<SObject>"
	}
	if len(params) == 2 {
		return []string{first, "AccessLevel"}
	}
	if len(params) == 3 {
		return []string{first, "Object", "AccessLevel"}
	}
	return params
}

type apexLanguageRuleRow struct {
	ID         string
	Namespace  string
	TypeName   string
	DocsSource string
	Notes      string
}

var apexLanguageRuleRows = []apexLanguageRuleRow{
	{
		ID:         ApexLanguageRuleID("SystemNamespaceDefaultImport"),
		Namespace:  "System",
		TypeName:   "System namespace default import",
		DocsSource: "apex-guide/apex_classes_namespaces_and_invoking_methods.md",
		Notes:      "Salesforce documents System as the default namespace; Glade sema covers qualified and unqualified System type spellings.",
	},
	{
		ID:         ApexLanguageRuleID("SchemaNamespaceImplicitImport"),
		Namespace:  "Schema",
		TypeName:   "Schema namespace implicit import",
		DocsSource: "apex-guide/apex_classes_schema_namespace_using.md",
		Notes:      "Salesforce documents Schema.* as implicitly imported; Glade sema covers qualified and short Schema type spellings.",
	},
	{
		ID:         ApexLanguageRuleID("NamespaceClassVariablePrecedence"),
		TypeName:   "Namespace, class, and variable precedence",
		DocsSource: "apex-guide/apex_classes_namespace_precedence.md",
		Notes:      "Salesforce documents expression lookup as local variable, class, then namespace; Glade sema covers shadowing and disambiguation.",
	},
	{
		ID:         ApexLanguageRuleID("TypeResolutionSystemNamespace"),
		TypeName:   "Type resolution and System namespace for types",
		DocsSource: "apex-guide/apex_classes_namespace_type_resolution.md",
		Notes:      "Salesforce documents scalar, local, class, and system type precedence; Glade sema covers inner-type-before-namespace resolution.",
	},
}

func addApexLanguageRuleRows(byID map[string]SurfaceLedgerRow) {
	for _, rule := range apexLanguageRuleRows {
		byID[surfaceIDKey(rule.ID)] = withDefaults(SurfaceLedgerRow{
			SurfaceID:     rule.ID,
			Product:       ProductApex,
			Area:          AreaFrontend,
			Namespace:     rule.Namespace,
			TypeName:      rule.TypeName,
			Kind:          KindLanguageRule,
			Docs:          SourcePresent,
			GladeShape:    ShapeTypeKnown,
			GladeBehavior: BehaviorSupported,
			Evidence:      EvidenceFixture,
			DocsSource:    rule.DocsSource,
			Owner:         "internal/sema",
			Sources:       []string{"apex-language-rules", "namespace-resolution-tests"},
			Notes:         rule.Notes,
		})
	}
}

type fixtureBackedStdlibAlias struct {
	TypeName string
	Methods  []string
}

var fixtureBackedStdlibAliases = []fixtureBackedStdlibAlias{
	{TypeName: "Blob", Methods: []string{"toString"}},
	{TypeName: "Datetime", Methods: []string{"newInstanceGmt", "valueOfGmt"}},
	{TypeName: "Decimal", Methods: []string{"abs", "longValue", "pow", "valueOf"}},
	{TypeName: "Double", Methods: []string{"valueOf"}},
	{TypeName: "Id", Methods: []string{"getSObjectType", "to15", "to18", "valueOf"}},
	{TypeName: "Integer", Methods: []string{"doubleValue", "valueOf"}},
	{TypeName: "JSON", Methods: []string{"createParser"}},
	{TypeName: "JSONParser", Methods: []string{"getBooleanValue", "getDateValue", "getText"}},
	{TypeName: "List", Methods: []string{"copyConstructor", "indexOf", "sort"}},
	{TypeName: "Long", Methods: []string{"valueOf"}},
	{TypeName: "Map", Methods: []string{"containsKey", "containsValue", "copyConstructor", "deepClone", "keySet", "toString"}},
	{TypeName: "Object", Methods: []string{"equals", "hashCode", "toString"}},
	{TypeName: "Pattern", Methods: []string{"pattern", "split"}},
	{TypeName: "RestRequest", Methods: []string{"getHeader", "getParameter"}},
	{TypeName: "RoundingMode", Methods: []string{"name", "ordinal", "toString", "valueOf", "values"}},
	{TypeName: "Set", Methods: []string{"copyConstructor", "deepClone", "remove"}},
	{TypeName: "String", Methods: []string{
		"codePointAt",
		"commonPrefix",
		"escapeCsv",
		"escapeEcmaScript",
		"escapeHtml3",
		"escapeHtml4",
		"escapeJava",
		"escapeSingleQuotes",
		"escapeUnicode",
		"escapeXml",
		"escapeXml10",
		"escapeXml11",
		"format",
		"getChars",
		"lastIndexOfAny",
		"lastOrdinalIndexOf",
		"ordinalIndexOf",
		"overlay",
		"remove",
		"removeIgnoreCase",
		"replaceAll",
		"replaceFirst",
		"replaceIgnoreCase",
		"replaceOnce",
		"rotate",
		"strip",
		"stripAll",
		"stripEnd",
		"stripStart",
		"stripToEmpty",
		"stripToNull",
		"swapCase",
		"unescapeCsv",
		"unescapeEcmaScript",
		"unescapeHtml3",
		"unescapeHtml4",
		"unescapeJava",
		"unescapeUnicode",
		"unescapeXml",
		"unescapeXml10",
		"unescapeXml11",
	}},
	{TypeName: "Time", Methods: []string{"valueOf"}},
	{TypeName: "Type", Methods: []string{"forName"}},
	{TypeName: "URL", Methods: []string{"getAuthority", "getCurrentRequestUrl", "getDefaultPort", "getHost"}},
}

func addFixtureBackedStdlibAliasRows(byID map[string]SurfaceLedgerRow) {
	for _, alias := range fixtureBackedStdlibAliases {
		for _, method := range alias.Methods {
			id := ApexMemberID("System", alias.TypeName, method, nil)
			byID[surfaceIDKey(id)] = RowFromGladeShape(SurfaceLedgerRow{
				SurfaceID:     id,
				Product:       ProductApex,
				Area:          AreaRuntime,
				Namespace:     "System",
				TypeName:      alias.TypeName,
				MemberName:    method,
				Kind:          KindMethod,
				GladeBehavior: BehaviorSupported,
				Sources:       []string{"stdlib-fixture-alias"},
				Notes:         "fixture-backed docs shorthand for a System stdlib method whose typed overloads are implemented locally",
			})
		}
	}
}

type fixtureBackedSystemAliasRow struct {
	SurfaceID string
	Kind      string
	Behavior  BehaviorState
	Notes     string
}

var fixtureBackedSystemAliasRows = []fixtureBackedSystemAliasRow{
	{SurfaceID: "apex:System.Auth.*", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostics for local Auth token, JWT, OAuth, and cloud surfaces"},
	{SurfaceID: "apex:System.Canvas.*", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostics for local Canvas app integration surfaces"},
	{SurfaceID: "apex:System.Continuation.*", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostics for local Continuation callback and callout surfaces"},
	{SurfaceID: "apex:System.Crypto.areEqualConstantTime(Blob,Blob)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Crypto constant-time Blob comparison"},
	{SurfaceID: "apex:System.CustomMetadataType.getAll", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local custom metadata static getAll access"},
	{SurfaceID: "apex:System.CustomSetting.getInstance", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local custom setting static getInstance access"},
	{SurfaceID: "apex:System.Database.DeletedRecord.getDeletedDate()", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.DeletedRecord.getDeletedDate local sync DTO accessor"},
	{SurfaceID: "apex:System.Database.DeletedRecord.getId()", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.DeletedRecord.getId local sync DTO accessor"},
	{SurfaceID: "apex:System.Database.GetDeletedResult.getDeletedRecords()", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.GetDeletedResult.getDeletedRecords local sync DTO accessor"},
	{SurfaceID: "apex:System.Database.GetUpdatedResult.getIds()", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.GetUpdatedResult.getIds local sync DTO accessor"},
	{SurfaceID: "apex:System.Database.GetUpdatedResult.getLatestDateCovered()", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.GetUpdatedResult.getLatestDateCovered local sync DTO accessor"},
	{SurfaceID: "apex:System.Database.countQuery(String,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.countQuery(String,AccessLevel) local SOQL execution"},
	{SurfaceID: "apex:System.Database.countQueryWithBinds(String,Map,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.countQueryWithBinds local SOQL bind execution"},
	{SurfaceID: "apex:System.Database.deleteImmediate(List<Object>,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.deleteImmediate list AccessLevel overload local DML"},
	{SurfaceID: "apex:System.Database.deleteImmediate(Object,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.deleteImmediate object AccessLevel overload local DML"},
	{SurfaceID: "apex:System.Database.getCursor(String,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified Object overload for Database.getCursor local cursor construction"},
	{SurfaceID: "apex:System.Database.getCursorWithBinds(String,Map,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.getCursorWithBinds local bind cursor construction"},
	{SurfaceID: "apex:System.Database.getDeleted(String,Datetime,Datetime)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.getDeleted local sync window"},
	{SurfaceID: "apex:System.Database.getPaginationCursor(String,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified Object overload for Database.getPaginationCursor local pagination cursor construction"},
	{SurfaceID: "apex:System.Database.getPaginationCursorWithBinds(String,Map,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.getPaginationCursorWithBinds local bind pagination cursor construction"},
	{SurfaceID: "apex:System.Database.getUpdated(String,Datetime,Datetime)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.getUpdated local sync window"},
	{SurfaceID: "apex:System.Database.insertImmediate(List<Object>,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.insertImmediate list AccessLevel overload local DML"},
	{SurfaceID: "apex:System.Database.insertImmediate(Object,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.insertImmediate object AccessLevel overload local DML"},
	{SurfaceID: "apex:System.Database.query(String,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.query(String,AccessLevel) local SOQL execution"},
	{SurfaceID: "apex:System.Database.queryWithBinds(String,Map,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.queryWithBinds local SOQL bind execution"},
	{SurfaceID: "apex:System.Database.updateImmediate(List<Object>,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.updateImmediate list AccessLevel overload local DML"},
	{SurfaceID: "apex:System.Database.updateImmediate(Object,Object)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.updateImmediate object AccessLevel overload local DML"},
	{SurfaceID: "apex:System.DMLOptions", Kind: KindType, Behavior: BehaviorPassive, Notes: "fixture-backed System-qualified alias for Database.DMLOptions local option shape"},
	{SurfaceID: "apex:System.DMLOptions.allowFieldTruncation", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.DMLOptions.allowFieldTruncation"},
	{SurfaceID: "apex:System.DMLOptions.assignmentRuleHeader", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.DMLOptions.assignmentRuleHeader"},
	{SurfaceID: "apex:System.DMLOptions.emailHeader", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.DMLOptions.emailHeader"},
	{SurfaceID: "apex:System.DMLOptions.localeOptions", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.DMLOptions.localeOptions"},
	{SurfaceID: "apex:System.DMLOptions.optAllOrNone", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Database.DMLOptions.optAllOrNone"},
	{SurfaceID: "apex:System.EventBus.*", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostics for local platform event delivery surfaces"},
	{SurfaceID: "apex:System.EventBus.publish", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local no-op platform event publish SaveResult behavior"},
	{SurfaceID: "apex:System.EventBus.publishAfterCommit", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostic for after-commit platform event delivery"},
	{SurfaceID: "apex:System.HierarchyCustomSetting.getOrgDefaults", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local hierarchy custom setting org-default lookup"},
	{SurfaceID: "apex:System.HttpRequest.setClientCertificate", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostic for inline client-certificate material"},
	{SurfaceID: "apex:System.HttpRequest.setClientCertificateName", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostic for named client-certificate configuration"},
	{SurfaceID: "apex:System.HttpRequest.setTimeout", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local HttpRequest timeout validation"},
	{SurfaceID: "apex:System.IllegalStateException", Kind: KindType, Behavior: BehaviorPassive, Notes: "fixture-backed System-qualified alias for the local built-in IllegalStateException type"},
	{SurfaceID: "apex:System.Integer.MAX_VALUE", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Integer.MAX_VALUE"},
	{SurfaceID: "apex:System.Integer.MIN_VALUE", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Integer.MIN_VALUE"},
	{SurfaceID: "apex:System.JSONGenerator.writeRaw(String)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for JSONGenerator.writeRaw"},
	{SurfaceID: "apex:System.JSONGenerator.writeRawField(String,String)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for JSONGenerator.writeRawField"},
	{SurfaceID: "apex:System.JSONGenerator.writeRawValue(String)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for JSONGenerator.writeRawValue"},
	{SurfaceID: "apex:System.Limits.getDmlRows", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Limits.getDmlRows"},
	{SurfaceID: "apex:System.Limits.getDmlStatements", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Limits.getDmlStatements"},
	{SurfaceID: "apex:System.Limits.getEmailInvocations", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Limits.getEmailInvocations"},
	{SurfaceID: "apex:System.Limits.getLimitDmlRows", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Limits.getLimitDmlRows"},
	{SurfaceID: "apex:System.Limits.getLimitDmlStatements", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Limits.getLimitDmlStatements"},
	{SurfaceID: "apex:System.Limits.getLimitScheduledJobs", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Limits.getLimitScheduledJobs"},
	{SurfaceID: "apex:System.Limits.getScheduledJobs", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Limits.getScheduledJobs"},
	{SurfaceID: "apex:System.Long.MAX_VALUE", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Long.MAX_VALUE"},
	{SurfaceID: "apex:System.Long.MIN_VALUE", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Long.MIN_VALUE"},
	{SurfaceID: "apex:System.Matcher.groupCount", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Matcher.groupCount"},
	{SurfaceID: "apex:System.Matcher.hasAnchoringBounds", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs shorthand for Matcher.hasAnchoringBounds()"},
	{SurfaceID: "apex:System.Matcher.hasTransparentBounds", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs shorthand for Matcher.hasTransparentBounds()"},
	{SurfaceID: "apex:System.Matcher.lookingAt", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Matcher.lookingAt"},
	{SurfaceID: "apex:System.Matcher.region", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Matcher.region"},
	{SurfaceID: "apex:System.Matcher.replaceAll", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Matcher.replaceAll"},
	{SurfaceID: "apex:System.Matcher.replaceFirst", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Matcher.replaceFirst"},
	{SurfaceID: "apex:System.Matcher.useAnchoringBounds", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs shorthand for Matcher.useAnchoringBounds(Boolean)"},
	{SurfaceID: "apex:System.Matcher.usePattern", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local Matcher.usePattern"},
	{SurfaceID: "apex:System.Matcher.useTransparentBounds", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs shorthand for Matcher.useTransparentBounds(Boolean)"},
	{SurfaceID: "apex:System.Messaging.MassEmailMessage", Kind: KindType, Behavior: BehaviorPassive, Notes: "fixture-backed System-qualified alias for the local MassEmailMessage DTO shape"},
	{SurfaceID: "apex:System.Messaging.SendEmailResult", Kind: KindType, Behavior: BehaviorPassive, Notes: "fixture-backed System-qualified alias for the local SendEmailResult DTO shape"},
	{SurfaceID: "apex:System.Messaging.sendPushNotification", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostic for messaging transport APIs"},
	{SurfaceID: "apex:System.PatternSyntaxException", Kind: KindType, Behavior: BehaviorPassive, Notes: "fixture-backed System-qualified alias for local PatternSyntaxException values"},
	{SurfaceID: "apex:System.PageReference(record)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs shorthand for the PageReference ApexPage record constructor"},
	{SurfaceID: "apex:Process.PluginDescribeResult.InputParameter.PluginDescribeResult.InputParameter(String,Process.PluginDescribeResult.ParameterType,Boolean)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs constructor spelling for Process.PluginDescribeResult.InputParameter local DTO shape"},
	{SurfaceID: "apex:Process.PluginDescribeResult.InputParameter.PluginDescribeResult.InputParameter(String,String,Process.PluginDescribeResult.ParameterType,Boolean)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs constructor spelling for Process.PluginDescribeResult.InputParameter local DTO shape"},
	{SurfaceID: "apex:Process.PluginDescribeResult.OutputParameter.PluginDescribeResult.OutputParameter(String,Process.PluginDescribeResult.ParameterType)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs constructor spelling for Process.PluginDescribeResult.OutputParameter local DTO shape"},
	{SurfaceID: "apex:Process.PluginDescribeResult.OutputParameter.PluginDescribeResult.OutputParameter(String,String,Process.PluginDescribeResult.ParameterType)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs constructor spelling for Process.PluginDescribeResult.OutputParameter local DTO shape"},
	{SurfaceID: "apex:System.QuickAction.*", Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "fixture-backed explicit unsupported diagnostics for local quick action UI surfaces"},
	{SurfaceID: "apex:System.RestRequest.getHeader(String)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for RestRequest.getHeader(String)"},
	{SurfaceID: "apex:System.RestRequest.getParameter(String)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for RestRequest.getParameter(String)"},
	{SurfaceID: "apex:Schema.Schema.describeSObjects(List<String>)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed qualified alias for Schema.describeSObjects(List<String>)"},
	{SurfaceID: "apex:Schema.Schema.getGlobalDescribe()", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed qualified alias for Schema.getGlobalDescribe()"},
	{SurfaceID: "apex:System.Search.find", Kind: KindMethod, Behavior: BehaviorPartial, Notes: "fixture-backed stdlib shorthand for deterministic local Search.find(String) behavior"},
	{SurfaceID: "apex:System.Search.find(String,Object)", Kind: KindMethod, Behavior: BehaviorPartial, Notes: "fixture-backed Object overload for deterministic local Search.find AccessLevel handling"},
	{SurfaceID: "apex:System.Search.query(String,Object)", Kind: KindMethod, Behavior: BehaviorPartial, Notes: "fixture-backed Object overload for deterministic local Search.query AccessLevel handling"},
	{SurfaceID: "apex:System.Search.suggest(String,String,Object)", Kind: KindMethod, Behavior: BehaviorPartial, Notes: "fixture-backed Object overload for deterministic local Search.suggest option handling"},
	{SurfaceID: "apex:System.Search.suggest(String,String,Object,Object)", Kind: KindMethod, Behavior: BehaviorPartial, Notes: "fixture-backed Object overload for deterministic local Search.suggest option and AccessLevel handling"},
	{SurfaceID: "apex:System.Time.valueOf(String)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for Time.valueOf(String)"},
	{SurfaceID: "apex:System.TxnSecurity.EventCondition.evaluate(SObject)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local transaction-security event-condition default evaluation"},
	{SurfaceID: "apex:System.TxnSecurity.PolicyCondition.evaluate(TxnSecurity.Event)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed System-qualified alias for local transaction-security policy-condition default evaluation"},
	{SurfaceID: "apex:System.Type.forName(namespace,name)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs shorthand for Type.forName namespace/name lookup"},
}

func addFixtureBackedSystemAliasRows(byID map[string]SurfaceLedgerRow) {
	for _, alias := range fixtureBackedSystemAliasRows {
		row := SurfaceLedgerRow{
			SurfaceID:     alias.SurfaceID,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          alias.Kind,
			GladeBehavior: alias.Behavior,
			Sources:       []string{"system-fixture-alias"},
			Notes:         alias.Notes,
		}
		fillFromApexID(&row)
		byID[surfaceIDKey(row.SurfaceID)] = RowFromGladeShape(row)
	}
}

var fixtureBackedApexAliasRows = []fixtureBackedSystemAliasRow{
	{SurfaceID: "apex:Database.DMLOptions", Kind: KindType, Behavior: BehaviorPassive, Notes: "fixture-backed exact docs type id for Database.DMLOptions local option shape"},
	{SurfaceID: "apex:Database.DMLOptions.DMLOptions()", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs constructor id for Database.DMLOptions local option storage"},
	{SurfaceID: "apex:Database.DMLOptions.allowFieldTruncation", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.allowFieldTruncation local storage"},
	{SurfaceID: "apex:Database.DMLOptions.assignmentRuleHeader", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.assignmentRuleHeader local storage"},
	{SurfaceID: "apex:Database.DMLOptions.DuplicateRuleHeader", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.DuplicateRuleHeader local storage"},
	{SurfaceID: "apex:Database.DMLOptions.DuplicateRuleHeader.allowSave", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.DuplicateRuleHeader.allowSave local storage"},
	{SurfaceID: "apex:Database.DMLOptions.DuplicateRuleHeader.runAsCurrentUser", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.DuplicateRuleHeader.runAsCurrentUser local storage"},
	{SurfaceID: "apex:Database.DMLOptions.emailHeader", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.emailHeader local storage"},
	{SurfaceID: "apex:Database.DMLOptions.localeOptions", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.localeOptions local storage"},
	{SurfaceID: "apex:Database.DMLOptions.LocalizeErrors", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.LocalizeErrors local storage"},
	{SurfaceID: "apex:Database.DMLOptions.optAllOrNone", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DMLOptions.optAllOrNone local storage"},
	{SurfaceID: "apex:Database.DmlOptions.AssignmentRuleHeader.assignmentRuleID", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DmlOptions.AssignmentRuleHeader.assignmentRuleID local storage"},
	{SurfaceID: "apex:Database.DmlOptions.AssignmentRuleHeader.useDefaultRule", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DmlOptions.AssignmentRuleHeader.useDefaultRule local storage"},
	{SurfaceID: "apex:Database.DmlOptions.EmailHeader.triggerAutoResponseEmail", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DmlOptions.EmailHeader.triggerAutoResponseEmail local storage"},
	{SurfaceID: "apex:Database.DmlOptions.EmailHeader.triggerOtherEmail", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DmlOptions.EmailHeader.triggerOtherEmail local storage"},
	{SurfaceID: "apex:Database.DmlOptions.EmailHeader.triggerUserEmail", Kind: KindProperty, Behavior: BehaviorSupported, Notes: "fixture-backed exact docs property id for Database.DmlOptions.EmailHeader.triggerUserEmail local storage"},
	{SurfaceID: "apex:Messaging.SingleEmailMessage.setFileAttachments(List<EmailFileAttachment>)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed docs shorthand for SingleEmailMessage.setFileAttachments(List<Messaging.EmailFileAttachment>)"},
	{SurfaceID: "apex:Support.EmailTemplateSelector.getDefaultTemplateId(Id)", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed selector alias returning the nullable local default email template Id result"},
	{SurfaceID: "apex:TxnSecurity.Event.Event()", Kind: KindMethod, Behavior: BehaviorSupported, Notes: "fixture-backed exact TxnSecurity docs constructor for the local passive transaction-security event record"},
}

func addFixtureBackedApexAliasRows(byID map[string]SurfaceLedgerRow) {
	for _, alias := range fixtureBackedApexAliasRows {
		row := SurfaceLedgerRow{
			SurfaceID:     alias.SurfaceID,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          alias.Kind,
			GladeBehavior: alias.Behavior,
			Sources:       []string{"apex-fixture-alias"},
			Notes:         alias.Notes,
		}
		fillFromApexID(&row)
		byID[surfaceIDKey(row.SurfaceID)] = RowFromGladeShape(row)
	}
}

type fixtureBackedApexMirrorAliasRow struct {
	SurfaceID string
	SourceID  string
	Kind      string
}

var fixtureBackedApexMirrorAliasRows = []fixtureBackedApexMirrorAliasRow{
	{SurfaceID: "apex:commercepayments.PostAuthApiPaymentMethodRequest_alternativePaymentMethod", SourceID: "apex:commercepayments.PostAuthApiPaymentMethodRequest.alternativePaymentMethod", Kind: KindType},
	{SurfaceID: "apex:commercepayments.PostAuthApiPaymentMethodRequest_cardPaymentMethod", SourceID: "apex:commercepayments.PostAuthApiPaymentMethodRequest.cardPaymentMethod", Kind: KindType},
	{SurfaceID: "apex:commercepayments.PostAuthorizationRequest_accountId", SourceID: "apex:commercepayments.PostAuthorizationRequest.accountId", Kind: KindType},
	{SurfaceID: "apex:commercepayments.PostAuthorizationRequest_amount", SourceID: "apex:commercepayments.PostAuthorizationRequest.amount", Kind: KindType},
	{SurfaceID: "apex:commercepayments.PostAuthorizationRequest_comments", SourceID: "apex:commercepayments.PostAuthorizationRequest.comments", Kind: KindType},
	{SurfaceID: "apex:commercepayments.PostAuthorizationRequest_currencyIsoCode", SourceID: "apex:commercepayments.PostAuthorizationRequest.currencyIsoCode", Kind: KindType},
	{SurfaceID: "apex:commercepayments.PostAuthorizationRequest_paymentMethod", SourceID: "apex:commercepayments.PostAuthorizationRequest.paymentMethod", Kind: KindType},
	{SurfaceID: "apex:industriesNlpSvc.NlpResponse_errors", SourceID: "apex:industriesNlpSvc.NlpResponse.errors", Kind: KindType},
	{SurfaceID: "apex:industriesNlpSvc.NlpResponse_summarizationResult", SourceID: "apex:industriesNlpSvc.NlpResponse.summarizationResult", Kind: KindType},
	{SurfaceID: "apex:industriesNlpSvc.NlpSummarizationResult_summary", SourceID: "apex:industriesNlpSvc.NlpSummarizationResult.summary", Kind: KindType},
	{SurfaceID: "apex:ise.DynamicMenuItem", SourceID: "apex:ise_bots_apex.DynamicMenuItem"},
	{SurfaceID: "apex:ise.DynamicMenuItem.EntityId", SourceID: "apex:ise_bots_apex.DynamicMenuItem.EntityId"},
	{SurfaceID: "apex:ise.DynamicMenuItem.EntityIdValue", SourceID: "apex:ise_bots_apex.DynamicMenuItem.EntityIdValue"},
	{SurfaceID: "apex:ise.DynamicMenuItem.EntityName", SourceID: "apex:ise_bots_apex.DynamicMenuItem.EntityName"},
	{SurfaceID: "apex:ise.DynamicMenuItem.EntityNameValue", SourceID: "apex:ise_bots_apex.DynamicMenuItem.EntityNameValue"},
	{SurfaceID: "apex:ise.DynamicMenuItem.Label", SourceID: "apex:ise_bots_apex.DynamicMenuItem.Label"},
	{SurfaceID: "apex:ise.DynamicMenuItem.LabelValue", SourceID: "apex:ise_bots_apex.DynamicMenuItem.LabelValue"},
	{SurfaceID: "apex:ise.DynamicMenuItem.sortByDate", SourceID: "apex:ise_bots_apex.DynamicMenuItem.sortByDate"},
	{SurfaceID: "apex:ise.DynamicMenuItem.sortByDateValue", SourceID: "apex:ise_bots_apex.DynamicMenuItem.sortByDateValue"},
	{SurfaceID: "apex:ise.DynamicMenuItem.SummaryTextWithFormula", SourceID: "apex:ise_bots_apex.DynamicMenuItem.SummaryTextWithFormula"},
	{SurfaceID: "apex:ise.DynamicMenuItem.SummaryTextWithFormulaValue", SourceID: "apex:ise_bots_apex.DynamicMenuItem.SummaryTextWithFormulaValue"},
	{SurfaceID: "apex:pref.LoadFormData", SourceID: "apex:pref_center.LoadFormData"},
	{SurfaceID: "apex:pref.LoadFormData.addOption(String,SelectOption)", SourceID: "apex:pref_center.LoadFormData.addOption(String,SelectOption)"},
	{SurfaceID: "apex:pref.LoadFormData.addOption(String,String,String)", SourceID: "apex:pref_center.LoadFormData.addOption(String,String,String)"},
	{SurfaceID: "apex:pref.LoadFormData.addSelectedOption(String,String)", SourceID: "apex:pref_center.LoadFormData.addSelectedOption(String,String)"},
	{SurfaceID: "apex:pref.LoadFormData.LoadFormData(Map<String,pref_center.FieldProperties>)", SourceID: "apex:pref_center.LoadFormData.LoadFormData(Map<String,pref_center.FieldProperties>)"},
	{SurfaceID: "apex:pref.LoadFormData.setButtonLabel(String,String)", SourceID: "apex:pref_center.LoadFormData.setButtonLabel(String,String)"},
	{SurfaceID: "apex:pref.LoadFormData.setOptions(String,List<SelectOption>)", SourceID: "apex:pref_center.LoadFormData.setOptions(String,List<SelectOption>)"},
	{SurfaceID: "apex:pref.LoadFormData.setSelectedOption(String,String)", SourceID: "apex:pref_center.LoadFormData.setSelectedOption(String,String)"},
	{SurfaceID: "apex:pref.LoadFormData.setSelectedOptions(String,List<String>)", SourceID: "apex:pref_center.LoadFormData.setSelectedOptions(String,List<String>)"},
	{SurfaceID: "apex:pref.LoadFormData.setTextHint(String,String)", SourceID: "apex:pref_center.LoadFormData.setTextHint(String,String)"},
	{SurfaceID: "apex:pref.LoadFormData.setTextValue(String,String)", SourceID: "apex:pref_center.LoadFormData.setTextValue(String,String)"},
	{SurfaceID: "apex:pref.LoadParameters", SourceID: "apex:pref_center.LoadParameters"},
	{SurfaceID: "apex:pref.LoadParameters.getRecordId()", SourceID: "apex:pref_center.LoadParameters.getRecordId()"},
	{SurfaceID: "apex:pref.PreferenceCenterApexHandler", SourceID: "apex:pref_center.PreferenceCenterApexHandler"},
	{SurfaceID: "apex:pref.PreferenceCenterApexHandler.load(pref_center.LoadParameters,pref_center.LoadFormData,pref_center.ValidationResult)", SourceID: "apex:pref_center.PreferenceCenterApexHandler.load(pref_center.LoadParameters,pref_center.LoadFormData,pref_center.ValidationResult)"},
	{SurfaceID: "apex:pref.PreferenceCenterApexHandler.submit(pref_center.SubmitParameters,pref_center.SubmitFormData,pref_center.ValidationResult)", SourceID: "apex:pref_center.PreferenceCenterApexHandler.submit(pref_center.SubmitParameters,pref_center.SubmitFormData,pref_center.ValidationResult)"},
	{SurfaceID: "apex:pref.SubmitFormData", SourceID: "apex:pref_center.SubmitFormData"},
	{SurfaceID: "apex:pref.SubmitFormData.getButtonClicked()", SourceID: "apex:pref_center.SubmitFormData.getButtonClicked()"},
	{SurfaceID: "apex:pref.SubmitFormData.getOldSelectedValue(String)", SourceID: "apex:pref_center.SubmitFormData.getOldSelectedValue(String)"},
	{SurfaceID: "apex:pref.SubmitFormData.getOldSelectedValues(String)", SourceID: "apex:pref_center.SubmitFormData.getOldSelectedValues(String)"},
	{SurfaceID: "apex:pref.SubmitFormData.getOldStringValue(String)", SourceID: "apex:pref_center.SubmitFormData.getOldStringValue(String)"},
	{SurfaceID: "apex:pref.SubmitFormData.getSelectedValue(String)", SourceID: "apex:pref_center.SubmitFormData.getSelectedValue(String)"},
	{SurfaceID: "apex:pref.SubmitFormData.getSelectedValues(String)", SourceID: "apex:pref_center.SubmitFormData.getSelectedValues(String)"},
	{SurfaceID: "apex:pref.SubmitFormData.getStringValue(String)", SourceID: "apex:pref_center.SubmitFormData.getStringValue(String)"},
	{SurfaceID: "apex:pref.SubmitParameters", SourceID: "apex:pref_center.SubmitParameters"},
	{SurfaceID: "apex:pref.SubmitParameters.getRecordId()", SourceID: "apex:pref_center.SubmitParameters.getRecordId()"},
	{SurfaceID: "apex:pref.TokenType", SourceID: "apex:pref_center.TokenType"},
	{SurfaceID: "apex:pref.TokenUtility", SourceID: "apex:pref_center.TokenUtility"},
	{SurfaceID: "apex:pref.TokenUtility.generateToken(String,pref_center.TokenType)", SourceID: "apex:pref_center.TokenUtility.generateToken(String,pref_center.TokenType)"},
	{SurfaceID: "apex:pref.TokenUtility.generateToken(String)", SourceID: "apex:pref_center.TokenUtility.generateToken(String)"},
	{SurfaceID: "apex:pref.TokenUtility.generateTokens(List<String>,pref_center.TokenType)", SourceID: "apex:pref_center.TokenUtility.generateTokens(List<String>,pref_center.TokenType)"},
	{SurfaceID: "apex:pref.TokenUtility.generateTokens(List<String>)", SourceID: "apex:pref_center.TokenUtility.generateTokens(List<String>)"},
	{SurfaceID: "apex:pref.ValidationResult", SourceID: "apex:pref_center.ValidationResult"},
	{SurfaceID: "apex:RichMessaging.ProcessFormHandler.processFormRequest", SourceID: "apex:RichMessaging.ProcessFormHandler.processFormRequest(RichMessaging.ProcessFormResponse)"},
	{SurfaceID: "apex:setup.FlowPerformanceSetupDetails", SourceID: "apex:setup_flow_performance.FlowPerformanceSetupDetails"},
	{SurfaceID: "apex:sfdc.Example", SourceID: "apex:ConnectApi.Example"},
	{SurfaceID: "apex:sfdc.LearningEvaluation", SourceID: "apex:sfdc_enablement.LearningEvaluation"},
	{SurfaceID: "apex:sfdc.LearningEvaluation.getDetails()", SourceID: "apex:sfdc_enablement.LearningEvaluation.getDetails()"},
	{SurfaceID: "apex:sfdc.LearningEvaluation.getLearningItemId()", SourceID: "apex:sfdc_enablement.LearningEvaluation.getLearningItemId()"},
	{SurfaceID: "apex:sfdc.LearningEvaluation.setDetails(Map<String,Object>)", SourceID: "apex:sfdc_enablement.LearningEvaluation.setDetails(Map<String,Object>)"},
	{SurfaceID: "apex:sfdc.LearningEvaluation.setLearningItemId(String)", SourceID: "apex:sfdc_enablement.LearningEvaluation.setLearningItemId(String)"},
	{SurfaceID: "apex:sfdc.LearningEvaluationResult", SourceID: "apex:sfdc_enablement.LearningEvaluationResult"},
	{SurfaceID: "apex:sfdc.LearningEvaluationResult.getLearningItemProgress()", SourceID: "apex:sfdc_enablement.LearningEvaluationResult.getLearningItemProgress()"},
	{SurfaceID: "apex:sfdc.LearningEvaluationResult.getLearningItemProgressStatus()", SourceID: "apex:sfdc_enablement.LearningEvaluationResult.getLearningItemProgressStatus()"},
	{SurfaceID: "apex:sfdc.LearningEvaluationResult.setLearningItemProgress(Double)", SourceID: "apex:sfdc_enablement.LearningEvaluationResult.setLearningItemProgress(Double)"},
	{SurfaceID: "apex:sfdc.LearningEvaluationResult.setLearningItemProgressStatus(sfdc_enablement.LearningItemProgressStatus)", SourceID: "apex:sfdc_enablement.LearningEvaluationResult.setLearningItemProgressStatus(sfdc_enablement.LearningItemProgressStatus)"},
	{SurfaceID: "apex:sfdc.LearningItemEvaluationHandler", SourceID: "apex:sfdc_enablement.LearningItemEvaluationHandler"},
	{SurfaceID: "apex:sfdc.LearningItemEvaluationHandler.evaluate(Sfdc_enablement.LearningEvaluation)", SourceID: "apex:sfdc_enablement.LearningItemEvaluationHandler.evaluate(sfdc_enablement.LearningEvaluation)"},
	{SurfaceID: "apex:sfdc.LearningItemProgressStatus", SourceID: "apex:sfdc_enablement.LearningItemProgressStatus"},
	{SurfaceID: "apex:sfdc.LearningItemSerializeDeserializer", SourceID: "apex:sfdc_enablement.LearningItemSerializeDeserializer"},
	{SurfaceID: "apex:sfdc.LearningItemSerializeDeserializer.deserialize(String)", SourceID: "apex:sfdc_enablement.LearningItemSerializeDeserializer.deserialize(String)"},
	{SurfaceID: "apex:sfdc.LearningItemSerializeDeserializer.serialize(String)", SourceID: "apex:sfdc_enablement.LearningItemSerializeDeserializer.serialize(String)"},
	{SurfaceID: "apex:sfdc.SurveyInvitationLinkShortener", SourceID: "apex:sfdc_surveys.SurveyInvitationLinkShortener"},
	{SurfaceID: "apex:sfdc.SurveyInvitationLinkShortener.getShortenedURL(String)", SourceID: "apex:sfdc_surveys.SurveyInvitationLinkShortener.getShortenedURL(String)"},
	{SurfaceID: "apex:System.Database.convertLead(leadsToConvert,accessLevel)", SourceID: "apex:System.Database.convertLead(List<Database.LeadConvert>,AccessLevel)"},
	{SurfaceID: "apex:System.Database.convertLead(leadToConvert,accessLevel)", SourceID: "apex:System.Database.convertLead(Database.LeadConvert,AccessLevel)"},
	{SurfaceID: "apex:System.String.template(valueMap)", SourceID: "apex:System.String.template(Map<String,Object>)"},
	{SurfaceID: "apex:System.System.attachFinalizer(finalizer)", SourceID: "apex:System.System.attachFinalizer(Object)"},
}

func addFixtureBackedApexMirrorAliasRows(byID map[string]SurfaceLedgerRow) {
	for _, alias := range fixtureBackedApexMirrorAliasRows {
		source, ok := byID[surfaceIDKey(alias.SourceID)]
		if !ok {
			continue
		}
		row := source
		row.SurfaceID = alias.SurfaceID
		row.Namespace = ""
		row.TypeName = ""
		row.MemberName = ""
		row.Parameters = nil
		row.GladeParameters = nil
		row.Sources = mergeStrings(row.Sources, []string{"apex-mirror-alias"})
		row.Notes = "fixture-backed docs alias for " + alias.SourceID
		if alias.Kind != "" {
			row.Kind = alias.Kind
		}
		fillFromApexID(&row)
		byID[surfaceIDKey(row.SurfaceID)] = RowFromGladeShape(row)
	}
}

func addFixtureBackedInvocableActionDTORows(byID map[string]SurfaceLedgerRow) {
	rows := []struct {
		typeName string
		methods  []string
	}{
		{typeName: "AdditionalAttribute", methods: []string{
			"getApexClass()",
			"getDataType()",
			"getIsCollection()",
			"getName()",
			"getValue()",
			"getValueAsBooleanList()",
			"getValueAsDateList()",
			"getValueAsDoubleList()",
			"getValueAsIntegerList()",
			"getValueAsList()",
			"getValueAsLongList()",
			"getValueAsStringList()",
		}},
		{typeName: "Error", methods: []string{
			"clone()",
			"getCode()",
			"getMessage()",
		}},
		{typeName: "GenericType", methods: []string{
			"getDescription()",
			"getLabel()",
			"getName()",
			"getSuperType()",
		}},
		{typeName: "OutputParameter", methods: []string{
			"getAdditionalAttributes()",
			"getApexClass()",
			"getDescription()",
			"getLabel()",
			"getMaxOccurs()",
			"getName()",
			"getPicklistValues()",
			"getSObjectType()",
			"getType()",
		}},
		{typeName: "PicklistValue", methods: []string{
			"getActive()",
			"getDefaultValue()",
			"getLabel()",
			"getValidFor()",
			"getValue()",
		}},
	}
	for _, group := range rows {
		for _, method := range group.methods {
			row := SurfaceLedgerRow{
				SurfaceID:     "apex:Invocable.Action." + group.typeName + "." + method,
				Product:       ProductApex,
				Area:          AreaRuntime,
				Kind:          KindMethod,
				GladeBehavior: BehaviorPassive,
				Sources:       []string{"apex-fixture-alias"},
				Notes:         "fixture-backed Invocable.Action passive DTO local default accessor",
			}
			fillFromApexID(&row)
			byID[surfaceIDKey(row.SurfaceID)] = RowFromGladeShape(row)
		}
	}
}

func mergeGladeBehavior(existing, next BehaviorState) BehaviorState {
	if existing == BehaviorUnsupported {
		return existing
	}
	if existing == BehaviorSupported && (next == BehaviorPassive || next == BehaviorStubNoOp || next == BehaviorUnsupported) {
		return existing
	}
	if existing == BehaviorPartial && (next == BehaviorStubNoOp || next == BehaviorUnsupported) {
		return existing
	}
	if next == "" || next == BehaviorNone {
		return existing
	}
	return next
}

func behaviorForStandardType(namespace, typeName string) BehaviorState {
	if strings.EqualFold(namespace, "Database") && strings.EqualFold(typeName, "Stateful") {
		return BehaviorSupported
	}
	return BehaviorNone
}

func addDataReferenceGladeRows(byID map[string]SurfaceLedgerRow) {
	for _, objectName := range storage.KnownStandardObjectNames() {
		definition, ok := storage.StandardObjectDefinition(objectName)
		if !ok {
			continue
		}
		objectID := DataObjectID(definition.APIName)
		byID[surfaceIDKey(objectID)] = RowFromGeneratedDataReferenceShape(SurfaceLedgerRow{
			SurfaceID:     objectID,
			Product:       ProductDataRef,
			Area:          AreaData,
			TypeName:      definition.APIName,
			Kind:          KindType,
			GladeBehavior: BehaviorSupported,
			Sources:       []string{SourceStandardSObjectGeneratedShape},
		})
		for _, field := range definition.Fields {
			fieldID := DataFieldID(definition.APIName, field.APIName)
			byID[surfaceIDKey(fieldID)] = RowFromGeneratedDataReferenceShape(SurfaceLedgerRow{
				SurfaceID:     fieldID,
				Product:       ProductDataRef,
				Area:          AreaData,
				TypeName:      definition.APIName,
				FieldName:     field.APIName,
				Kind:          KindField,
				ReturnType:    string(field.Type),
				GladeBehavior: BehaviorSupported,
				Sources:       []string{SourceStandardSObjectGeneratedShape},
			})
		}
	}
}

func addLocalTestLWCGladeRows(byID map[string]SurfaceLedgerRow) {
	for _, module := range localTestUnsupportedLWCModules {
		id := LWCModuleID(module)
		byID[surfaceIDKey(id)] = RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductLWC,
			Area:          AreaUI,
			TypeName:      module,
			Kind:          KindModule,
			GladeBehavior: BehaviorUnsupported,
			Sources:       []string{"uicontroller-import-shape"},
			Notes:         "local Apex tests can index this LWC import shape; browser or service execution is not modeled locally",
		})
	}
}

func addLocalTestVisualforceComponentRows(byID map[string]SurfaceLedgerRow) {
	for _, name := range visualforce.StandardComponentReferenceNames() {
		id := visualforceComponentReferenceID(name)
		byID[surfaceIDKey(id)] = RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductVisualforce,
			Area:          AreaUI,
			TypeName:      strings.TrimPrefix(id, "visualforce:"),
			Kind:          KindGuide,
			GladeShape:    ShapeGenerated,
			GladeBehavior: BehaviorPassive,
			Sources:       []string{"visualforce-component-doc-shape"},
			Notes:         "local Visualforce indexing accepts this component reference as metadata shape; browser rendering is not modeled locally",
		})
	}
}

func visualforceComponentReferenceID(name string) string {
	if name == "" {
		return "visualforce:pages_compref"
	}
	return "visualforce:pages_compref_" + name
}

func addLocalTestAuraMetadataRows(byID map[string]SurfaceLedgerRow) {
	for _, id := range localTestAuraMetadataSurfaceIDs {
		byID[surfaceIDKey(id)] = RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductAura,
			Area:          AreaUI,
			TypeName:      strings.TrimPrefix(id, "unknown:"),
			Kind:          KindGuide,
			GladeShape:    ShapeGenerated,
			GladeBehavior: BehaviorPassive,
			Sources:       []string{"aura-metadata-doc-shape"},
			Notes:         "local UI controller discovery can index this Aura metadata shape; browser rendering and Lightning services are not modeled locally",
		})
	}
}

type unsupportedQueryRuntimeRow struct {
	ID   string
	Note string
}

var unsupportedQueryRuntimeRows = []unsupportedQueryRuntimeRow{
	{ID: "unknown:MockSOQLTestsForDMOs", Note: "Data Cloud DMO mock SOQL tests require Data Cloud services and are not executed by the local Apex runtime."},
	{ID: "unknown:apex_connector_external_objects_mock_soql_tests", Note: "Salesforce Connect external-object mock SOQL tests require connector services outside the local Apex runtime."},
	{ID: "unknown:salesforce_app_limits_platform_soslsoql", Note: "SOQL and SOSL platform limit tables document remote service limits, not local Apex query execution behavior."},
	{ID: "unknown:sforce_api_calls_describesoqllistview", Note: "SOAP describeSObject list-view metadata calls are API metadata surfaces, not local Apex SOQL execution."},
	{ID: "unknown:sforce_api_calls_describesoqllistview_soqlwherecondition", Note: "List-view SOQL where-condition metadata is returned by describe API calls, not executed as local Apex SOQL."},
	{ID: "unknown:sforce_api_calls_describesoqllistviewparams", Note: "DescribeSObject list-view request parameters are SOAP API metadata, not local Apex runtime behavior."},
	{ID: "unknown:sforce_api_calls_describesoqllistviewresult", Note: "DescribeSObject list-view result DTOs are SOAP API metadata, not local Apex runtime behavior."},
	{ID: "unknown:sforce_api_calls_describesoqllistviews", Note: "DescribeSObject list-view collection metadata is a SOAP API surface outside local Apex execution."},
	{ID: "unknown:sforce_api_calls_describesoqllistviewsrequest", Note: "DescribeSoqlListViewsRequest is SOAP API metadata input, not local Apex SOQL execution."},
	{ID: "unknown:sforce_api_calls_soql_changing_batch_size", Note: "API query batch-size negotiation is a remote API cursor setting and has no local Apex test-runner equivalent."},
	{ID: "unknown:sforce_api_calls_soql_feeds_url_syntax", Note: "Syndication feed SOQL mapping is public-site feed configuration outside local Apex query execution."},
	{ID: "unknown:sforce_api_calls_soql_relationships_query_datacat", Note: "DataCategorySelection article relationships depend on Knowledge data category services outside the local runtime."},
	{ID: "unknown:sforce_api_calls_soql_relationships_query_hist", Note: "History relationship queries depend on Salesforce field-history tracking services outside the local runtime."},
	{ID: "unknown:sforce_api_calls_soql_select_set_options", Note: "SOQL SET OPTIONS targets Data Cloud DLO and DMO service behavior outside local Apex query execution."},
	{ID: "unknown:sforce_api_calls_soql_select_with_datacategory", Note: "SOQL WITH DATA CATEGORY filters Knowledge and Question visibility services outside the local runtime."},
	{ID: "unknown:sforce_api_calls_soql_select_with_datacategory_catselection", Note: "SOQL data category selection syntax depends on Knowledge category services outside the local runtime."},
	{ID: "unknown:sforce_api_calls_soql_select_with_recordvisibilitycontext", Note: "RecordVisibilityContext uses Salesforce visibility service descriptors outside local Apex query execution."},
	{ID: "unknown:sforce_api_calls_soql_typos", Note: "SOQL typographical conventions are documentation syntax guidance, not a runtime surface."},
	{ID: "unknown:sforce_api_calls_sosl_limits_external_objects", Note: "SOSL external-object limits depend on Salesforce Connect services outside the local runtime."},
	{ID: "unknown:sforce_api_calls_sosl_typos", Note: "SOSL typographical conventions are documentation syntax guidance, not a runtime surface."},
	{ID: "unknown:sforce_api_calls_sosl_update_tracking", Note: "SOSL UPDATE TRACKING records Salesforce Knowledge search analytics outside the local runtime."},
	{ID: "unknown:sforce_api_calls_sosl_update_viewstat", Note: "SOSL UPDATE VIEWSTAT records Salesforce Knowledge article view statistics outside the local runtime."},
	{ID: "unknown:sforce_api_calls_sosl_using_listview", Note: "SOSL USING ListView depends on Salesforce list-view metadata and service filtering outside the local runtime."},
	{ID: "unknown:sforce_api_calls_sosl_with", Note: "SOSL WITH DivisionFilter depends on Salesforce division filtering services outside the local runtime."},
	{ID: "unknown:sforce_api_calls_sosl_with_data_category", Note: "SOSL WITH DATA CATEGORY filters Knowledge and Question category visibility outside the local runtime."},
	{ID: "unknown:sforce_api_calls_sosl_with_metadata", Note: "SOSL WITH METADATA returns service response labels outside local Apex search execution."},
	{ID: "unknown:supported_soql", Note: "Supported PushTopic query rules are Streaming API behavior outside local Apex query execution."},
	{ID: "unknown:unsupported_soql_statements", Note: "Unsupported PushTopic query rules are Streaming API behavior outside local Apex query execution."},
}

func addUnsupportedQueryRuntimeGladeRows(byID map[string]SurfaceLedgerRow) {
	for _, item := range unsupportedQueryRuntimeRows {
		byID[surfaceIDKey(item.ID)] = RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     item.ID,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          KindMethod,
			GladeBehavior: BehaviorUnsupported,
			Sources:       []string{"query-runtime-explicit-unsupported"},
			Notes:         item.Note,
		})
	}
}

type surfaceClosureTailRow struct {
	ID       string
	Kind     string
	Behavior BehaviorState
	Notes    string
}

var surfaceClosureTailRows = []surfaceClosureTailRow{
	{ID: ApexMemberID("ConnectApi", "CommerceSearchConnectFamily", "searchProducts", []string{"String", "String", "List<String>", "String", "String", "String", "List<String>", "String", "Integer", "Integer", "String", "List<String>", "Boolean", "Boolean"}), Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "ConnectApi commerce search executes hosted Commerce search services outside the local runtime."},
	{ID: ApexMemberID("ConnectApi", "OptimizationFiles", "FetchOptimizationFiles", []string{"ConnectApi.fetchFilesInput"}), Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "ConnectApi optimization files fetches hosted optimization resources outside the local runtime."},
	{ID: ApexMemberID("Schema", "DescribeSObjectResult", "getAssociateEntityType", nil), Kind: KindMethod, Behavior: BehaviorPassive, Notes: "Schema associate entity type is represented as a passive describe shape in local metadata."},
	{ID: ApexMemberID("System", "List", "List", []string{"Set<T>"}), Kind: KindMethod, Behavior: BehaviorPassive, Notes: "Generic List Set-copy constructor is a passive collection shape."},
	{ID: ApexMemberID("System", "Set", "Set", []string{"Object"}), Kind: KindMethod, Behavior: BehaviorPassive, Notes: "Generic Set Object constructor is a passive collection shape."},
	{ID: ApexMemberID("System", "Site", "getCurrentSiteUrl", nil), Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "Removed by Salesforce after API version 29.0; retain as a negative compiler contract."},
	{ID: ApexMemberID("System", "Site", "getCustomWebAddress", nil), Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "Removed by Salesforce after API version 29.0; retain as a negative compiler contract."},
	{ID: ApexMemberID("System", "Site", "getPrefix", nil), Kind: KindMethod, Behavior: BehaviorUnsupported, Notes: "Removed by Salesforce after API version 29.0; retain as a negative compiler contract."},
}

func addSurfaceClosureTailGladeRows(byID map[string]SurfaceLedgerRow) {
	for _, item := range surfaceClosureTailRows {
		row := RowFromGladeShape(SurfaceLedgerRow{
			SurfaceID:     item.ID,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          item.Kind,
			GladeBehavior: item.Behavior,
			Sources:       []string{"surface-closure-tail-shape"},
			Notes:         item.Notes,
		})
		fillFromApexID(&row)
		byID[surfaceIDKey(item.ID)] = row
	}
}

var localTestUnsupportedLWCModules = []string{
	"Decorators",
	"HTML",
	"LWC",
	"PageReference",
	"Salesforce",
	"Standard",
	"XML",
	"`@salesforce`",
	"`experience/blockBuilderApi`",
	"`experience/cms*Api`",
	"`experience/cmsEditorApi`",
	"`lightning/analyticsWaveApi`",
	"`lightning/graphql`",
	"`lightning/industriesEducationPublicApi`",
	"`lightning/mobileCapabilities`",
	"`lightning/serviceKnowledgeApi`",
	"`lightning/ui*Api`",
	"`lightning/uiGraphQLApi`",
	"`notifyRecordUpdateAvailable(recordIds)`",
	"lightning/cmsDeliveryApi",
}

var localTestAuraMetadataSurfaceIDs = strings.Fields(`
unknown:apex_classes_annotation_AuraEnabled
unknown:code_sample_lightning_cmp
unknown:meta_auradefinitionbundle
unknown:meta_lightningbolt
unknown:meta_lightningcomponentbundle
unknown:meta_lightningexperiencesettings
unknown:meta_lightningexperiencetheme
unknown:meta_lightningmessagechannel
unknown:meta_lightningonboardingconfig
unknown:meta_lightningtypebundle
unknown:ref_attr_types_aura
unknown:ref_attr_types_aura_action
unknown:ref_aura_application
unknown:ref_aura_attribute
unknown:ref_aura_event
unknown:ref_aura_interface
`)

// addMethodFamilyShapeReconciliation promotes absent, signatureless Apex
// method-family rows to type-known when a different exact sibling overload
// with the same namespace, type, member, and kind is already shaped by Glade.
func addMethodFamilyShapeReconciliation(byID map[string]SurfaceLedgerRow) {
	familyKeys := make(map[string]bool)
	for _, row := range byID {
		if row.Product != ProductApex {
			continue
		}
		if row.GladeShape == ShapeAbsent || row.GladeShape == "" {
			continue
		}
		// Shaped siblings must have an explicit parameter list in their
		// surfaceId — detectable by '(' in the surface ID.
		if !strings.Contains(row.SurfaceID, "(") {
			continue
		}
		if row.MemberName == "" {
			continue
		}
		key := methodFamilyReconciliationKey(row.Namespace, row.TypeName, row.MemberName, row.Kind)
		familyKeys[key] = true
	}
	for key, row := range byID {
		if row.Product != ProductApex {
			continue
		}
		if row.GladeShape != ShapeAbsent {
			continue
		}
		// Only promote signatureless rows — surfaceId contains no '('.
		if strings.Contains(row.SurfaceID, "(") {
			continue
		}
		if row.MemberName == "" {
			continue
		}
		familyKey := methodFamilyReconciliationKey(row.Namespace, row.TypeName, row.MemberName, row.Kind)
		if !familyKeys[familyKey] {
			continue
		}
		row.GladeShape = ShapeTypeKnown
		row.Sources = mergeStrings(row.Sources, []string{"standard-symbol-family"})
		byID[key] = row
	}
}

// methodFamilyReconciliationKey builds an exact-match key from namespace, type,
// member, and kind. The key is used to link signatureless family rows to their
// shaped sibling overloads.
func methodFamilyReconciliationKey(namespace, typeName, memberName, kind string) string {
	return namespace + "\x00" + typeName + "\x00" + memberName + "\x00" + kind
}

func splitTypeName(namespace, name string) (string, string) {
	if namespace != "" {
		return namespace, strings.TrimPrefix(name, namespace+".")
	}
	if idx := strings.LastIndex(name, "."); idx > 0 {
		return name[:idx], name[idx+1:]
	}
	return "System", name
}

func gladeConstructorMemberName(namespace, typeName, memberName string) string {
	if !strings.EqualFold(memberName, lastTypeSegment(typeName)) {
		return memberName
	}
	if strings.EqualFold(namespace, "Messaging.InboundEmail") {
		return typeName
	}
	return typeName
}

func messagingInboundEmailDTOType(namespace, typeName string) bool {
	if !strings.EqualFold(namespace, "Messaging.InboundEmail") {
		return false
	}
	switch typeName {
	case "AuthenticationResult", "AuthenticationResultField", "BinaryAttachment", "TextAttachment":
		return true
	default:
		return false
	}
}

func lastTypeSegment(typeName string) string {
	if idx := strings.LastIndexByte(typeName, '.'); idx >= 0 && idx < len(typeName)-1 {
		return typeName[idx+1:]
	}
	return typeName
}

func memberParameterTypes(params []apexast.Parameter) []string {
	out := make([]string, 0, len(params))
	for _, param := range params {
		out = append(out, param.Type)
	}
	return cleanList(out)
}

func gladeMemberKind(kind string) string {
	switch kind {
	case "method", "constructor":
		return KindMethod
	case "property":
		return KindProperty
	case "field":
		return KindField
	default:
		return KindType
	}
}

func idFromStdlibAPI(api string) string {
	api = strings.TrimSpace(api)
	if isSyntheticStdlibAPI(api) {
		return ""
	}
	parts := strings.SplitN(api, ".", 2)
	if len(parts) != 2 {
		namespace, typeName := splitTypeName("", api)
		return ApexTypeID(namespace, typeName)
	}
	if stdlibTypeAPI(api) {
		namespace, typeName := splitTypeName("", api)
		return ApexTypeID(namespace, typeName)
	}
	params := []string(nil)
	member := parts[1]
	if open := strings.IndexByte(member, '('); open >= 0 && strings.HasSuffix(member, ")") {
		rawParams := strings.TrimSuffix(member[open+1:], ")")
		member = member[:open]
		params = splitSurfaceParameterList(rawParams)
	} else if member == "contains" {
		params = []string{"String"}
	}
	namespace, typeName := splitTypeName("", parts[0])
	return ApexMemberID(namespace, typeName, member, params)
}

func stdlibTypeAPI(api string) bool {
	switch api {
	case "ApexPages.Message", "Database.UnitOfWork", "Messaging.SingleEmailMessage":
		return true
	default:
		return false
	}
}

func isSyntheticStdlibAPI(api string) bool {
	if api == "PageReference(partialURL)" || api == "Search.query / SOSL FIND" || api == "unimplemented platform/stdlib calls" {
		return true
	}
	return strings.Contains(api, "*") || strings.Contains(api, " constructors") || strings.Contains(api, " malformed ")
}

var nonCanonicalGeneratedSurfaceIDs = map[string]struct{}{
	"apex:Schema.ChildRelationship.ChildRelationship()":                                                      {},
	"apex:Schema.DescribeColorResult.DescribeColorResult()":                                                  {},
	"apex:Schema.DescribeDataCategoryGroupResult.DescribeDataCategoryGroupResult()":                          {},
	"apex:Schema.DescribeDataCategoryGroupStructureResult.DescribeDataCategoryGroupStructureResult()":        {},
	"apex:Schema.DescribeFieldResult.DescribeFieldResult()":                                                  {},
	"apex:Schema.DescribeIconResult.DescribeIconResult()":                                                    {},
	"apex:Schema.DescribeSObjectResult.DescribeSObjectResult()":                                              {},
	"apex:Schema.DescribeTabResult.DescribeTabResult()":                                                      {},
	"apex:Schema.DescribeTabSetResult.DescribeTabSetResult()":                                                {},
	"apex:Schema.FieldSet.FieldSet()":                                                                        {},
	"apex:Schema.FieldSetMember.FieldSetMember()":                                                            {},
	"apex:Schema.FilteredLookupInfo.FilteredLookupInfo()":                                                    {},
	"apex:Schema.PicklistEntry.PicklistEntry()":                                                              {},
	"apex:Schema.RecordTypeInfo.RecordTypeInfo()":                                                            {},
	"apex:Schema.SObjectField.SObjectField()":                                                                {},
	"apex:Schema.SObjectType.SObjectType()":                                                                  {},
	"apex:System.EmailException.getDmlFieldNames(Integer)":                                                   {},
	"apex:System.EmailException.getDmlFields(Integer)":                                                       {},
	"apex:System.EmailException.getDmlId(Integer)":                                                           {},
	"apex:System.EmailException.getDmlIndex(Integer)":                                                        {},
	"apex:System.EmailException.getDmlMessage(Integer)":                                                      {},
	"apex:System.EmailException.getDmlStatusCode(Integer)":                                                   {},
	"apex:System.EmailException.getDmlType(Integer)":                                                         {},
	"apex:System.EmailException.getNumDml()":                                                                 {},
	"apex:System.FeatureManagement.FeatureManagement()":                                                      {},
	"apex:System.JSONException.getInaccessibleFields()":                                                      {},
	"apex:System.JSONException.initCause(Exception)":                                                         {},
	"apex:Approval.Approval()":                                                                               {},
	"apex:QueueableDuplicateSignature.QueueableDuplicateSignature()":                                         {},
	"apex:ConnectApi.getError()":                                                                             {},
	"apex:ConnectApi.getErrorMessage()":                                                                      {},
	"apex:ConnectApi.getErrorTypeName()":                                                                     {},
	"apex:ConnectApi.getResult()":                                                                            {},
	"apex:ConnectApi.isSuccess()":                                                                            {},
	"apex:System.Assert.areEqual(Object,Object,Object)":                                                      {},
	"apex:System.Assert.areNotEqual(Object,Object,Object)":                                                   {},
	"apex:System.Assert.isTrue(Boolean,Object)":                                                              {},
	"apex:System.Assert.isFalse(Boolean,Object)":                                                             {},
	"apex:System.Assert.isNull(Object,Object)":                                                               {},
	"apex:System.Assert.isNotNull(Object,Object)":                                                            {},
	"apex:System.Assert.fail(Object)":                                                                        {},
	"apex:System.Http.send(Object)":                                                                          {},
	"apex:Messaging.ActionableNotification.Builder.withActionIdentifier":                                     {},
	"apex:Messaging.ActionableNotification.Builder.withTargetId":                                             {},
	"apex:Messaging.ActionableNotification.Builder.withTargetPageRef":                                        {},
	"apex:Messaging.CustomNotification.CustomNotification(String,String,String,String,String,String,String)": {},
	"apex:Messaging.CustomNotification.setActionGroup(String)":                                               {},
	"apex:System.IntegrationTest.clone()":                                                                    {},
	"apex:Approval.*":                                                                                        {},
	"apex:Search.SuggestionOption.setFilter(Search.KnowledegeSuggestionFilter)":                              {},
	"apex:System.BusinessHours malformed local holiday metadata":                                             {},
	"apex:System.InvalidParameterValueException constructors":                                                {},
	"apex:System.Limits.get*":                                                                                {},
	"apex:System.NoAccessException constructors":                                                             {},
	"apex:System.NoDataFoundException constructors":                                                          {},
	"apex:System.NullPointerException constructors":                                                          {},
	"apex:System.PageReference(partialURL)":                                                                  {},
	"apex:System.Search.query / SOSL FIND":                                                                   {},
	"apex:System.TimeZone.getDisplayName":                                                                    {},
	"apex:System.TimeZone.getTimeZone":                                                                       {},
	"apex:System.unimplemented platform/stdlib calls":                                                        {},
	// CB75: exact frozen non-deferred missing-shape rows proven to be stale,
	// malformed, aliased, or policy-only ledger projections.
	"apex:Canvas.Test_constants": {},
	"apex:Messaging.InboundEmail.AuthenticationResult.InboundEmail.AuthenticationResult()":                   {},
	"apex:Messaging.InboundEmail.AuthenticationResultField.InboundEmail.AuthenticationResultField()":         {},
	"apex:PushUpgradeCustomizationRepository.create(String,String,Boolean,Integer)":                          {},
	"apex:PushUpgradeCustomizationRepository.getCustomizationSummaryById(String)":                            {},
	"apex:PushUpgradeCustomizationRepository.getCustomizationSummaryByIndex(String,String)":                  {},
	"apex:PushUpgradeCustomizationRepository.getExpirationDaysForId(String)":                                 {},
	"apex:PushUpgradeCustomizationRepository.getExpirationDaysForIndex(String,String)":                       {},
	"apex:PushUpgradeCustomizationRepository.getPushUpgradeBlockInitiatedDateForId(String)":                  {},
	"apex:PushUpgradeCustomizationRepository.getPushUpgradeBlockInitiatedDateForIndex(String,String)":        {},
	"apex:PushUpgradeCustomizationRepository.isBlockingCapabilityExpiredForId(String)":                       {},
	"apex:PushUpgradeCustomizationRepository.isBlockingCapabilityExpiredForIndex(String,String)":             {},
	"apex:PushUpgradeCustomizationRepository.listAllCustomizationSummaries()":                                {},
	"apex:PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(String,Boolean,Integer)":           {},
	"apex:PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(String,String,Boolean,Integer)": {},
	"apex:PushUpgradeCustomizationRepository.setExpirationDaysForId(String,Integer)":                         {},
	"apex:PushUpgradeCustomizationRepository.setExpirationDaysForIndex(String,String,Integer)":               {},
	"apex:RestResource":                                                                                     {},
	"apex:System.Database.lock":                                                                             {},
	"apex:System.Database.unlock":                                                                           {},
	"apex:System.Exception.Exception()":                                                                     {},
	"apex:System.Exception.Exception(Exception)":                                                            {},
	"apex:System.Exception.Exception(String)":                                                               {},
	"apex:System.Exception.Exception(String,Exception)":                                                     {},
	"apex:System.InvalidParameterValueException.InvalidParameterValueException()":                           {},
	"apex:System.InvalidParameterValueException.InvalidParameterValueException(Exception)":                  {},
	"apex:System.InvalidParameterValueException.InvalidParameterValueException(String)":                     {},
	"apex:System.InvalidParameterValueException.InvalidParameterValueException(String,String)":              {},
	"apex:System.Iterator.remove":                                                                           {},
	"apex:System.Matcher.appendReplacement":                                                                 {},
	"apex:System.Matcher.appendTail":                                                                        {},
	"apex:System.Messaging.SingleEmailMessage":                                                              {},
	"apex:System.NoAccessException.NoAccessException(Exception)":                                            {},
	"apex:System.NoAccessException.NoAccessException(String)":                                               {},
	"apex:System.NoAccessException.NoAccessException(String,Exception)":                                     {},
	"apex:System.NoDataFoundException.NoDataFoundException(Exception)":                                      {},
	"apex:System.NoDataFoundException.NoDataFoundException(String)":                                         {},
	"apex:System.NoDataFoundException.NoDataFoundException(String,Exception)":                               {},
	"apex:System.NullPointerException.NullPointerException(Exception)":                                      {},
	"apex:System.NullPointerException.NullPointerException(String)":                                         {},
	"apex:System.NullPointerException.NullPointerException(String,Exception)":                               {},
	"apex:System.PushUpgradeCustomizationRepository.create(String,String,Boolean)":                          {},
	"apex:System.PushUpgradeCustomizationRepository.deleteById(String)":                                     {},
	"apex:System.PushUpgradeCustomizationRepository.deleteByIndex(String,String)":                           {},
	"apex:System.PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForId(String)":                   {},
	"apex:System.PushUpgradeCustomizationRepository.getCustomUpgradeAllowedForIndex(String,String)":         {},
	"apex:System.PushUpgradeCustomizationRepository.getCustomUpgradeTypeForId(String)":                      {},
	"apex:System.PushUpgradeCustomizationRepository.getCustomUpgradeTypeForIndex(String,String)":            {},
	"apex:System.PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForId(String,Boolean)":           {},
	"apex:System.PushUpgradeCustomizationRepository.setCustomUpgradeAllowedForIndex(String,String,Boolean)": {},
	"apex:System.Type.newInstance":                                                                          {},
}

func isNonCanonicalGeneratedSurfaceID(id string) bool {
	if _, ok := nonCanonicalGeneratedSurfaceIDs[id]; ok {
		return true
	}
	key := surfaceIDKey(id)
	for candidate := range nonCanonicalGeneratedSurfaceIDs {
		if surfaceIDKey(candidate) == key {
			return true
		}
	}
	return false
}

func removeNonCanonicalGeneratedRows(byID map[string]SurfaceLedgerRow) {
	for id := range nonCanonicalGeneratedSurfaceIDs {
		delete(byID, surfaceIDKey(id))
	}
}

func splitSurfaceParameterList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var params []string
	depth := 0
	start := 0
	for i, r := range raw {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				params = append(params, strings.TrimSpace(raw[start:i]))
				start = i + len(string(r))
			}
		}
	}
	params = append(params, strings.TrimSpace(raw[start:]))
	return params
}

func idFromStubBehavior(entry capability.StubBehaviorEntry) string {
	namespace, typeName := splitTypeName("", entry.Type)
	if entry.Member == "" {
		return ApexTypeID(namespace, typeName)
	}
	if gladeMemberKind(entry.Kind) == KindProperty {
		return ApexMemberID(namespace, typeName, entry.Member, nil)
	}
	return ApexMemberID(namespace, typeName, entry.Member, entry.Parameters)
}

func behaviorFromCapabilityStatus(status capability.Status) BehaviorState {
	switch status {
	case capability.StatusSupported:
		return BehaviorSupported
	case capability.StatusPartial:
		return BehaviorPartial
	case capability.StatusUnsupported:
		return BehaviorUnsupported
	case capability.StatusStub:
		return BehaviorStubNoOp
	default:
		return BehaviorNone
	}
}

func behaviorFromStubStatus(status capability.StubBehaviorStatus) BehaviorState {
	switch status {
	case capability.StubBehaviorImplemented:
		return BehaviorSupported
	case capability.StubBehaviorPassiveDefault:
		return BehaviorPassive
	case capability.StubBehaviorStubNoOp:
		return BehaviorStubNoOp
	case capability.StubBehaviorUnsupported:
		return BehaviorUnsupported
	default:
		return BehaviorNone
	}
}

func fillFromApexID(row *SurfaceLedgerRow) {
	if row == nil || !strings.HasPrefix(row.SurfaceID, "apex:") {
		return
	}
	rest := strings.TrimPrefix(row.SurfaceID, "apex:")
	identity := rest
	parameters := ""
	hasParameters := false
	if open := strings.IndexByte(rest, '('); open >= 0 && strings.HasSuffix(rest, ")") {
		identity = rest[:open]
		parameters = rest[open+1 : len(rest)-1]
		hasParameters = true
	}
	if dot := strings.LastIndex(identity, "."); dot > 0 {
		prefix := identity[:dot]
		member := identity[dot+1:]
		if hasParameters || row.Kind == KindMethod || row.Kind == KindProperty || row.Kind == KindField {
			row.MemberName = member
			if hasParameters {
				parsedParameters := splitSurfaceParameterList(parameters)
				if strings.EqualFold(identity, "System.EventBus.publishWithAccessLevel") {
					row.Parameters = canonicalEventBusAccessLevelParameters(parsedParameters)
				} else {
					row.Parameters = cleanList(parsedParameters)
				}
			}
			fillApexTypeParts(row, prefix)
			return
		}
		row.Namespace = prefix
		row.TypeName = member
	}
}

func fillApexTypeParts(row *SurfaceLedgerRow, typePart string) {
	if typeDot := strings.LastIndex(typePart, "."); typeDot > 0 {
		row.Namespace = typePart[:typeDot]
		row.TypeName = typePart[typeDot+1:]
		return
	}
	row.Namespace = ""
	row.TypeName = typePart
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
