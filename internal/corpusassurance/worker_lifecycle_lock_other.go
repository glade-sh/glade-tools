//go:build !darwin && !linux

package corpusassurance

import "fmt"

func acquireWorkerLifecycleLock(string) (func(), error) {
	return nil, fmt.Errorf("worker lifecycle locking is unsupported")
}
