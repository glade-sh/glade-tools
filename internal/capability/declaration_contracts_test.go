package capability

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
)

func TestBuildDeclarationContractsExportsGeneratedStubShape(t *testing.T) {
	inv := apexdocs.Inventory{
		Documents: []apexdocs.Document{
			{
				SourcePath: "apex/apex_methods_system_string.md",
				Kind:       "class",
				Namespace:  "System",
				Name:       "String",
				Members: []apexdocs.Member{
					{
						Kind:       "method",
						Name:       "format",
						Signature:  "public static String format(String stringToFormat, List<Object> formattingArguments)",
						ReturnType: "String",
						Parameters: []string{"String", "List<Object>"},
					},
					{
						Kind:       "method",
						Name:       "trim",
						Signature:  "public String trim()",
						ReturnType: "String",
					},
				},
			},
			{
				SourcePath: "lwc/reference-api-modules.md",
				Kind:       "document",
				Name:       "reference-api-modules",
			},
			{
				SourcePath: "apex/apex_ConnectAPI_ChatterFeeds_static_methods.md",
				Kind:       "document",
				Namespace:  "ConnectApi",
				Name:       "ChatterFeeds",
				Members: []apexdocs.Member{
					{
						Kind:      "method",
						Name:      "getFeed",
						Signature: "getFeed(communityId, feedType)",
						Section:   "ChatterFeeds Methods",
					},
				},
			},
		},
	}

	contracts := BuildDeclarationContracts(inv)
	if contracts.SchemaVersion != 1 {
		t.Fatalf("schema version = %d", contracts.SchemaVersion)
	}
	if len(contracts.Documents) != 2 {
		t.Fatalf("documents = %#v", contracts.Documents)
	}
	doc := contracts.Documents[1]
	if doc.SourcePath != "apex/apex_methods_system_string.md" || doc.Kind != "class" || doc.Namespace != "System" || doc.Name != "String" {
		t.Fatalf("doc = %#v", doc)
	}
	if len(doc.Members) != 2 {
		t.Fatalf("members = %#v", doc.Members)
	}
	if !doc.Members[0].Static || doc.Members[0].ReturnType != "String" || len(doc.Members[0].Parameters) != 2 {
		t.Fatalf("format member = %#v", doc.Members[0])
	}
	if doc.Members[1].Static {
		t.Fatalf("trim should be instance method: %#v", doc.Members[1])
	}
	chatter := contracts.Documents[0]
	if chatter.SourcePath != "apex/apex_ConnectAPI_ChatterFeeds_static_methods.md" || !chatter.Members[0].Static {
		t.Fatalf("chatter doc = %#v", chatter)
	}
}

func TestWriteDeclarationContractsJSONUsesGeneratorSchema(t *testing.T) {
	report := DeclarationContracts{
		SchemaVersion: 1,
		Documents: []DeclarationDocumentContract{
			{
				SourcePath: "apex/apex_class_commercepayments_LineItemInput.md",
				Kind:       "class",
				Namespace:  "CommercePayments",
				Name:       "LineItemInput",
				Members: []DeclarationMemberContract{
					{Kind: "property", Name: "quantity", Signature: "public Double quantity {get; set;}", PropertyType: "Double"},
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := WriteDeclarationContractsJSON(&buf, report); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SchemaVersion int `json:"schemaVersion"`
		Documents     []struct {
			Members []struct {
				PropertyType string `json:"propertyType"`
				Static       bool   `json:"static"`
			} `json:"members"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json did not decode: %v\n%s", err, buf.String())
	}
	if decoded.SchemaVersion != 1 || decoded.Documents[0].Members[0].PropertyType != "Double" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestBuildDeclarationContractsCleansEscapedEnumConstantNames(t *testing.T) {
	inv := apexdocs.Inventory{
		Documents: []apexdocs.Document{
			{
				SourcePath: "apex/apex_class_System_AccessLevel.md",
				Kind:       "class",
				Namespace:  "System",
				Name:       "AccessLevel",
				Members: []apexdocs.Member{
					{
						Kind:      "property",
						Name:      `SYSTEM\_MODE`,
						Signature: "public System.AccessLevel SYSTEM_MODE {get;}",
						Section:   "AccessLevel Properties",
					},
				},
			},
		},
	}

	contracts := BuildDeclarationContracts(inv)
	member := contracts.Documents[0].Members[0]
	if member.Name != "SYSTEM_MODE" {
		t.Fatalf("name = %q", member.Name)
	}
	if member.PropertyType != "System.AccessLevel" {
		t.Fatalf("property type = %q", member.PropertyType)
	}
	if !member.Static {
		t.Fatalf("constant property should be static: %#v", member)
	}
}

func TestBuildDeclarationContractsCleansNamespacedDocumentAndZeroWidthMemberNames(t *testing.T) {
	inv := apexdocs.Inventory{
		Documents: []apexdocs.Document{
			{
				SourcePath: "apex/apex_connectapi_output_managed_content_version_collection.md",
				Kind:       "output",
				Namespace:  "ConnectApi",
				Name:       "ConnectApi.ManagedContentVersionCollection",
				Members: []apexdocs.Member{
					{
						Kind:         "property",
						Name:         "managedContent\u200bTypes",
						Signature:    "managedContent\u200bTypes",
						PropertyType: "Map<String,ConnectApi.ManagedContentType>",
					},
				},
			},
		},
	}

	contracts := BuildDeclarationContracts(inv)
	doc := contracts.Documents[0]
	if doc.Name != "ManagedContentVersionCollection" {
		t.Fatalf("document name = %q", doc.Name)
	}
	if doc.Members[0].Name != "managedContentTypes" {
		t.Fatalf("member name = %q", doc.Members[0].Name)
	}
}
