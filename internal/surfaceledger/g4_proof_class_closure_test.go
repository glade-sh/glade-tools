package surfaceledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var g4ProofClassFixtureRows = map[string][]string{
	"g4-auth-jwt-local-evidence.json": {
		"apex:Auth.JWT",
		"apex:Auth.JWT.getAdditionalClaims()",
		"apex:Auth.JWT.getAud()",
		"apex:Auth.JWT.getIss()",
		"apex:Auth.JWT.getNbfClockSkew()",
		"apex:Auth.JWT.getSub()",
		"apex:Auth.JWT.getValidityLength()",
		"apex:Auth.JWT.setAdditionalClaims(Map<String,Object>)",
		"apex:Auth.JWT.setAud(String)",
		"apex:Auth.JWT.setIss(String)",
		"apex:Auth.JWT.setNbfClockSkew(Integer)",
		"apex:Auth.JWT.setSub(String)",
		"apex:Auth.JWT.setValidityLength(Integer)",
		"apex:Auth.JWT.toJSONString()",
		"apex:Auth.JWTUtil.parseJWTFromStringWithoutValidation(String)",
	},
	"g4-cache-deterministic-evidence.json": {
		"apex:Cache.Org.remove(Type,String)",
		"apex:Cache.OrgPartition.createFullyQualifiedKey(String,String,String)",
		"apex:Cache.OrgPartition.createFullyQualifiedPartition(String,String)",
		"apex:Cache.OrgPartition.validateKey(Boolean,String)",
		"apex:Cache.OrgPartition.validateKeyValue(Boolean,String,Object)",
		"apex:Cache.OrgPartition.validatePartitionName(String)",
		"apex:Cache.Partition.createFullyQualifiedKey(String,String,String)",
		"apex:Cache.Partition.createFullyQualifiedPartition(String,String)",
		"apex:Cache.Partition.validateKey(Boolean,String)",
		"apex:Cache.Partition.validateKeyValue(Boolean,String,Object)",
		"apex:Cache.Partition.validatePartitionName(String)",
		"apex:Cache.Session.get(Type,String)",
		"apex:Cache.Session.remove(Type,String)",
		"apex:Cache.SessionPartition.createFullyQualifiedKey(String,String,String)",
		"apex:Cache.SessionPartition.createFullyQualifiedPartition(String,String)",
		"apex:Cache.SessionPartition.validateKey(Boolean,String)",
		"apex:Cache.SessionPartition.validateKeyValue(Boolean,String,Object)",
		"apex:Cache.SessionPartition.validatePartitionName(String)",
	},
	"current-base-deterministic-mock-required-messaging-002-api67.json": {
		"apex:Messaging.SingleEmailMessage.customHeaders",
		"apex:Messaging.SingleEmailMessage.getCustomHeaders()",
	},
	"core-runtime-local-metadata-search-evidence.json": {
		"apex:Schema.DescribeDataCategoryGroupResult",
		"apex:Schema.DescribeDataCategoryGroupStructureResult",
	},
	"data-platform-database-convertlead-overloads.json": {
		"apex:System.Database.convertLead(leadToConvert,accessLevel)",
		"apex:System.Database.convertLead(leadsToConvert,accessLevel)",
	},
	"g4-apexpages-controller-evidence.json": {
		"apex:ApexPages.StandardSetController.setPageSize(Integer)",
	},
	"core-runtime-apexpages-controller-wave17-runtime.json": {
		"apex:ApexPages.KnowledgeArticleVersionStandardController",
		"apex:ApexPages.KnowledgeArticleVersionStandardController.hashCode()",
		"apex:ApexPages.KnowledgeArticleVersionStandardController.toString()",
		"apex:ApexPages.StandardController.addFields(List<String>)",
		"apex:ApexPages.StandardController.reset()",
		"apex:ApexPages.StandardSetController.addFields(List<String>)",
		"apex:ApexPages.StandardSetController.hashCode()",
		"apex:ApexPages.StandardSetController.toString()",
	},
}

func TestG4ProofClassRowsHaveOneExecutableFixtureOwner(t *testing.T) {
	fixtureRoot := filepath.Join("..", "..", "docs", "fixtures")
	paths := make([]string, 0, len(g4ProofClassFixtureRows))
	for name := range g4ProofClassFixtureRows {
		paths = append(paths, filepath.Join(fixtureRoot, name))
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int, len(evidence))
	for _, row := range evidence {
		counts[row.SurfaceID]++
	}
	for fixture, ids := range g4ProofClassFixtureRows {
		for _, id := range ids {
			if counts[id] != 1 {
				t.Errorf("%s evidence count from %s = %d, want 1", id, fixture, counts[id])
			}
		}
	}
}

func TestG4ProofClassClosureDropsBareMapContainsValueAlias(t *testing.T) {
	rows := rowsByID(BuildGladeSnapshot())
	if _, ok := rows["apex:System.Map.containsValue"]; ok {
		t.Fatal("bare Map.containsValue alias must not survive beside the exact Object overload")
	}
	exact, ok := rows["apex:System.Map.containsValue(Object)"]
	if !ok {
		t.Fatal("exact Map.containsValue(Object) row disappeared")
	}
	if exact.GladeShape != ShapeSignatureKnown || exact.GladeBehavior != BehaviorSupported {
		t.Fatalf("exact Map.containsValue(Object) = %s/%s, want signature-known/supported", exact.GladeShape, exact.GladeBehavior)
	}
}

func TestG4MessagingCustomHeadersPropertyHasDirectSourceWitness(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", "current-base-deterministic-mock-required-messaging-002-api67.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "single.customHeaders") {
		t.Fatal("SingleEmailMessage.customHeaders evidence requires a direct property access")
	}
}
