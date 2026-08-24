//go:build darwin || linux

package corpusassurance

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestWorkerLifecycleLockFencesRawCreationAndCleanup(t *testing.T) {
	_, _, cleanupRequest, _ := workerCleanupTestRequest(t, "reservation-only")
	unlockCleanup := holdWorkerLifecycleLock(t, cleanupRequest.OutputRoot)
	cleanupCalled := false
	cleanupRequest.cleanup = func(SalesforceOrgCleanupRequest) (SalesforceOrgCleanup, error) {
		cleanupCalled = true
		return SalesforceOrgCleanup{}, nil
	}
	if _, err := RunOrchestratorWorkerCleanup(cleanupRequest); err == nil || cleanupCalled {
		t.Fatalf("cleanup entered held lifecycle: called=%t err=%v", cleanupCalled, err)
	}
	unlockCleanup()

	rawRequest, rawRoot, _ := rawSalesforceShardTestRequest(t)
	unlockRaw := holdWorkerLifecycleLock(t, rawRoot)
	rawCalled := false
	rawRequest.orgCreate = func(SalesforceOrgCreateRequest) (SalesforceOrgCreation, error) {
		rawCalled = true
		return SalesforceOrgCreation{}, fmt.Errorf("stop after entry")
	}
	if _, err := RunRawSalesforceShard(rawRequest); err == nil || rawCalled {
		t.Fatalf("raw creation entered held lifecycle: called=%t err=%v", rawCalled, err)
	}
	unlockRaw()
}

func holdWorkerLifecycleLock(t *testing.T, outputRoot string) func() {
	t.Helper()
	file, err := os.OpenFile(outputRoot+".lifecycle.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		t.Fatal(err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}
}
