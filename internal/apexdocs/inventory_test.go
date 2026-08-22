package apexdocs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildInventoryParsesSystemMethodPage(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_methods_system_string.md"), `# String Class

## Namespace
[System](./apex_namespace_System.md)

## String Methods
### contains(substring)
Returns true if this string contains the specified substring.

### trim()
Removes leading and trailing white space.

### Example: Trim a value
String s = ' hi ';
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.SchemaVersion != InventorySchemaVersion {
		t.Fatalf("schema version = %d", inv.SchemaVersion)
	}
	if inv.TotalFiles != 1 || inv.TotalMembers != 2 {
		t.Fatalf("summary = files %d members %d", inv.TotalFiles, inv.TotalMembers)
	}
	doc := inv.Documents[0]
	if doc.SourcePath != "apex_methods_system_string.md" || doc.Kind != "class" || doc.Namespace != "System" || doc.Name != "String" {
		t.Fatalf("doc metadata = %#v", doc)
	}
	if got := doc.Members[0].Signature; got != "contains(substring)" {
		t.Fatalf("first member = %q", got)
	}
	if len(doc.Examples) != 1 || doc.Examples[0].Heading != "Example: Trim a value" {
		t.Fatalf("examples = %#v", doc.Examples)
	}
}

func TestBuildInventoryParsesMultilineInlineSignature(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_methods_system_approval.md"), `# Approval Class

## Approval Methods
### lock(recordId)

#### Signature

`+"`"+`public static Approval.LockResult lock(Id
recordId)`+"`"+`
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	member := inv.Documents[0].Members[0]
	if member.Signature != "public static Approval.LockResult lock(Id recordId)" || len(member.Parameters) != 1 || member.Parameters[0] != "Id" {
		t.Fatalf("member = %#v", member)
	}
}

func TestBuildInventorySkipsInlineProseBeforeSignature(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_class_Messaging_Builder.md"), `# Builder Class

## Builder Methods
### withTargetId(targetId)

#### Signature

Specify either a `+"`"+`targetID`+"`"+` or a target page.

`+"`"+`public Messaging.Builder withTargetId(String
targetId)`+"`"+`
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	member := inv.Documents[0].Members[0]
	if member.Signature != "public Messaging.Builder withTargetId(String targetId)" || len(member.Parameters) != 1 || member.Parameters[0] != "String" {
		t.Fatalf("member = %#v", member)
	}
}

func TestBuildInventoryReadsSignatureAfterNote(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_classes_sites_cookie.md"), `# Cookie Class

## Cookie Constructors
### Cookie(name, value, path, maxAge, isSecure, sameSite)

#### Signature

#### Note

The default is `+"`"+`Lax`+"`"+`.

`+"`"+`public Cookie(String name, String value, String path, Integer maxAge, Boolean isSecure, String sameSite)`+"`"+`
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	member := inv.Documents[0].Members[0]
	if member.Signature != "public Cookie(String name, String value, String path, Integer maxAge, Boolean isSecure, String sameSite)" {
		t.Fatalf("signature = %q", member.Signature)
	}
}

func TestBuildInventoryRecoversTrailingParameterTypesFromParametersSection(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_class_Invocable_Action.md"), `# Action Class

## Action Methods
### createCustomAction(type, namespace, name, version)

#### Signature

`+"`"+`public static Invocable.Action createCustomAction(String type, String namespace, String name)`+"`"+`

#### Parameters

type
:   Type: String

namespace
:   Type: String

name
:   Type: String

version
:   Type: String

#### Return Value

Type: Invocable.Action
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	member := inv.Documents[0].Members[0]
	if member.Signature != "public static Invocable.Action createCustomAction(String type, String namespace, String name, String version)" {
		t.Fatalf("signature = %q", member.Signature)
	}
	if got := strings.Join(member.Parameters, ","); got != "String,String,String,String" {
		t.Fatalf("parameters = %q", got)
	}
}

func TestBuildInventoryMarksInternalOnlyIntro(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_class_setup_Internal.md"), `# Internal Class

The methods and properties in this class are for internal use only.

## Namespace

[setup](./apex_namespace_setup.md)
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Documents[0].InternalOnly {
		t.Fatalf("document = %#v", inv.Documents[0])
	}
}

func TestBuildInventoryDoesNotApplyNestedAvailabilityToWholeDocument(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_methods_system_system.md"), `# System Class

## System Methods
### oldMethod()

#### Signature

`+"`"+`public static Boolean oldMethod()`+"`"+`

### newMethod()

#### API Version

67.0

#### Signature

`+"`"+`public static Boolean newMethod()`+"`"+`
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.Documents[0].APIVersion; got != "" {
		t.Fatalf("document API version = %q, want empty for a multi-surface page", got)
	}
	for _, member := range inv.Documents[0].Members {
		if member.Name == "newMethod" && member.APIVersion != "67.0" {
			t.Fatalf("newMethod API version = %q, want 67.0", member.APIVersion)
		}
	}
}

func TestBuildInventoryUsesDocumentAvailabilityInsteadOfNestedTableValues(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "tooling_api_objects_customfield.md"), `# CustomField

Available from API version 28.0 or later.

## Fields

| Field | Description |
| --- | --- |
| creationType | Available in API version 67.0 and later. |
`)
	writeDoc(t, filepath.Join(root, "tooling_api_objects_nested_only.md"), `# NestedOnly

## Fields

| Field | Description |
| --- | --- |
| newField | Available in API version 67.0 and later. |
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.Documents[0].APIVersion; got != "28.0" {
		t.Fatalf("CustomField API version = %q", got)
	}
	if got := inv.Documents[1].APIVersion; got != "" {
		t.Fatalf("nested-only document API version = %q", got)
	}
}

func TestBuildInventoryUsesDedicatedAvailabilityTableForPropertyOnlyDocument(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_connectapi_output_quote.md"), `# ConnectApi.Quote

Representation of a quote.

| Property Name | Type | Available Version |
| --- | --- | --- |
| id | String | 66.0 |
| name | String | 66.0 |
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.Documents[0].APIVersion; got != "66.0" {
		t.Fatalf("property-only document API version = %q", got)
	}
}

func TestBuildInventoryParsesToolingDocumentAvailability(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "tooling_api_objects_group.md"), `# Group

Available in Tooling API version 38.0 and later.
`)
	writeDoc(t, filepath.Join(root, "tooling_api_objects_engagement.md"), `# Engagement

This object is available in API versions 65.0 and later.
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Documents[0].APIVersion != "65.0" || inv.Documents[1].APIVersion != "38.0" {
		t.Fatalf("document API versions = %q, %q", inv.Documents[0].APIVersion, inv.Documents[1].APIVersion)
	}
}

func TestBuildInventorySkipsNarrativeSubsectionsInMethodLists(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_class_System_Queueable.md"), `# Queueable Interface

## Namespace
[System](./apex_namespace_System.md)

## Queueable Methods
### execute(context)
Executes the queueable job.

## Queueable Example Implementation
### Testing Queueable Jobs
This section describes how to test queueable jobs.
`)
	writeDoc(t, filepath.Join(root, "apex_methods_system_bare.md"), `# Bare Class

## Namespace
[System](./apex_namespace_System.md)

## Bare Methods
### trim
Trims the value.
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.TotalMembers != 2 {
		t.Fatalf("documents = %#v", inv.Documents)
	}
	signatures := map[string]bool{}
	for _, doc := range inv.Documents {
		for _, member := range doc.Members {
			signatures[member.Signature] = true
		}
	}
	for _, want := range []string{"execute(context)", "trim"} {
		if !signatures[want] {
			t.Fatalf("missing member %q in %#v", want, inv.Documents)
		}
	}
	if signatures["Testing Queueable Jobs"] {
		t.Fatalf("narrative heading was emitted as member: %#v", inv.Documents)
	}
}

func TestBuildInventoryParsesProductNamespaceShapes(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_class_Metadata_ConsoleComponent.md"), `# ConsoleComponent Class

## Namespace
[Metadata](./apex_namespace_Metadata.md)

## ConsoleComponent Properties
### label
The component label.
`)
	writeDoc(t, filepath.Join(root, "apex_connectapi_output_ChatterConversationPage.md"), `# ChatterConversationPage

## Properties
### conversations
Page values.
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Documents) != 2 {
		t.Fatalf("documents = %d", len(inv.Documents))
	}
	if inv.Documents[0].Namespace != "Metadata" || inv.Documents[0].Kind != "class" {
		t.Fatalf("metadata doc = %#v", inv.Documents[0])
	}
	if inv.Documents[1].Namespace != "ConnectApi" || inv.Documents[1].Kind != "output" {
		t.Fatalf("connectapi doc = %#v", inv.Documents[1])
	}
}

func TestBuildInventoryParsesDocsContractTypes(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_connectapi_output_ManagedContentVersionCollection.md"), `# ManagedContentVersionCollection

## Properties

| Property Name | Type | Available Version |
| --- | --- | --- |
| items | List<[`+"`"+`ConnectApi.ManagedContentVersion`+"`"+`](./apex_connectapi_output_ManagedContentVersion.md)> | 49.0 |
| managedContentTypes | Map<[String](./apex_methods_system_string.md), [`+"`"+`ConnectApi.ManagedContentType`+"`"+`](./apex_connectapi_output_ManagedContentType.md)> | 49.0 |
`)
	writeDoc(t, filepath.Join(root, "apex_methods_system_object.md"), `# Object Class

## Object Methods

### equals(obj)
Returns true if the two values are equal.

#### Signature

`+"```apex"+`
public Boolean equals(Object obj)
`+"```"+`
`)
	writeDoc(t, filepath.Join(root, "apex_methods_system_list.md"), `# List Class

## List Constructors

### List(setToCopy)
Creates a list from a set.

#### Signature

`+"```apex"+`
public List<T>(Set<T> setToCopy)
`+"```"+`
`)
	writeDoc(t, filepath.Join(root, "apex_class_System_CustomSettings.md"), `# CustomSettings Class

## Namespace
[System](./apex_namespace_System.md)

## CustomSettings Methods

### getAll()
Returns all settings.

#### Signature

`+"```apex"+`
public Map<String, CustomSetting__c> getAll()
`+"```"+`
`)
	writeDoc(t, filepath.Join(root, "apex_class_RichMessaging_CurrencyAmount.md"), `# CurrencyAmount Class

## Namespace
[RichMessaging](./apex_namespace_RichMessaging.md)

## CurrencyAmount Properties

### currency
The ISO currency code.

#### Signature

`+"```apex"+`
public String currency {get; set;}
`+"```"+`
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	members := map[string]Member{}
	for _, doc := range inv.Documents {
		for _, member := range doc.Members {
			members[doc.Name+"."+member.Name] = member
		}
	}
	if got := members["ManagedContentVersionCollection.items"].PropertyType; got != "List<ConnectApi.ManagedContentVersion>" {
		t.Fatalf("items property type = %q", got)
	}
	if got := members["ManagedContentVersionCollection.items"].APIVersion; got != "49.0" {
		t.Fatalf("items API version = %q", got)
	}
	for _, doc := range inv.Documents {
		if doc.Name == "ManagedContentVersionCollection" && doc.APIVersion != "49.0" {
			t.Fatalf("ManagedContentVersionCollection API version = %q", doc.APIVersion)
		}
	}
	if got := members["ManagedContentVersionCollection.managedContentTypes"].PropertyType; got != "Map<String,ConnectApi.ManagedContentType>" {
		t.Fatalf("managedContentTypes property type = %q", got)
	}
	if got := members["Object.equals"].ReturnType; got != "Boolean" {
		t.Fatalf("equals return type = %q", got)
	}
	if got := members["Object.equals"].Parameters; len(got) != 1 || got[0] != "Object" {
		t.Fatalf("equals parameters = %#v", got)
	}
	if got := members["List.List"].Parameters; len(got) != 1 || got[0] != "Set<T>" {
		t.Fatalf("List constructor parameters = %#v", got)
	}
	if got := members["CustomSettings.getAll"].ReturnType; got != "Map<String,CustomSetting__c>" {
		t.Fatalf("getAll return type = %q", got)
	}
	if got := members["CurrencyAmount.currency"].PropertyType; got != "String" {
		t.Fatalf("currency property type = %q", got)
	}
}

func TestBuildInventoryDoesNotTreatParameterTablesAsProperties(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_methods_system_object.md"), `# Object Class

## Object Methods

### equals(obj)
Returns true if the two values are equal.

#### Signature

`+"```apex"+`
public Boolean equals(Object obj)
`+"```"+`

#### Parameters

| Name | Type | Description |
| --- | --- | --- |
| obj | Object | The value to compare. |
`)

	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.TotalMembers != 1 {
		t.Fatalf("members = %#v", inv.Documents[0].Members)
	}
	member := inv.Documents[0].Members[0]
	if member.Name != "equals" || member.Kind != "method" {
		t.Fatalf("member = %#v", member)
	}
}

func TestWriteJSONIsStable(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, filepath.Join(root, "apex_methods_system_list.md"), `# List Class

## List Methods
### add(listElement)
Adds an element.
`)
	inv, err := BuildInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := WriteJSON(&out, inv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"sourcePath": "apex_methods_system_list.md"`) {
		t.Fatalf("json = %q", out.String())
	}
	var decoded Inventory
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Documents[0].Members[0].Name != "add" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestDiffInventories(t *testing.T) {
	oldInv := Inventory{Documents: []Document{{
		SourcePath: "a.md",
		Kind:       "class",
		Namespace:  "System",
		Name:       "A",
		Title:      "A Class",
		Members:    []Member{{Kind: "method", Name: "one", Signature: "one()"}},
	}}}
	newInv := Inventory{Documents: []Document{{
		SourcePath: "a.md",
		Kind:       "class",
		Namespace:  "System",
		Name:       "A",
		Title:      "A Class",
		Members:    []Member{{Kind: "method", Name: "two", Signature: "two()"}},
	}, {
		SourcePath: "b.md",
		Kind:       "class",
		Name:       "B",
	}}}
	diff := DiffInventories(oldInv, newInv)
	if len(diff.AddedDocuments) != 1 || diff.AddedDocuments[0] != "b.md" {
		t.Fatalf("added docs = %#v", diff.AddedDocuments)
	}
	if len(diff.ChangedDocuments) != 1 {
		t.Fatalf("changed docs = %#v", diff.ChangedDocuments)
	}
	changed := diff.ChangedDocuments[0]
	if changed.AddedMembers[0] != "method|two|two()" || changed.RemovedMembers[0] != "method|one|one()" {
		t.Fatalf("changed = %#v", changed)
	}
}

func TestCanonicalDigestRepeatedIsIdentical(t *testing.T) {
	inv := Inventory{
		SchemaVersion: 1,
		TotalFiles:    1,
		TotalMembers:  1,
		Documents: []Document{{
			SourcePath: "apex_methods_system_string.md",
			Kind:       "class",
			Namespace:  "System",
			Name:       "String",
			Members: []Member{{
				Kind:      "method",
				Name:      "trim",
				Signature: "trim()",
			}},
		}},
	}

	first := CanonicalDigest(inv)
	second := CanonicalDigest(inv)
	third := CanonicalDigest(inv)

	if first != second || second != third {
		t.Fatalf("digests differ across repeated calls: %s %s %s", first, second, third)
	}
	if first == "" {
		t.Fatal("digest is empty")
	}
}

func TestFirstParagraphTruncationKeepsValidUTF8(t *testing.T) {
	got := firstParagraph([]string{strings.Repeat("x", 399) + "’"})
	if !strings.Contains(got, strings.Repeat("x", 399)) || !utf8.ValidString(got) {
		t.Fatalf("firstParagraph returned invalid UTF-8: %x", []byte(got))
	}
}

func TestCanonicalDigestDocumentOrderInvariant(t *testing.T) {
	invAB := Inventory{
		SchemaVersion: 1,
		TotalFiles:    2,
		Documents: []Document{
			{SourcePath: "a.md", Kind: "class", Name: "A", Members: []Member{{Kind: "method", Name: "one", Signature: "one()"}}},
			{SourcePath: "b.md", Kind: "class", Name: "B", Members: []Member{{Kind: "method", Name: "two", Signature: "two()"}}},
		},
	}
	invBA := Inventory{
		SchemaVersion: 1,
		TotalFiles:    2,
		Documents: []Document{
			{SourcePath: "b.md", Kind: "class", Name: "B", Members: []Member{{Kind: "method", Name: "two", Signature: "two()"}}},
			{SourcePath: "a.md", Kind: "class", Name: "A", Members: []Member{{Kind: "method", Name: "one", Signature: "one()"}}},
		},
	}

	dAB := CanonicalDigest(invAB)
	dBA := CanonicalDigest(invBA)

	if dAB != dBA {
		t.Fatalf("document order changed digest: %s vs %s", dAB, dBA)
	}
}

func TestCanonicalDigestMemberOrderInvariant(t *testing.T) {
	invXY := Inventory{
		SchemaVersion: 1,
		TotalFiles:    1,
		Documents: []Document{{
			SourcePath: "a.md",
			Kind:       "class",
			Name:       "A",
			Members: []Member{
				{Kind: "method", Name: "one", Signature: "one()"},
				{Kind: "method", Name: "two", Signature: "two()"},
			},
		}},
	}
	invYX := Inventory{
		SchemaVersion: 1,
		TotalFiles:    1,
		Documents: []Document{{
			SourcePath: "a.md",
			Kind:       "class",
			Name:       "A",
			Members: []Member{
				{Kind: "method", Name: "two", Signature: "two()"},
				{Kind: "method", Name: "one", Signature: "one()"},
			},
		}},
	}

	dXY := CanonicalDigest(invXY)
	dYX := CanonicalDigest(invYX)

	if dXY != dYX {
		t.Fatalf("member order changed digest: %s vs %s", dXY, dYX)
	}
}

func TestCanonicalDigestSameSizeSignatureChangeChangesDigest(t *testing.T) {
	invA := Inventory{
		SchemaVersion: 1,
		Documents: []Document{{
			SourcePath: "a.md",
			Kind:       "class",
			Name:       "A",
			Members:    []Member{{Kind: "method", Name: "abc", Signature: "abc()"}},
		}},
	}
	invB := Inventory{
		SchemaVersion: 1,
		Documents: []Document{{
			SourcePath: "a.md",
			Kind:       "class",
			Name:       "A",
			Members:    []Member{{Kind: "method", Name: "xyz", Signature: "xyz()"}},
		}},
	}

	// Both have same byte size for name/signature but different values.
	if len(invA.Documents[0].Members[0].Signature) != len(invB.Documents[0].Members[0].Signature) {
		t.Skip("signatures are not same-length; this test requires same-length strings")
	}
	if len(invA.Documents[0].Members[0].Name) != len(invB.Documents[0].Members[0].Name) {
		t.Skip("names are not same-length; this test requires same-length strings")
	}

	dA := CanonicalDigest(invA)
	dB := CanonicalDigest(invB)

	if dA == dB {
		t.Fatalf("same-size member change did not change digest: both %s", dA)
	}
}

func TestCanonicalDigestIncludesDescriptionsAndBehaviors(t *testing.T) {
	base := Inventory{
		SchemaVersion: 1,
		Documents: []Document{{
			SourcePath: "a.md",
			Kind:       "class",
			Name:       "A",
			Members:    []Member{{Kind: "method", Name: "one", Signature: "one()", Description: "First method."}},
			Behaviors:  []DocBehavior{{Kind: BehaviorDeprecated, Evidence: "deprecated"}},
		}},
	}

	descChanged := deepCopyInventory(base)
	descChanged.Documents[0].Members[0].Description = "Something entirely different now."

	behavChanged := deepCopyInventory(base)
	behavChanged.Documents[0].Behaviors = []DocBehavior{{Kind: BehaviorCalloutInTest, Evidence: "treated as a callout"}}

	dBase := CanonicalDigest(base)
	dDesc := CanonicalDigest(descChanged)
	dBehav := CanonicalDigest(behavChanged)

	if dBase == dDesc {
		t.Fatal("description change did not change digest")
	}
	if dBase == dBehav {
		t.Fatal("behavior constraint change did not change digest")
	}
	if dDesc == dBehav {
		t.Fatal("description and behavior changes produced identical digest")
	}
}

func TestCanonicalDigestDoesNotMutateCaller(t *testing.T) {
	inv := Inventory{
		SchemaVersion: 1,
		Documents: []Document{{
			SourcePath: "z.md",
			Kind:       "class",
			Name:       "Z",
			Members: []Member{
				{Kind: "method", Name: "second", Signature: "second()"},
				{Kind: "method", Name: "first", Signature: "first()"},
			},
		}},
	}

	// Capture the original document/member order before hashing.
	origDocOrder := make([]string, len(inv.Documents))
	copy(origDocOrder, docPaths(inv))
	origMemberOrder := make([]string, len(inv.Documents[0].Members))
	copy(origMemberOrder, memberNames(inv.Documents[0]))

	_ = CanonicalDigest(inv)

	// Verify document order unchanged.
	for i, path := range docPaths(inv) {
		if path != origDocOrder[i] {
			t.Fatalf("document order mutated at %d: was %s, now %s", i, origDocOrder[i], path)
		}
	}
	// Verify member order unchanged.
	for i, name := range memberNames(inv.Documents[0]) {
		if name != origMemberOrder[i] {
			t.Fatalf("member order mutated at %d: was %s, now %s", i, origMemberOrder[i], name)
		}
	}
}

func docPaths(inv Inventory) []string {
	out := make([]string, len(inv.Documents))
	for i, d := range inv.Documents {
		out[i] = d.SourcePath
	}
	return out
}

func memberNames(doc Document) []string {
	out := make([]string, len(doc.Members))
	for i, m := range doc.Members {
		out[i] = m.Name
	}
	return out
}

func TestReleaseManifestFields(t *testing.T) {
	m := NewReleaseManifest(
		"Summer '26",
		"66.0",
		"abc123def456",
		"glade-tools v1.2.3",
		[]string{"apex-reference", "connectapi-reference"},
	)

	if m.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", m.SchemaVersion)
	}
	if m.Release != "Summer '26" {
		t.Fatalf("release = %q", m.Release)
	}
	if m.APIVersion != "66.0" {
		t.Fatalf("apiVersion = %q", m.APIVersion)
	}
	if m.Digest != "abc123def456" {
		t.Fatalf("digest = %q", m.Digest)
	}
	if m.Acquisition != "glade-tools v1.2.3" {
		t.Fatalf("acquisition = %q", m.Acquisition)
	}
	if len(m.SourceFamilies) != 2 || m.SourceFamilies[0] != "apex-reference" || m.SourceFamilies[1] != "connectapi-reference" {
		t.Fatalf("sourceFamilies = %#v", m.SourceFamilies)
	}

	// Round-trip through JSON.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(m); err != nil {
		t.Fatal(err)
	}
	var decoded ReleaseManifest
	if err := json.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Release != m.Release || decoded.APIVersion != m.APIVersion {
		t.Fatalf("roundtrip = %#v", decoded)
	}
}

func writeDoc(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
