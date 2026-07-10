//go:build !darwin && !linux

package compat

import (
	"errors"
	"os"
	"os/exec"
)

func configureLocalTestComparisonProcess(*exec.Cmd) error {
	return errors.New("local test comparison process-tree termination is unsupported on this platform")
}

func cleanupLocalTestComparisonProcess(*exec.Cmd) error {
	return errors.New("local test comparison process-tree termination is unsupported on this platform")
}

func validateLocalTestComparisonOwner(os.FileInfo) error {
	return errors.New("local test comparison file ownership is unsupported on this platform")
}

func createLocalTestComparisonArtifactDirectory(*os.File, string) (*os.File, error) {
	return nil, errors.New("descriptor-relative local test comparison artifacts are unsupported on this platform")
}

func createLocalTestComparisonArtifactFile(*os.File, string) (*os.File, error) {
	return nil, errors.New("descriptor-relative local test comparison artifacts are unsupported on this platform")
}

func openLocalTestComparisonArtifactFile(*os.File, string) (*os.File, error) {
	return nil, errors.New("descriptor-relative local test comparison artifacts are unsupported on this platform")
}

func validateLocalTestComparisonArtifactDirectory(*os.File, *os.File, string) error {
	return errors.New("descriptor-relative local test comparison artifacts are unsupported on this platform")
}

func removeLocalTestComparisonArtifactDirectory(*os.File, *os.File, string, []string) error {
	return errors.New("descriptor-relative local test comparison artifacts are unsupported on this platform")
}
