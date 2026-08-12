package surfaceledger

import (
	"testing"

	"github.com/glade-sh/glade/tools/internal/capability"
)

func TestRowsFromToolingCompletions(t *testing.T) {
	rows := RowsFromToolingCompletions(capability.ToolingCompletions{
		PublicDeclarations: map[string]map[string]capability.ToolingClassDecl{
			"System": {
				"Label": {
					Methods: []capability.ToolingMethod{{
						Name:       "get",
						ReturnType: "String",
						Parameters: []capability.ToolingParameter{
							{Name: "section", Type: "String"},
							{Name: "key", Type: "String"},
						},
					}},
				},
			},
		},
	})
	byID := rowsByID(rows)
	id := ApexMemberID("System", "Label", "get", []string{"String", "String"})
	if byID[id].Org != SourcePresent {
		t.Fatalf("org state for %s = %q", id, byID[id].Org)
	}
	if byID[id].ReturnType != "String" {
		t.Fatalf("return type = %q", byID[id].ReturnType)
	}
}

func TestRowsFromToolingCompletionsNormalizesApexObjectParameters(t *testing.T) {
	rows := RowsFromToolingCompletions(capability.ToolingCompletions{
		PublicDeclarations: map[string]map[string]capability.ToolingClassDecl{
			"System": {
				"Database": {
					Methods: []capability.ToolingMethod{{
						Name:       "executeBatch",
						ReturnType: "ID",
						Parameters: []capability.ToolingParameter{
							{Name: "batchClassObject", Type: "APEX_OBJECT"},
							{Name: "scope", Type: "Integer"},
						},
					}},
				},
			},
		},
	})

	id := ApexMemberID("System", "Database", "executeBatch", []string{"Object", "Integer"})
	if rowsByID(rows)[id].Org != SourcePresent {
		t.Fatalf("Tooling APEX_OBJECT parameter did not normalize to %s: %#v", id, rows)
	}
}

func TestRowsFromToolingCompletionsNormalizesGenericAnyParameters(t *testing.T) {
	rows := RowsFromToolingCompletions(capability.ToolingCompletions{
		PublicDeclarations: map[string]map[string]capability.ToolingClassDecl{
			"Database": {
				"Batchable": {
					Methods: []capability.ToolingMethod{{
						Name:       "execute",
						ReturnType: "void",
						Parameters: []capability.ToolingParameter{
							{Name: "context", Type: "Database.BatchableContext"},
							{Name: "scope", Type: "List<ANY>"},
						},
					}},
				},
			},
		},
	})

	id := ApexMemberID("Database", "Batchable", "execute", []string{"Database.BatchableContext", "List<Object>"})
	if rowsByID(rows)[id].Org != SourcePresent {
		t.Fatalf("Tooling List<ANY> parameter did not normalize to %s: %#v", id, rows)
	}
}

func TestRowsFromToolingCompletionsCanonicalizesFlattenedInvocableActionDTOOwners(t *testing.T) {
	flattenedOwners := []string{
		"AdditionalAttribute",
		"DescribeResult",
		"GenericType",
		"InputParameter",
		"OutputParameter",
		"PicklistValue",
	}
	invocable := make(map[string]capability.ToolingClassDecl, len(flattenedOwners)+1)
	for _, typeName := range flattenedOwners {
		invocable[typeName] = capability.ToolingClassDecl{}
	}
	invocable["AdditionalAttribute"] = capability.ToolingClassDecl{Methods: []capability.ToolingMethod{{
		Name:       "clone",
		ReturnType: "Object",
	}}}
	invocable["DescribeResult"] = capability.ToolingClassDecl{Methods: []capability.ToolingMethod{
		{Name: "getGenericTypes", ReturnType: "List<Invocable.Action.GenericType>"},
		{Name: "getInputs", ReturnType: "List<Invocable.Action.InputParameter>"},
	}}
	invocable["GenericType"] = capability.ToolingClassDecl{Methods: []capability.ToolingMethod{{
		Name:       "getSelf",
		ReturnType: "Invocable.Action.GenericType",
	}}}
	invocable["InputParameter"] = capability.ToolingClassDecl{Methods: []capability.ToolingMethod{
		{Name: "getAdditionalAttributes", ReturnType: "List<Invocable.Action.AdditionalAttribute>"},
		{Name: "getPicklistValues", ReturnType: "List<Invocable.Action.PicklistValue>"},
	}}
	invocable["OutputParameter"] = capability.ToolingClassDecl{Methods: []capability.ToolingMethod{
		{Name: "getAdditionalAttributes", ReturnType: "List<Invocable.Action.AdditionalAttribute>"},
		{Name: "getPicklistValues", ReturnType: "List<Invocable.Action.PicklistValue>"},
	}}
	invocable["PicklistValue"] = capability.ToolingClassDecl{Methods: []capability.ToolingMethod{{
		Name:       "getSelf",
		ReturnType: "Invocable.Action.PicklistValue",
	}}}
	invocable["Result"] = capability.ToolingClassDecl{Methods: []capability.ToolingMethod{{
		Name:       "getAction",
		ReturnType: "Invocable.Action",
	}}, Properties: []capability.ToolingProperty{{
		Name: "action",
		Type: "Invocable.Action",
	}}}

	rows := RowsFromToolingCompletions(capability.ToolingCompletions{
		PublicDeclarations: map[string]map[string]capability.ToolingClassDecl{
			"Invocable": invocable,
			"Process": {
				"InputParameter": {Methods: []capability.ToolingMethod{{
					Name:       "clone",
					ReturnType: "Object",
				}}},
			},
		},
	})
	byID := rowsByID(rows)

	for _, typeName := range flattenedOwners {
		qualifiedID := ApexTypeID("Invocable.Action", typeName)
		if byID[qualifiedID].Org != SourcePresent {
			t.Fatalf("missing qualified Tooling row %s: %#v", qualifiedID, rows)
		}
		unqualifiedID := ApexTypeID("Invocable", typeName)
		if _, ok := byID[unqualifiedID]; ok {
			t.Fatalf("retained flattened orphan Tooling row %s", unqualifiedID)
		}
	}

	wantReturns := map[string]string{
		ApexMemberID("Invocable.Action", "DescribeResult", "getGenericTypes", []string{}):          "List<Invocable.Action.GenericType>",
		ApexMemberID("Invocable.Action", "DescribeResult", "getInputs", []string{}):                "List<Invocable.Action.InputParameter>",
		ApexMemberID("Invocable.Action", "GenericType", "getSelf", []string{}):                     "Invocable.Action.GenericType",
		ApexMemberID("Invocable.Action", "InputParameter", "getAdditionalAttributes", []string{}):  "List<Invocable.Action.AdditionalAttribute>",
		ApexMemberID("Invocable.Action", "InputParameter", "getPicklistValues", []string{}):        "List<Invocable.Action.PicklistValue>",
		ApexMemberID("Invocable.Action", "OutputParameter", "getAdditionalAttributes", []string{}): "List<Invocable.Action.AdditionalAttribute>",
		ApexMemberID("Invocable.Action", "OutputParameter", "getPicklistValues", []string{}):       "List<Invocable.Action.PicklistValue>",
		ApexMemberID("Invocable.Action", "PicklistValue", "getSelf", []string{}):                   "Invocable.Action.PicklistValue",
	}
	for id, want := range wantReturns {
		if got := byID[id].ReturnType; got != want {
			t.Errorf("return type for %s = %q, want %q", id, got, want)
		}
	}
	cloneID := ApexMemberID("Invocable.Action", "AdditionalAttribute", "clone", []string{})
	if byID[cloneID].Org != SourcePresent || byID[cloneID].ReturnType != "Object" {
		t.Fatalf("canonicalized clone row changed: %#v", byID[cloneID])
	}

	resultID := ApexMemberID("Invocable", "Result", "getAction", []string{})
	if byID[resultID].Org != SourcePresent || byID[resultID].ReturnType != "Invocable.Action" {
		t.Fatalf("unrelated Invocable.Result row changed: %#v", byID[resultID])
	}
	propertyID := ApexMemberID("Invocable", "Result", "action", nil)
	if byID[propertyID].Org != SourcePresent || byID[propertyID].ReturnType != "Invocable.Action" {
		t.Fatalf("unrelated Invocable.Result property changed: %#v", byID[propertyID])
	}
	processID := ApexTypeID("Process", "InputParameter")
	if byID[processID].Org != SourcePresent {
		t.Fatalf("unrelated Process.InputParameter row changed: %#v", byID[processID])
	}
}

func TestCanonicalParameterTypeNormalizesSObjectGenericParameters(t *testing.T) {
	got := ApexMemberID("Database", "Batchable", "execute", []string{"Database.BatchableContext", "List<sObject>"})
	want := ApexMemberID("Database", "Batchable", "execute", []string{"Database.BatchableContext", "List<Object>"})
	if got != want {
		t.Fatalf("List<sObject> canonical id = %q, want %q", got, want)
	}
}

func TestDecodeToolingCompletionsAcceptsWrappedResult(t *testing.T) {
	completions, err := decodeToolingCompletions([]byte(`{"result":{"publicDeclarations":{"System":{"Label":{}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := completions.PublicDeclarations["System"]["Label"]; !ok {
		t.Fatalf("wrapped completions not decoded: %#v", completions)
	}
}
