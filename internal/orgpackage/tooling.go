package orgpackage

import (
	"context"
	"strings"
)

func CaptureApexClasses(ctx context.Context, client Client, namespace string) ([]ApexClassContract, error) {
	soql := "SELECT Id, Name, NamespacePrefix, SymbolTable, ManageableState FROM ApexClass WHERE NamespacePrefix = '" + strings.ReplaceAll(namespace, "'", "\\'") + "' AND Status = 'Active'"
	var result queryResult[apexClassRow]
	if err := client.ToolingQuery(ctx, soql, &result); err != nil {
		return nil, err
	}
	out := make([]ApexClassContract, 0, len(result.Records))
	for _, row := range result.Records {
		for _, contract := range contractsFromSymbolTable(row.Name, row.NamespacePrefix, row.SymbolTable) {
			if visibleContract(contract.Visibility) {
				out = append(out, contract)
			}
		}
	}
	return out, nil
}

type apexClassRow struct {
	ID              string         `json:"Id"`
	Name            string         `json:"Name"`
	NamespacePrefix string         `json:"NamespacePrefix"`
	SymbolTable     rawSymbolTable `json:"SymbolTable"`
	ManageableState string         `json:"ManageableState"`
}

type rawSymbolTable struct {
	TableDeclaration rawSymbol        `json:"tableDeclaration"`
	Methods          []rawMethod      `json:"methods"`
	Constructors     []rawMethod      `json:"constructors"`
	Properties       []rawTypedSymbol `json:"properties"`
	InnerClasses     []rawSymbolTable `json:"innerClasses"`
}

type rawSymbol struct {
	Name        string   `json:"name"`
	Visibility  string   `json:"visibility"`
	Modifiers   []string `json:"modifiers"`
	Annotations []string `json:"annotations"`
	Interfaces  []string `json:"interfaces"`
	SuperClass  string   `json:"superClass"`
}

type rawTypedSymbol struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Visibility  string   `json:"visibility"`
	Modifiers   []string `json:"modifiers"`
	Annotations []string `json:"annotations"`
}

type rawMethod struct {
	Name        string         `json:"name"`
	ReturnType  string         `json:"returnType"`
	Visibility  string         `json:"visibility"`
	Modifiers   []string       `json:"modifiers"`
	Annotations []string       `json:"annotations"`
	Parameters  []rawParameter `json:"parameters"`
}

type rawParameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func contractsFromSymbolTable(fallbackName, namespace string, table rawSymbolTable) []ApexClassContract {
	contract := classContractFromTable(fallbackName, namespace, table)
	if contract.Name == "" {
		return nil
	}
	out := []ApexClassContract{contract}
	for _, inner := range table.InnerClasses {
		for _, child := range contractsFromSymbolTable("", namespace, inner) {
			child.Name = contract.Name + "." + child.Name
			out = append(out, child)
		}
	}
	return out
}

func classContractFromTable(fallbackName, namespace string, table rawSymbolTable) ApexClassContract {
	decl := table.TableDeclaration
	name := strings.TrimSpace(decl.Name)
	if name == "" {
		name = strings.TrimSpace(fallbackName)
	}
	return ApexClassContract{
		Name:         name,
		Namespace:    namespace,
		Visibility:   visibilityFromRaw(decl.Visibility, decl.Annotations),
		Abstract:     hasModifier(decl.Modifiers, "abstract"),
		Interface:    hasModifier(decl.Modifiers, "interface"),
		Enum:         hasModifier(decl.Modifiers, "enum"),
		SuperClass:   strings.TrimSpace(decl.SuperClass),
		Interfaces:   sortedUniqueStrings(decl.Interfaces),
		Methods:      methodsFromRaw(table.Methods),
		Constructors: methodsFromRaw(table.Constructors),
		Properties:   propertiesFromRaw(table.Properties),
	}
}

func methodsFromRaw(rows []rawMethod) []ApexMethodContract {
	out := make([]ApexMethodContract, 0, len(rows))
	for _, row := range rows {
		params := make([]ApexParameterContract, 0, len(row.Parameters))
		for _, param := range row.Parameters {
			params = append(params, ApexParameterContract{Name: param.Name, Type: param.Type})
		}
		out = append(out, ApexMethodContract{
			Name:       row.Name,
			ReturnType: row.ReturnType,
			Visibility: visibilityFromRaw(row.Visibility, row.Annotations),
			Static:     hasModifier(row.Modifiers, "static"),
			Abstract:   hasModifier(row.Modifiers, "abstract"),
			Parameters: params,
		})
	}
	return out
}

func propertiesFromRaw(rows []rawTypedSymbol) []ApexPropertyContract {
	out := make([]ApexPropertyContract, 0, len(rows))
	for _, row := range rows {
		out = append(out, ApexPropertyContract{
			Name:       row.Name,
			Type:       row.Type,
			Visibility: visibilityFromRaw(row.Visibility, row.Annotations),
			Static:     hasModifier(row.Modifiers, "static"),
		})
	}
	return out
}

func visibilityFromRaw(visibility string, annotations []string) string {
	if hasAnnotation(annotations, "NamespaceAccessible") {
		return "namespaceaccessible"
	}
	return strings.ToLower(strings.TrimSpace(visibility))
}

func hasModifier(modifiers []string, want string) bool {
	for _, modifier := range modifiers {
		if strings.EqualFold(modifier, want) {
			return true
		}
	}
	return false
}

func hasAnnotation(annotations []string, want string) bool {
	for _, annotation := range annotations {
		if strings.EqualFold(strings.TrimPrefix(annotation, "@"), want) {
			return true
		}
	}
	return false
}
