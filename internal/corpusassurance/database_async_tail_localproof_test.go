package corpusassurance

import "testing"

func TestLocalProofBatchableTestCommandPolicyHasNegativeSiblingGuard(t *testing.T) {
	for _, id := range []string{
		"apex:Database.Batchable.execute(Database.BatchableContext,List<Object>)",
		"apex:Database.Batchable.finish(Database.BatchableContext)",
		"apex:Database.Batchable.start(Database.BatchableContext)",
		"apex:Database.BatchableContext.getChildJobId()",
		"apex:Database.BatchableContext.getJobId()",
		"apex:Database.BatchableContextImpl",
		"apex:Database.BatchableContextImpl.BatchableContextImpl()",
		"apex:System.Database.executeBatch(Object)",
		"apex:System.Database.executeBatch(Object,Integer)",
	} {
		if !localProofCommandMatchesDisposition(localRuntimeRequired, "test", id) {
			t.Fatalf("Batchable test command rejected for %s", id)
		}
	}
	for _, id := range []string{
		"apex:System.Database.getCursor(String,Object)",
		"apex:Database.QueryLocator",
	} {
		if localProofCommandMatchesDisposition(localRuntimeRequired, "test", id) {
			t.Fatalf("unrelated test command accepted for %s", id)
		}
	}
}
