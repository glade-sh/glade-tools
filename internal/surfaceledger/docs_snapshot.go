package surfaceledger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
)

func BuildDocsSnapshot(source string) ([]SurfaceLedgerRow, error) {
	if err := validateDocsSource(source); err != nil {
		return nil, err
	}
	inv, err := apexdocs.BuildInventory(source)
	if err != nil {
		return nil, err
	}
	rows := ReleaseRowsFromDocsInventory(inv)
	if len(rows) == 0 {
		return nil, fmt.Errorf("docs source produced zero inventory rows: %s", source)
	}
	apexRows := 0
	for _, row := range rows {
		if row.Product == ProductApex {
			apexRows++
		}
	}
	if apexRows == 0 {
		return nil, fmt.Errorf("docs source produced no Apex inventory rows: %s", source)
	}
	return rows, nil
}

// validateDocsSource prevents an empty or missing docs cache from being
// treated as a valid zero-row Salesforce inventory. A zero-row docs snapshot
// changes the denominator and can make a refresh look complete while omitting
// the public Apex surface entirely.
func validateDocsSource(source string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("docs source: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("docs source is not a directory: %s", source)
	}
	files := 0
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != source && entry.Type().IsRegular() {
			files++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan docs source: %w", err)
	}
	if files == 0 {
		return fmt.Errorf("docs source is empty: %s", source)
	}
	return nil
}

func filterUnresolvedApexHeadingRows(rows []SurfaceLedgerRow) []SurfaceLedgerRow {
	out := rows[:0]
	for _, row := range rows {
		if row.Product == ProductApex && row.Kind == KindMethod && row.ReturnType == "" && isApexHeadingOnlySignature(row.Signature) {
			continue
		}
		out = append(out, row)
	}
	return out
}

func RowsFromDocsInventory(inv apexdocs.Inventory) []SurfaceLedgerRow {
	var rows []SurfaceLedgerRow
	for _, doc := range inv.Documents {
		product := ProductFromSourcePath(doc.SourcePath)
		if !shouldIncludeDocsSurface(product, doc) {
			continue
		}
		apiVersion := doc.APIVersion
		namespace := docsNamespace(product, doc)
		typeName := doc.Name
		if product == ProductDataRef {
			typeName = dataReferenceDocsName(doc)
		}
		if product == ProductApex {
			identity := apexDocsIdentity(doc)
			namespace = identity.namespace
			typeName = identity.typeName
			if identity.memberName != "" {
				rows = append(rows, rowFromDocsSnapshot(SurfaceLedgerRow{
					SurfaceID:  ApexMemberID(namespace, typeName, identity.memberName, identity.parameters),
					Product:    product,
					Area:       areaForProduct(product),
					Namespace:  namespace,
					TypeName:   typeName,
					MemberName: identity.memberName,
					Kind:       identity.kind,
					Signature:  identity.signature,
					Parameters: identity.parameters,
					APIVersion: apiVersion,
					DocsSource: doc.SourcePath,
					DocsTitle:  doc.Title,
					Sources:    []string{"docs"},
				}))
				continue
			}
		}
		surfaceID := docsSurfaceID(product, doc, apexdocs.Member{})
		if product == ProductApex {
			surfaceID = ApexTypeID(namespace, typeName)
		}
		if shouldEmitDocsDocumentRow(product, doc) {
			row := rowFromDocsSnapshot(SurfaceLedgerRow{
				SurfaceID:  surfaceID,
				Product:    product,
				Area:       areaForProduct(product),
				Namespace:  namespace,
				TypeName:   typeName,
				Kind:       docsDocumentKind(product, doc.Kind),
				APIVersion: apiVersion,
				DocsSource: doc.SourcePath,
				DocsTitle:  doc.Title,
				Sources:    []string{"docs"},
			})
			rows = append(rows, row)
		}
		apexRealSignatures := apexMembersWithRealSignatures(doc.Members)
		for _, member := range doc.Members {
			if product == ProductApex && isApexHeadingOnlySignature(member.Signature) && apexRealSignatures[member.Name] {
				continue
			}
			if product == ProductApex && member.Kind == "member" && member.ReturnType == "" && member.PropertyType == "" && !isApexRealSignature(member.Signature) {
				if rest := strings.TrimSpace(strings.TrimPrefix(member.Signature, member.Name)); rest != "" {
					continue
				}
			}
			params := canonicalDocsParameters(namespace, docsMemberParameters(member))
			returnType := docsMemberReturnType(member)
			memberAPIVersion := apiVersion
			if member.APIVersion != "" {
				memberAPIVersion = member.APIVersion
			}
			surfaceID := docsSurfaceID(product, doc, member)
			if product == ProductApex {
				surfaceID = ApexMemberID(namespace, typeName, member.Name, params)
			}
			rows = append(rows, rowFromDocsSnapshot(SurfaceLedgerRow{
				SurfaceID:  surfaceID,
				Product:    product,
				Area:       areaForProduct(product),
				Namespace:  namespace,
				TypeName:   typeName,
				MemberName: member.Name,
				Kind:       docsKind(product, member.Kind),
				Signature:  member.Signature,
				ReturnType: returnType,
				Parameters: params,
				APIVersion: memberAPIVersion,
				DocsSource: doc.SourcePath,
				DocsTitle:  doc.Title,
				Sources:    []string{"docs"},
			}))
		}
	}
	sortRows(rows)
	return rows
}

// ReleaseRowsFromDocsInventory drops heading-only Apex guesses that a checked
// release inventory cannot resolve to a declaration signature.
func ReleaseRowsFromDocsInventory(inv apexdocs.Inventory) []SurfaceLedgerRow {
	return filterUnresolvedApexHeadingRows(RowsFromDocsInventory(inv))
}

func rowFromDocsSnapshot(row SurfaceLedgerRow) SurfaceLedgerRow {
	row = identifyDocsSourceFamily(row)
	return RowFromDocs(row)
}

func identifyDocsSourceFamily(row SurfaceLedgerRow) SurfaceLedgerRow {
	family := sourceFamilyFromPath(row.DocsSource)
	if family == "" {
		family = surfaceFamilyForProduct(row.Product)
	}
	if family != "" && family != ProductUnknown {
		row.SalesforceSurfaceFamily = family
	}
	return row
}

func docsMemberReturnType(member apexdocs.Member) string {
	if member.PropertyType != "" {
		return member.PropertyType
	}
	return member.ReturnType
}

func docsMemberParameters(member apexdocs.Member) []string {
	if len(member.Parameters) > 0 {
		return append([]string(nil), member.Parameters...)
	}
	return parametersFromSignature(member.Signature)
}

type apexDocsDocumentIdentity struct {
	namespace  string
	typeName   string
	memberName string
	signature  string
	parameters []string
	kind       string
}

func apexDocsIdentity(doc apexdocs.Document) apexDocsDocumentIdentity {
	identity := apexDocsDocumentIdentity{
		namespace: docsNamespace(ProductApex, doc),
		typeName:  doc.Name,
		kind:      docsKind(ProductApex, doc.Kind),
	}
	titleTypeName := ""
	if identity.namespace == "ConnectApi" {
		titleTypeName = connectApiTypeNameFromTitle(doc.Title)
		if titleTypeName != "" {
			identity.typeName = titleTypeName
		}
		if typeName := connectApiStandaloneMethodParent(doc.SourcePath, doc.Name); typeName != "" {
			identity.typeName = typeName
		}
	}
	namespace, typeName, memberName, memberKind, ok := inferApexIdentityFromSource(doc.SourcePath, doc.Name)
	if !ok {
		return identity
	}
	identity.namespace = namespace
	if len(doc.Members) > 0 {
		return identity
	}
	if titleTypeName == "" && identity.typeName == doc.Name {
		identity.typeName = typeName
	}
	if memberName == "" {
		return identity
	}
	identity.memberName = memberName
	identity.signature = doc.Signature
	if identity.signature == "" {
		identity.signature = doc.Name
	}
	identity.parameters = parametersFromSignature(identity.signature)
	identity.parameters = canonicalDocsParameters(identity.namespace, identity.parameters)
	identity.kind = memberKind
	return identity
}

func apexMembersWithRealSignatures(members []apexdocs.Member) map[string]bool {
	out := map[string]bool{}
	for _, member := range members {
		if isApexRealSignature(member.Signature) {
			out[member.Name] = true
		}
	}
	return out
}

func isApexHeadingOnlySignature(signature string) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" || isApexRealSignature(signature) {
		return false
	}
	return strings.Contains(signature, "(") && strings.Contains(signature, ")")
}

func isApexRealSignature(signature string) bool {
	signature = strings.TrimSpace(signature)
	first := strings.Fields(signature)
	if len(first) == 0 {
		return false
	}
	switch strings.ToLower(first[0]) {
	case "public", "private", "protected", "global", "webservice", "static":
		return true
	default:
		return false
	}
}

func shouldIncludeDocsSurface(product string, doc apexdocs.Document) bool {
	if doc.InternalOnly {
		return false
	}
	if product == ProductUnknown && strings.EqualFold(sourceStemBase(doc.SourcePath), "apex_cursors_versus_batch") {
		return false
	}
	stem := strings.ToLower(sourceStemBase(doc.SourcePath))
	switch product {
	case ProductCLIReference:
		return stem != "cli-reference"
	case ProductConnectRESTAPI:
		return !isConnectRESTAPIRollup(doc.SourcePath)
	case ProductREST:
		return !hasAnySuffix(stem, "_intro", "_setup", "_vscode")
	case ProductServiceConnectorAPIRef:
		return stem != "index"
	case ProductTooling:
		return strings.HasPrefix(stem, "tooling_api_objects_")
	}
	if product != ProductApex {
		return true
	}
	if isApexGuideDoc(doc.SourcePath) || isApexNamespaceDoc(doc.SourcePath) || isGeneratedCustomObjectMethodDoc(doc.SourcePath) || isConnectApiSummaryDoc(doc.SourcePath) {
		return false
	}
	return true
}

func isConnectRESTAPIRollup(sourcePath string) bool {
	rel := strings.TrimPrefix(strings.ToLower(filepath.ToSlash(sourcePath)), "connect-rest-api/")
	first, _, _ := strings.Cut(rel, "/")
	stem := strings.TrimSuffix(first, filepath.Ext(first))
	return stem == "index" || strings.HasPrefix(stem, "connect-rest-api-")
}

func isConnectApiSummaryDoc(sourcePath string) bool {
	switch strings.ToLower(sourceStemBase(sourcePath)) {
	case "apex_connectapi_input", "apex_connectapi_input_retired", "apex_connectapi_output", "apex_connectapi_output_retired", "apex_connectapi_release_notes":
		return true
	default:
		return false
	}
}

func connectApiTypeNameFromTitle(title string) string {
	title = cleanIdentityPart(title)
	name := ""
	if strings.HasPrefix(title, "ConnectApi.") {
		name = strings.TrimPrefix(title, "ConnectApi.")
		if idx := strings.Index(name, " ("); idx >= 0 {
			name = name[:idx]
		}
	} else if strings.HasSuffix(title, " Class") {
		name = strings.TrimSuffix(title, " Class")
	} else if strings.HasSuffix(title, " Methods") {
		name = strings.TrimSuffix(title, " Methods")
	}
	name = strings.TrimSpace(name)
	if !isApexIdentifier(name) {
		return ""
	}
	return name
}

func connectApiStandaloneMethodParent(sourcePath, name string) string {
	base := sourceStemBase(sourcePath)
	lower := strings.ToLower(base)
	const prefix = "apex_connectapi_output_"
	if !strings.HasPrefix(lower, prefix) || !strings.Contains(name, "(") {
		return ""
	}
	member := strings.TrimSpace(name[:strings.Index(name, "(")])
	suffix := "_" + camelToSnake(member)
	rest := base[len(prefix):]
	if !strings.HasSuffix(strings.ToLower(rest), suffix) {
		return ""
	}
	rest = rest[:len(rest)-len(suffix)]
	return snakeToPascal(rest)
}

func isApexIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func upperFirstASCII(value string) string {
	if value != "" && value[0] >= 'a' && value[0] <= 'z' {
		return string(value[0]-('a'-'A')) + value[1:]
	}
	return value
}

func camelToSnake(value string) string {
	var out strings.Builder
	for i, r := range value {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		out.WriteRune(r)
	}
	return out.String()
}

func snakeToPascal(value string) string {
	var out strings.Builder
	for _, part := range strings.Split(value, "_") {
		out.WriteString(upperFirstASCII(part))
	}
	return out.String()
}

func shouldEmitDocsDocumentRow(product string, doc apexdocs.Document) bool {
	if product != ProductDataRef {
		return true
	}
	return isDataReferenceObjectDoc(doc.SourcePath)
}

func isDataReferenceObjectDoc(sourcePath string) bool {
	path := filepath.ToSlash(sourcePath)
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[len(parts)-2] != "object-reference" {
		return false
	}
	stem := strings.ToLower(sourceStemBase(sourcePath))
	if strings.HasPrefix(stem, "sforce_api_objects_eventlogfile_") {
		return true
	}
	if !strings.HasPrefix(stem, "sforce_api_objects_") {
		return false
	}
	rest := strings.TrimPrefix(stem, "sforce_api_objects_")
	switch rest {
	case "concepts", "custom_object__c", "custom_objects", "custommetadatatype__mdt", "customobject__feed", "external_objects", "fequently_occurring_fields", "frequently_occurring_fields", "list", "salesforce_surveys_object_model":
		return false
	default:
		return rest != ""
	}
}

func isApexGuideDoc(sourcePath string) bool {
	base := strings.ToLower(sourceStemBase(sourcePath))
	if strings.HasSuffix(base, "_exceptions") || strings.HasPrefix(base, "apex_exceptions_") || strings.HasPrefix(base, "apex_shopping_cart_example") {
		return true
	}
	switch base {
	case "apex_dml_section", "apex_appendices", "apex_qs_conventions", "apex_ref_guide", "apex_releasenotes", "apex_reserved_words", "versioned_behavior_changes":
		return true
	default:
		return false
	}
}

func isApexNamespaceDoc(sourcePath string) bool {
	return strings.HasPrefix(strings.ToLower(sourceStemBase(sourcePath)), "apex_namespace_")
}

func isGeneratedCustomObjectMethodDoc(sourcePath string) bool {
	switch sourceStemBase(sourcePath) {
	case "apex_methods_system_custom_metadata_types", "apex_methods_system_custom_settings":
		return true
	default:
		return false
	}
}

func sourceStemBase(sourcePath string) string {
	return strings.TrimSuffix(filepath.Base(filepath.ToSlash(sourcePath)), filepath.Ext(sourcePath))
}

func ProductFromSourcePath(sourcePath string) string {
	parts := strings.Split(filepath.ToSlash(sourcePath), "/")
	for _, part := range parts {
		switch strings.ToLower(part) {
		case "apex":
			return ProductApex
		case "bulk-api":
			return ProductBulkAPI
		case "cli-reference":
			return ProductCLIReference
		case "commerce-cli-reference":
			return ProductCommerceCLIReference
		case "analytics-cli-reference":
			return ProductAnalyticsCLIReference
		case "connect-rest-api":
			return ProductConnectRESTAPI
		case "lightning":
			return ProductLightning
		case "metadata-api":
			return ProductMetadataAPI
		case "platform-events":
			return ProductPlatformEvents
		case "rest-api", "rest_api":
			return ProductREST
		case "service-connector-api-reference":
			return ProductServiceConnectorAPIRef
		case "site-references":
			return ProductSiteReferences
		case "soap-api":
			return ProductSOAPAPI
		case "streaming-api":
			return ProductStreamingAPI
		case "tooling-api", "tooling_api":
			return ProductTooling
		case "ui-api":
			return ProductUIAPI
		case "visualforce":
			return ProductVisualforce
		case "lightning-aura", "aura":
			return ProductAura
		case "lwc", "lightning-web-components":
			return ProductLWC
		case "object-reference", "field-reference":
			return ProductDataRef
		}
	}
	return ProductUnknown
}

func sourceFamilyFromPath(sourcePath string) string {
	product := ProductFromSourcePath(sourcePath)
	if product == ProductUnknown {
		return ""
	}
	return surfaceFamilyForProduct(product)
}

func docsSurfaceID(product string, doc apexdocs.Document, member apexdocs.Member) string {
	switch product {
	case ProductApex:
		ns := docsNamespace(product, doc)
		if member.Name == "" {
			return ApexTypeID(ns, doc.Name)
		}
		return ApexMemberID(ns, doc.Name, member.Name, parametersFromSignature(member.Signature))
	case ProductTooling:
		if member.Name == "" {
			return ToolingObjectID(doc.Name)
		}
		return ToolingFieldID(doc.Name, member.Name)
	case ProductDataRef:
		if member.Name == "" {
			return DataObjectID(dataReferenceDocsName(doc))
		}
		return DataFieldID(dataReferenceDocsName(doc), member.Name)
	case ProductREST:
		if member.Name == "" {
			return RestResourceID(sourceStem(doc.SourcePath), "get")
		}
		return RestResourceID(sourceStem(doc.SourcePath), member.Name)
	case ProductVisualforce:
		if member.Name == "" {
			return "visualforce:" + sourceStem(doc.SourcePath)
		}
		return VisualforceAttrID(docsNamespace(product, doc), strings.TrimPrefix(strings.ToLower(doc.Name), "apex:"), member.Name)
	case ProductAura:
		return AuraID(sourceStem(doc.SourcePath))
	case ProductLWC:
		return LWCModuleID(doc.Name)
	case ProductServiceConnectorAPIRef:
		stem := strings.ReplaceAll(sourceStemBase(doc.SourcePath), "-", "_")
		if member.Name == "" {
			return product + ":" + stem
		}
		return product + ":" + stem + "." + member.Name
	default:
		if member.Name == "" {
			return product + ":" + sourceStem(doc.SourcePath)
		}
		return product + ":" + sourceStem(doc.SourcePath) + "." + member.Name
	}
}

func dataReferenceDocsName(doc apexdocs.Document) string {
	stem := sourceStemBase(doc.SourcePath)
	if strings.HasPrefix(stem, "sforce_api_objects_eventlogfile_") {
		return "EventLogFile"
	}
	if isDataReferenceObjectDoc(doc.SourcePath) {
		if name := dataReferenceNameFromTitle(doc.Title); name != "" {
			return name
		}
	}
	return cleanIdentityPart(doc.Name)
}

func dataReferenceNameFromTitle(title string) string {
	title = cleanIdentityPart(title)
	if idx := strings.IndexByte(title, '('); idx >= 0 {
		title = strings.TrimSpace(title[:idx])
	}
	title = strings.TrimSuffix(title, " Object")
	title = strings.TrimSuffix(title, " object")
	parts := strings.Fields(title)
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		part = strings.Trim(part, ",.;:")
		if part == "" {
			continue
		}
		b.WriteString(part)
	}
	return b.String()
}

func docsNamespace(product string, doc apexdocs.Document) string {
	if doc.Namespace != "" {
		if product == ProductApex {
			return canonicalApexNamespaceName(doc.Namespace)
		}
		return doc.Namespace
	}
	if product == ProductApex {
		return inferApexNamespace(doc.SourcePath, doc.Name)
	}
	if product == ProductVisualforce {
		return "apex"
	}
	return ""
}

func inferApexNamespace(sourcePath, name string) string {
	path := strings.ToLower(filepath.ToSlash(sourcePath))
	if strings.Contains(path, "apex_fsccashflow") {
		return "fsccashflow"
	}
	if strings.Contains(path, "apex_canvas") || strings.Contains(path, "/canvas") {
		return "Canvas"
	}
	if strings.Contains(path, "system_") || strings.Contains(path, "/system") || name == "Object" || name == "String" || name == "Label" {
		return "System"
	}
	if strings.Contains(path, "connectapi") {
		return "ConnectApi"
	}
	if strings.Contains(path, "schema") {
		return "Schema"
	}
	return "System"
}

func inferApexIdentityFromSource(sourcePath, name string) (string, string, string, string, bool) {
	base := sourceStemBase(sourcePath)
	lower := strings.ToLower(base)
	switch lower {
	case "apex_commercepay_postauthapipaymethodreq_altpaymethod", "apex_commercepayments_postauthapipaymentmethodrequest_alternativepaymentmethod":
		return "commercepayments", "PostAuthApiPaymentMethodRequest", "alternativePaymentMethod", KindProperty, true
	case "apex_commercepay_postauthresp_setauthexpirationdate":
		return "commercepayments", "PostAuthorizationResponse", "setAuthorizationExpirationDate", KindMethod, true
	case "apex_commercepay_postauthresp_setgatewayresultcodedesc":
		return "commercepayments", "PostAuthorizationResponse", "setGatewayResultCodeDescription", KindMethod, true
	}
	for _, prefix := range []struct {
		key       string
		namespace string
	}{
		{key: "apex_system_", namespace: "System"},
		{key: "apex_messaging_", namespace: "Messaging"},
		{key: "apex_connectapi_", namespace: "ConnectApi"},
		{key: "apex_industriesnlpsvc_", namespace: "industriesNlpSvc"},
		{key: "apex_commercepayments_", namespace: "commercepayments"},
		{key: "apex_canvas_", namespace: "Canvas"},
	} {
		if !strings.HasPrefix(lower, prefix.key) {
			continue
		}
		rest := base[len(prefix.key):]
		typeName, memberName := apexTypeAndMemberFromStem(rest, name)
		if typeName == "" {
			return "", "", "", "", false
		}
		return prefix.namespace, typeName, memberName, KindMethod, true
	}
	return "", "", "", "", false
}

func canonicalDocsParameters(namespace string, parameters []string) []string {
	if parameters == nil {
		return nil
	}
	out := append([]string{}, parameters...)
	for i, parameter := range out {
		if len(namespace) == 0 || len(parameter) <= len(namespace) || parameter[:len(namespace)] != strings.ToLower(namespace) {
			continue
		}
		next, _ := utf8.DecodeRuneInString(parameter[len(namespace):])
		if unicode.IsUpper(next) {
			out[i] = namespace + "." + parameter[len(namespace):]
		}
	}
	return out
}

func hasAnySuffix(value string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func apexTypeAndMemberFromStem(rest, name string) (string, string) {
	for _, suffix := range []string{"_methods", "_properties", "_constructors"} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), ""
		}
	}
	if idx := strings.Index(rest, "_ctor"); idx > 0 {
		typeName := rest[:idx]
		return typeName, typeName
	}
	open := strings.Index(name, "(")
	if open <= 0 {
		return rest, ""
	}
	if idx := strings.IndexByte(rest, '_'); idx > 0 {
		return rest[:idx], strings.TrimSpace(name[:open])
	}
	return rest, ""
}

func docsKind(product, kind string) string {
	switch product {
	case ProductREST:
		return KindResource
	case ProductDataRef:
		if strings.TrimSpace(kind) == "" || strings.EqualFold(kind, "type") {
			return KindType
		}
		return KindField
	case ProductLWC:
		return KindModule
	case ProductAura:
		return KindGuide
	}
	switch strings.ToLower(kind) {
	case "method", "constructor":
		return KindMethod
	case "property", "member":
		return KindProperty
	case "field":
		return KindField
	default:
		return KindType
	}
}

func docsDocumentKind(product, kind string) string {
	if product == ProductDataRef {
		return KindType
	}
	return docsKind(product, kind)
}

func areaForProduct(product string) string {
	switch product {
	case ProductREST, ProductTooling, ProductBulkAPI, ProductCLIReference, ProductCommerceCLIReference, ProductConnectRESTAPI,
		ProductAnalyticsCLIReference, ProductMetadataAPI, ProductPlatformEvents,
		ProductServiceConnectorAPIRef, ProductSOAPAPI, ProductStreamingAPI:
		return AreaServer
	case ProductDataRef:
		return AreaData
	case ProductVisualforce, ProductAura, ProductLWC, ProductLightning, ProductSiteReferences, ProductUIAPI:
		return AreaUI
	default:
		return AreaRuntime
	}
}

func parametersFromSignature(signature string) []string {
	open := strings.Index(signature, "(")
	close := strings.LastIndex(signature, ")")
	if open < 0 || close < open {
		return nil
	}
	inside := strings.TrimSpace(signature[open+1 : close])
	if inside == "" {
		return []string{}
	}
	parts := splitSurfaceParameterList(inside)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		paramType := parameterTypeFromSignaturePart(part)
		if paramType == "" {
			continue
		}
		out = append(out, paramType)
	}
	return out
}

func parameterTypeFromSignaturePart(part string) string {
	part = strings.TrimSpace(part)
	if part == "" {
		return ""
	}
	depth := 0
	split := -1
	for i, r := range part {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && unicode.IsSpace(r) {
				split = i
			}
		}
	}
	if split < 0 {
		if genericEnd := stuckGenericParameterNameSplit(part); genericEnd >= 0 {
			return strings.TrimSpace(part[:genericEnd+1])
		}
		return part
	}
	return strings.TrimSpace(part[:split])
}

func stuckGenericParameterNameSplit(part string) int {
	depth := 0
	for i, r := range part {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
			if depth == 0 && i+1 < len(part) {
				next := rune(part[i+1])
				if next == '_' || unicode.IsLetter(next) {
					return i
				}
			}
		}
	}
	return -1
}

func sourceStem(sourcePath string) string {
	sourcePath = strings.TrimSuffix(filepath.ToSlash(sourcePath), filepath.Ext(sourcePath))
	parts := strings.Split(sourcePath, "/")
	if len(parts) > 1 {
		sourcePath = strings.Join(parts[1:], "/")
	}
	return cleanIdentityPart(sourcePath)
}
