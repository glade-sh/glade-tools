//go:build darwin || linux

package compat

import (
	"syscall"
	"testing"
)

func TestLocalTestComparisonProcessGroupGoneKeepsPollingOnPermissionError(t *testing.T) {
	gone, err := localTestComparisonProcessGroupGone(syscall.EPERM)
	if err != nil {
		t.Fatalf("localTestComparisonProcessGroupGone(EPERM) error = %v", err)
	}
	if gone {
		t.Fatal("localTestComparisonProcessGroupGone(EPERM) reported the group gone")
	}
}
