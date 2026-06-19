package orgpackage

import (
	"context"
	"sort"
	"strings"

	"github.com/glade-sh/glade/internal/orgdescribe"
)

func CaptureObjects(ctx context.Context, client Client, namespace string) ([]orgdescribe.SObject, error) {
	var global struct {
		SObjects []struct {
			Name string `json:"name"`
		} `json:"sobjects"`
	}
	if err := client.Get(ctx, "/services/data/v"+client.apiVersion()+"/sobjects", &global); err != nil {
		return nil, err
	}
	prefix := namespace + "__"
	standardObjectsWithPackageFields, err := queryStandardObjectsWithNamespacedFields(ctx, client, namespace)
	if err != nil && !unsupportedToolingObject(err) {
		return nil, err
	}
	useStandardFieldIndex := err == nil
	selected := make([]orgdescribe.SObject, 0)
	for _, row := range global.SObjects {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if !strings.HasPrefix(name, prefix) && isCustomObjectName(name) {
			continue
		}
		packageObject := strings.HasPrefix(name, prefix)
		if !packageObject && useStandardFieldIndex && !standardObjectsWithPackageFields[name] {
			continue
		}
		var object orgdescribe.SObject
		if err := client.Get(ctx, "/services/data/v"+client.apiVersion()+"/sobjects/"+name+"/describe", &object); err != nil {
			return nil, err
		}
		if packageObject {
			selected = append(selected, sortObject(object))
			continue
		}
		object = trimStandardObjectToNamespacedFields(object, prefix)
		if len(object.Fields) > 0 {
			selected = append(selected, sortObject(object))
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })
	return selected, nil
}

func isCustomObjectName(name string) bool {
	return strings.HasSuffix(name, "__c") || strings.HasSuffix(name, "__mdt") || strings.Contains(name, "__")
}

func CaptureMetadataNames(ctx context.Context, client Client, namespace string) (labels []string, staticResources []string, warnings []string, err error) {
	labels, labelWarnings, err := captureNames(ctx, client, "SELECT Name, NamespacePrefix FROM ExternalString WHERE NamespacePrefix = '"+strings.ReplaceAll(namespace, "'", "\\'")+"'", namespace, "ExternalString")
	if err != nil {
		return nil, nil, nil, err
	}
	staticResources, resourceWarnings, err := captureNames(ctx, client, "SELECT Name, NamespacePrefix FROM StaticResource WHERE NamespacePrefix = '"+strings.ReplaceAll(namespace, "'", "\\'")+"'", namespace, "StaticResource")
	if err != nil {
		return nil, nil, nil, err
	}
	warnings = append(warnings, labelWarnings...)
	warnings = append(warnings, resourceWarnings...)
	return labels, staticResources, warnings, nil
}

func CaptureLightningBundles(ctx context.Context, client Client, namespace string) ([]LightningBundleContract, []string, error) {
	lwc, lwcWarnings, err := captureBundleRows(ctx, client, "SELECT DeveloperName, NamespacePrefix, MasterLabel, IsExposed FROM LightningComponentBundle WHERE NamespacePrefix = '"+strings.ReplaceAll(namespace, "'", "\\'")+"'", "lwc", "LightningComponentBundle")
	if err != nil {
		return nil, nil, err
	}
	aura, auraWarnings, err := captureBundleRows(ctx, client, "SELECT DeveloperName, NamespacePrefix FROM AuraDefinitionBundle WHERE NamespacePrefix = '"+strings.ReplaceAll(namespace, "'", "\\'")+"'", "aura", "AuraDefinitionBundle")
	if err != nil {
		return nil, nil, err
	}
	out := append(lwc, aura...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	warnings := append(lwcWarnings, auraWarnings...)
	return out, warnings, nil
}

type metadataNameRow struct {
	Name            string `json:"Name"`
	NamespacePrefix string `json:"NamespacePrefix"`
}

type bundleRow struct {
	DeveloperName   string `json:"DeveloperName"`
	NamespacePrefix string `json:"NamespacePrefix"`
	MasterLabel     string `json:"MasterLabel"`
	IsExposed       bool   `json:"IsExposed"`
}

type fieldDefinitionRow struct {
	QualifiedAPIName string `json:"QualifiedApiName"`
	EntityDefinition struct {
		QualifiedAPIName string `json:"QualifiedApiName"`
	} `json:"EntityDefinition"`
}

func queryStandardObjectsWithNamespacedFields(ctx context.Context, client Client, namespace string) (map[string]bool, error) {
	escapedNamespace := strings.ReplaceAll(namespace, "'", "\\'")
	var result queryResult[fieldDefinitionRow]
	err := client.ToolingQuery(ctx, "SELECT EntityDefinition.QualifiedApiName, QualifiedApiName FROM FieldDefinition WHERE NamespacePrefix = '"+escapedNamespace+"'", &result)
	if err != nil {
		return nil, err
	}
	objects := map[string]bool{}
	for _, row := range result.Records {
		objectName := strings.TrimSpace(row.EntityDefinition.QualifiedAPIName)
		fieldName := strings.TrimSpace(row.QualifiedAPIName)
		if objectName == "" || fieldName == "" {
			continue
		}
		if isCustomObjectName(objectName) {
			continue
		}
		objects[objectName] = true
	}
	return objects, nil
}

func captureNames(ctx context.Context, client Client, soql string, namespace string, objectName string) ([]string, []string, error) {
	var result queryResult[metadataNameRow]
	if err := client.ToolingQuery(ctx, soql, &result); err != nil {
		if unsupportedToolingObject(err) {
			return nil, []string{"skipped " + objectName + " query: " + err.Error()}, nil
		}
		return nil, nil, err
	}
	names := make([]string, 0, len(result.Records))
	for _, row := range result.Records {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if row.NamespacePrefix != "" && !strings.Contains(name, "__") {
			name = row.NamespacePrefix + "__" + name
		}
		names = append(names, name)
	}
	return sortedUniqueStrings(names), nil, nil
}

func captureBundleRows(ctx context.Context, client Client, soql string, typ string, objectName string) ([]LightningBundleContract, []string, error) {
	var result queryResult[bundleRow]
	if err := client.ToolingQuery(ctx, soql, &result); err != nil {
		if unsupportedToolingObject(err) {
			return nil, []string{"skipped " + objectName + " query: " + err.Error()}, nil
		}
		return nil, nil, err
	}
	out := make([]LightningBundleContract, 0, len(result.Records))
	for _, row := range result.Records {
		out = append(out, LightningBundleContract{
			Namespace: row.NamespacePrefix,
			Name:      row.DeveloperName,
			Type:      typ,
			Exposed:   row.IsExposed,
		})
	}
	return out, nil, nil
}

func unsupportedToolingObject(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "invalid type") || strings.Contains(text, "sobject type")
}

func trimStandardObjectToNamespacedFields(object orgdescribe.SObject, prefix string) orgdescribe.SObject {
	fields := make([]orgdescribe.Field, 0, len(object.Fields))
	keptFieldNames := map[string]bool{}
	for _, field := range object.Fields {
		if strings.HasPrefix(field.Name, prefix) {
			fields = append(fields, field)
			keptFieldNames[field.Name] = true
		}
	}
	relationships := make([]orgdescribe.ChildRelationship, 0, len(object.ChildRelationships))
	for _, relationship := range object.ChildRelationships {
		if keptFieldNames[relationship.Field] || strings.HasPrefix(relationship.ChildSObject, prefix) {
			relationships = append(relationships, relationship)
		}
	}
	object.Fields = fields
	object.ChildRelationships = relationships
	object.RecordTypeInfos = nil
	return object
}

func sortObject(object orgdescribe.SObject) orgdescribe.SObject {
	sort.Slice(object.Fields, func(i, j int) bool { return object.Fields[i].Name < object.Fields[j].Name })
	sort.Slice(object.RecordTypeInfos, func(i, j int) bool {
		return object.RecordTypeInfos[i].DeveloperName < object.RecordTypeInfos[j].DeveloperName
	})
	sort.Slice(object.ChildRelationships, func(i, j int) bool {
		return object.ChildRelationships[i].RelationshipName < object.ChildRelationships[j].RelationshipName
	})
	return object
}
