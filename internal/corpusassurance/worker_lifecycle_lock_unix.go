//go:build darwin || linux

package corpusassurance

import (
	"fmt"
	"os"
	"syscall"
)

func acquireWorkerLifecycleLock(outputRoot string) (func(), error) {
	fd, err := syscall.Open(outputRoot+".lifecycle.lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("worker lifecycle is unavailable")
	}
	file := os.NewFile(uintptr(fd), "worker-lifecycle-lock")
	if syscall.Fchmod(fd, 0o600) != nil || syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		file.Close()
		return nil, fmt.Errorf("worker lifecycle is active")
	}
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}
