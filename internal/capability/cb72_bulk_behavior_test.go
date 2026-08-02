package capability

import "testing"

func TestCB72FrozenMethodsUseImplementedStubBehavior(t *testing.T) {
	entries := make(map[string]StubBehaviorEntry)
	for _, entry := range BuildStubBehaviorReport().Entries {
		entries[entry.ID] = entry
	}
	want := map[string]StubBehaviorStatus{
		"Security.stripInaccessible(AccessType,List<SObject>,Boolean,Id)": StubBehaviorImplemented,
		"Process.SparkPlugApi.describePlugin(String)":                     StubBehaviorImplemented,
		"Process.SparkPlugApi.describePlugins()":                          StubBehaviorImplemented,
		"Process.SparkPlugApi.invokePluginWithJson(String,String)":        StubBehaviorImplemented,
		"Crypto.decryptWithManagedIV(String,Blob,Blob,Blob)":              StubBehaviorImplemented,
		"Crypto.encryptWithManagedIV(String,Blob,Blob,Blob)":              StubBehaviorImplemented,
		"TimeZone.getDisplayName()":                                       StubBehaviorImplemented,
		"TimeZone.getTimeZone(String)":                                    StubBehaviorImplemented,
		"Crypto.decryptWithManagedIV(String,Blob,Blob)":                   StubBehaviorImplemented,
		"Crypto.encryptWithManagedIV(String,Blob,Blob)":                   StubBehaviorImplemented,
	}
	for id, status := range want {
		entry, ok := entries[id]
		if !ok {
			t.Fatalf("missing stub behavior entry %s", id)
		}
		if entry.Status != status {
			t.Errorf("%s status = %q, want %q", id, entry.Status, status)
		}
	}

	for _, id := range []string{
		"Crypto.decryptWithManagedIV(String,Blob,Blob,Blob)",
		"Crypto.encryptWithManagedIV(String,Blob,Blob,Blob)",
	} {
		if len(entries[id].Evidence) == 0 {
			t.Errorf("%s has no existing behavior evidence", id)
		}
	}
}
